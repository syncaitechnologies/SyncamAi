-- ADR-016: self-service workspace registration with a 14-day trial.
--
-- A signed-in user with no membership at all may create exactly one new
-- workspace (tenant + first site) and becomes its Super Admin. The runtime role
-- still has no direct write access to tenants or memberships: creation goes
-- through one narrow definer routine owned by a dedicated NOLOGIN role.

-- Trial and plan limits. Existing tenants stay active with no limits.
ALTER TABLE identity.tenants
    ADD COLUMN IF NOT EXISTS plan varchar(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS trial_ends_at timestamptz,
    ADD COLUMN IF NOT EXISTS site_limit integer,
    ADD COLUMN IF NOT EXISTS camera_limit integer;

ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_plan_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_plan_check
    CHECK (plan IN ('trial', 'active', 'suspended'));
ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_limits_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_limits_check
    CHECK ((site_limit IS NULL OR site_limit >= 0) AND (camera_limit IS NULL OR camera_limit >= 0));
ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_trial_end_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_trial_end_check
    CHECK (plan <> 'trial' OR trial_ends_at IS NOT NULL);

-- The access-token hook now also reads the member's tenant plan.
DROP POLICY IF EXISTS supabase_auth_admin_tenant_read ON identity.tenants;
CREATE POLICY supabase_auth_admin_tenant_read ON identity.tenants
    FOR SELECT TO supabase_auth_admin USING (true);
GRANT SELECT ON identity.tenants TO supabase_auth_admin;

-- Token hook:
--  * no membership row at all  -> a normal token without app_metadata.syncam,
--    plus a server-set app_metadata.syncam_registration.email_verified flag,
--    so the user can reach the "create your workspace" screen;
--  * suspended or revoked      -> still refused, exactly as before;
--  * active                    -> the usual claims plus plan, trial end and limits.
-- Every existing API route still requires app_metadata.syncam.
CREATE OR REPLACE FUNCTION identity.syncam_custom_access_token(event jsonb)
RETURNS jsonb
LANGUAGE plpgsql
SET search_path = ''
AS $$
DECLARE
    membership identity.user_tenant_memberships%ROWTYPE;
    workspace identity.tenants%ROWTYPE;
    allowed_site_ids uuid[];
    email_verified boolean;
    claims jsonb;
BEGIN
    claims := coalesce(event->'claims', '{}'::jsonb);
    IF jsonb_typeof(claims->'app_metadata') IS DISTINCT FROM 'object' THEN
        claims := jsonb_set(claims, '{app_metadata}', '{}'::jsonb, true);
    END IF;
    -- Authorization is only ever issued by this hook, never carried over.
    claims := claims #- '{app_metadata,syncam}' #- '{app_metadata,syncam_registration}';

    SELECT * INTO membership
    FROM identity.user_tenant_memberships
    WHERE user_id = (event->>'user_id')::uuid;

    IF NOT FOUND THEN
        SELECT u.email_confirmed_at IS NOT NULL INTO email_verified
        FROM auth.users AS u
        WHERE u.id = (event->>'user_id')::uuid;
        claims := jsonb_set(
            claims,
            '{app_metadata,syncam_registration}',
            jsonb_build_object('email_verified', coalesce(email_verified, false)),
            true
        );
        RETURN jsonb_build_object('claims', claims);
    END IF;

    IF membership.status <> 'active' THEN
        RAISE EXCEPTION 'SyncCam user is not provisioned or active' USING ERRCODE = '42501';
    END IF;

    SELECT coalesce(array_agg(site_id ORDER BY site_id), ARRAY[]::uuid[])
    INTO allowed_site_ids
    FROM identity.user_site_memberships
    WHERE user_id = membership.user_id
      AND tenant_id = membership.tenant_id
      AND status = 'active';

    SELECT * INTO workspace FROM identity.tenants WHERE id = membership.tenant_id;

    claims := jsonb_set(
        claims,
        '{app_metadata,syncam}',
        jsonb_build_object(
            'tenant_id', membership.tenant_id,
            'site_ids', to_jsonb(allowed_site_ids),
            'roles', to_jsonb(membership.roles),
            'scopes', to_jsonb(membership.scopes),
            'data_class', to_jsonb(membership.data_classes),
            'plan', coalesce(workspace.plan, 'active'),
            'trial_ends_at', to_jsonb(workspace.trial_ends_at),
            'site_limit', to_jsonb(workspace.site_limit),
            'camera_limit', to_jsonb(workspace.camera_limit)
        ),
        true
    );
    RETURN jsonb_build_object('claims', claims);
END;
$$;

-- Dedicated owner for the registration routine: no login, no RLS bypass.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_registration_executor') THEN
        CREATE ROLE syncam_registration_executor
            NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
END;
$$;

-- Tenants, sites and audit events already carry a tenant_isolation policy for
-- every role. Memberships need explicit policies for the executor: it may see
-- the registering user's own membership (to make a repeat call return it) and
-- rows of the tenant it is creating, and may insert only into that tenant.
DROP POLICY IF EXISTS registration_executor_tenant_membership_read ON identity.user_tenant_memberships;
CREATE POLICY registration_executor_tenant_membership_read ON identity.user_tenant_memberships
    FOR SELECT TO syncam_registration_executor
    USING (
        user_id = NULLIF(current_setting('app.registration_user_id', true), '')::uuid
        OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    );
DROP POLICY IF EXISTS registration_executor_tenant_membership_insert ON identity.user_tenant_memberships;
CREATE POLICY registration_executor_tenant_membership_insert ON identity.user_tenant_memberships
    FOR INSERT TO syncam_registration_executor
    WITH CHECK (
        tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND user_id = NULLIF(current_setting('app.registration_user_id', true), '')::uuid
    );
DROP POLICY IF EXISTS registration_executor_site_membership_read ON identity.user_site_memberships;
CREATE POLICY registration_executor_site_membership_read ON identity.user_site_memberships
    FOR SELECT TO syncam_registration_executor
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
DROP POLICY IF EXISTS registration_executor_site_membership_insert ON identity.user_site_memberships;
CREATE POLICY registration_executor_site_membership_insert ON identity.user_site_memberships
    FOR INSERT TO syncam_registration_executor
    WITH CHECK (
        tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND user_id = NULLIF(current_setting('app.registration_user_id', true), '')::uuid
    );

CREATE OR REPLACE FUNCTION identity.register_workspace(
    p_user_id uuid,
    p_request_id uuid,
    p_company text,
    p_site_name text,
    p_timezone text
)
RETURNS TABLE (tenant_id uuid, site_id uuid, created boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
#variable_conflict use_column
DECLARE
    v_company text := btrim(coalesce(p_company, ''));
    v_site_name text := btrim(coalesce(p_site_name, ''));
    v_timezone text := btrim(coalesce(p_timezone, ''));
    v_existing identity.user_tenant_memberships%ROWTYPE;
    v_tenant_id uuid;
    v_site_id uuid;
    v_slug text;
    v_occurred_at timestamptz := clock_timestamp();
    v_occurred_at_text text;
    v_chain_date date;
    v_previous_hash bytea;
    v_payload_text text;
    v_payload_hash bytea;
    v_record_hash bytea;
    v_entry jsonb;
BEGIN
    IF p_user_id IS NULL OR p_request_id IS NULL THEN
        RAISE EXCEPTION 'user and request identifiers are required' USING ERRCODE = '22023';
    END IF;
    IF length(v_company) NOT BETWEEN 2 AND 120 OR length(v_site_name) NOT BETWEEN 2 AND 120 THEN
        RAISE EXCEPTION 'company and site names must be 2 to 120 characters' USING ERRCODE = '22023';
    END IF;
    IF length(v_timezone) NOT BETWEEN 1 AND 64
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_timezone_names WHERE name = v_timezone) THEN
        RAISE EXCEPTION 'timezone must be an IANA timezone' USING ERRCODE = '22023';
    END IF;

    -- One registration at a time per user, so a double-click creates one workspace.
    PERFORM pg_advisory_xact_lock(hashtextextended('syncam-registration:' || p_user_id::text, 0));
    PERFORM set_config('app.registration_user_id', p_user_id::text, true);

    SELECT * INTO v_existing
    FROM identity.user_tenant_memberships AS m
    WHERE m.user_id = p_user_id;
    IF FOUND THEN
        IF v_existing.status <> 'active' THEN
            RAISE EXCEPTION 'membership is not active' USING ERRCODE = '42501';
        END IF;
        PERFORM set_config('app.tenant_id', v_existing.tenant_id::text, true);
        SELECT s.id INTO v_site_id
        FROM config.sites AS s
        WHERE s.tenant_id = v_existing.tenant_id AND s.status <> 'retired'
        ORDER BY s.created_at, s.id
        LIMIT 1;
        tenant_id := v_existing.tenant_id;
        site_id := v_site_id;
        created := false;
        RETURN NEXT;
        RETURN;
    END IF;

    v_tenant_id := pg_catalog.gen_random_uuid();
    v_site_id := pg_catalog.gen_random_uuid();
    PERFORM set_config('app.tenant_id', v_tenant_id::text, true);

    v_slug := btrim(left(btrim(regexp_replace(lower(v_company), '[^a-z0-9]+', '-', 'g'), '-'), 40), '-');
    IF v_slug = '' THEN
        v_slug := 'workspace';
    END IF;
    v_slug := v_slug || '-' || left(replace(v_tenant_id::text, '-', ''), 8);

    INSERT INTO identity.tenants (
        id, name, slug, data_region, tier, plan, trial_ends_at, site_limit, camera_limit
    ) VALUES (
        v_tenant_id, v_company, v_slug, 'ap-south-1', 'smb', 'trial',
        v_occurred_at + interval '14 days', 1, 10
    );
    INSERT INTO config.sites (id, tenant_id, name, timezone, created_by)
    VALUES (v_site_id, v_tenant_id, v_site_name, v_timezone, p_user_id::text);
    INSERT INTO identity.user_tenant_memberships (
        user_id, tenant_id, roles, scopes, data_classes, status
    ) VALUES (
        p_user_id,
        v_tenant_id,
        ARRAY['super_admin']::text[],
        ARRAY[
            'auth:read', 'sites:read', 'tenant:manage', 'users:manage',
            'site:manage', 'config:read', 'config:write', 'raw_video:read',
            'alerts:read', 'alerts:write', 'evidence:export', 'biometric:read',
            'audit:read', 'analytics:read', 'events:write', 'privacy_masks:approve'
        ]::text[],
        ARRAY['metadata', 'raw_video', 'biometric']::text[],
        'active'
    );
    INSERT INTO identity.user_site_memberships (user_id, tenant_id, site_id, status)
    VALUES (p_user_id, v_tenant_id, v_site_id, 'active');

    -- Hash-chained audit events, in the same canonical form as the ADR-010
    -- bootstrap routine.
    v_chain_date := (v_occurred_at AT TIME ZONE 'UTC')::date;
    PERFORM pg_advisory_xact_lock(
        hashtextextended(v_tenant_id::text || ':' || to_char(v_chain_date, 'YYYY-MM-DD'), 0)
    );
    v_previous_hash := decode(repeat('00', 32), 'hex');
    v_occurred_at_text := regexp_replace(
        regexp_replace(
            to_char(v_occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            '(\.\d*?)0+Z$', '\1Z'
        ),
        '\.Z$', 'Z'
    );
    FOR v_entry IN
        SELECT value FROM jsonb_array_elements(jsonb_build_array(
            jsonb_build_object(
                'action', 'tenant.self_registered', 'resource_type', 'tenant',
                'resource_id', v_tenant_id::text,
                'after_state', jsonb_build_object(
                    'name', v_company, 'slug', v_slug, 'plan', 'trial',
                    'trial_days', 14, 'site_limit', 1, 'camera_limit', 10
                )
            ),
            jsonb_build_object(
                'action', 'site.created', 'resource_type', 'site',
                'resource_id', v_site_id::text,
                'after_state', jsonb_build_object(
                    'id', v_site_id, 'tenant_id', v_tenant_id,
                    'name', v_site_name, 'timezone', v_timezone
                )
            ),
            jsonb_build_object(
                'action', 'identity.super_admin.self_registered',
                'resource_type', 'user_tenant_membership',
                'resource_id', p_user_id::text,
                'after_state', jsonb_build_object(
                    'roles', jsonb_build_array('super_admin'), 'status', 'active',
                    'tenant_id', v_tenant_id, 'user_id', p_user_id
                )
            )
        ))
    LOOP
        v_payload_text :=
            '{"version":1,"tenant_id":' || to_json(v_tenant_id::text)::text ||
            ',"actor_id":' || to_json(p_user_id::text)::text ||
            ',"action":' || to_json(v_entry->>'action')::text ||
            ',"resource_type":' || to_json(v_entry->>'resource_type')::text ||
            ',"resource_id":' || to_json(v_entry->>'resource_id')::text ||
            ',"request_id":' || to_json(p_request_id::text)::text ||
            ',"occurred_at":' || to_json(v_occurred_at_text)::text ||
            ',"before_state":null' ||
            ',"after_state":' || (v_entry->'after_state')::text ||
            '}';
        v_payload_hash := extensions.digest(convert_to(v_payload_text, 'UTF8'), 'sha256');
        v_record_hash := extensions.digest(
            v_previous_hash || v_payload_hash || convert_to(v_occurred_at_text, 'UTF8'),
            'sha256'
        );
        INSERT INTO audit.events (
            event_id, tenant_id, chain_date, occurred_at, actor_id, action,
            resource_type, resource_id, request_id, before_state, after_state,
            canonical_payload, canonical_payload_bytes, previous_hash, record_hash
        ) VALUES (
            pg_catalog.gen_random_uuid(), v_tenant_id, v_chain_date, v_occurred_at,
            p_user_id::text, v_entry->>'action', v_entry->>'resource_type',
            v_entry->>'resource_id', p_request_id, 'null'::jsonb, v_entry->'after_state',
            v_payload_text::jsonb, convert_to(v_payload_text, 'UTF8'),
            v_previous_hash, v_record_hash
        );
        v_previous_hash := v_record_hash;
    END LOOP;

    tenant_id := v_tenant_id;
    site_id := v_site_id;
    created := true;
    RETURN NEXT;
END;
$$;

-- New functions inherit PUBLIC EXECUTE: remove it, allow only the Go API's
-- application role, then hand ownership to the narrow executor.
REVOKE ALL ON FUNCTION identity.register_workspace(uuid, uuid, text, text, text) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION identity.register_workspace(uuid, uuid, text, text, text) FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'service_role') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION identity.register_workspace(uuid, uuid, text, text, text) FROM service_role';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION identity.register_workspace(uuid, uuid, text, text, text) TO syncam_app';
    END IF;
END;
$$;

DO $$
BEGIN
    EXECUTE format('GRANT syncam_registration_executor TO %I WITH SET TRUE', current_user);
END;
$$;
GRANT USAGE, CREATE ON SCHEMA identity TO syncam_registration_executor;
ALTER FUNCTION identity.register_workspace(uuid, uuid, text, text, text)
    OWNER TO syncam_registration_executor;
REVOKE CREATE ON SCHEMA identity FROM syncam_registration_executor;
DO $$
BEGIN
    EXECUTE format('REVOKE syncam_registration_executor FROM %I', current_user);
END;
$$;

GRANT USAGE ON SCHEMA identity, config, audit, extensions TO syncam_registration_executor;
GRANT SELECT, INSERT ON identity.tenants TO syncam_registration_executor;
GRANT SELECT, INSERT ON config.sites TO syncam_registration_executor;
GRANT SELECT, INSERT ON identity.user_tenant_memberships TO syncam_registration_executor;
GRANT SELECT, INSERT ON identity.user_site_memberships TO syncam_registration_executor;
GRANT SELECT, INSERT ON audit.events TO syncam_registration_executor;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO syncam_registration_executor;
