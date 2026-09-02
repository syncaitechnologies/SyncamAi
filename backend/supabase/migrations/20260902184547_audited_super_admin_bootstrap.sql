-- The initial Super Admin has no authenticated predecessor. This private,
-- owner-operated function is deliberately the only narrow exception to the
-- normal Go user-management boundary. It is not exposed by the Data API and
-- no runtime role receives EXECUTE.

ALTER TABLE audit.events
    ADD COLUMN IF NOT EXISTS canonical_payload_bytes bytea;

ALTER TABLE audit.events
    DROP CONSTRAINT IF EXISTS audit_events_canonical_payload_bytes_nonempty;
ALTER TABLE audit.events
    ADD CONSTRAINT audit_events_canonical_payload_bytes_nonempty
    CHECK (
        canonical_payload_bytes IS NULL
        OR octet_length(canonical_payload_bytes) > 0
    );

COMMENT ON COLUMN audit.events.canonical_payload_bytes IS
    'Exact UTF-8 canonical audit payload when an audit writer can preserve it.';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_bootstrap_executor') THEN
        CREATE ROLE syncam_bootstrap_executor
            NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
END;
$$;

DROP POLICY IF EXISTS bootstrap_executor_tenant_membership_read
    ON identity.user_tenant_memberships;
CREATE POLICY bootstrap_executor_tenant_membership_read
    ON identity.user_tenant_memberships
    FOR SELECT TO syncam_bootstrap_executor
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS bootstrap_executor_tenant_membership_insert
    ON identity.user_tenant_memberships;
CREATE POLICY bootstrap_executor_tenant_membership_insert
    ON identity.user_tenant_memberships
    FOR INSERT TO syncam_bootstrap_executor
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION identity.bootstrap_initial_super_admin(
    p_tenant_id uuid,
    p_user_id uuid,
    p_request_id uuid,
    p_approval_reference text
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
DECLARE
    v_approval_reference text := btrim(p_approval_reference);
    v_occurred_at timestamptz := clock_timestamp();
    v_occurred_at_text text;
    v_chain_date date;
    v_previous_hash bytea := decode(repeat('00', 32), 'hex');
    v_payload_text text;
    v_payload_hash bytea;
    v_record_hash bytea;
    v_after_state jsonb;
BEGIN
    IF p_tenant_id IS NULL OR p_user_id IS NULL OR p_request_id IS NULL THEN
        RAISE EXCEPTION 'tenant, user, and request identifiers are required'
            USING ERRCODE = '22023';
    END IF;
    IF v_approval_reference IS NULL
       OR v_approval_reference !~ '^[A-Za-z0-9][A-Za-z0-9._:/#-]{0,127}$' THEN
        RAISE EXCEPTION 'owner approval reference is required and must be a safe change identifier'
            USING ERRCODE = '22023';
    END IF;

    -- Serialize all bootstrap attempts for this tenant, including an attempt
    -- that crosses the UTC day boundary used by the audit hash chain.
    PERFORM pg_advisory_xact_lock(
        hashtextextended('syncam-bootstrap:' || p_tenant_id::text, 0)
    );
    PERFORM set_config('app.tenant_id', p_tenant_id::text, true);

    PERFORM 1 FROM identity.tenants WHERE id = p_tenant_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'tenant does not exist' USING ERRCODE = '23503';
    END IF;

    PERFORM 1 FROM auth.users WHERE id = p_user_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Auth user does not exist' USING ERRCODE = '23503';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM identity.user_tenant_memberships
        WHERE user_id = p_user_id
    ) THEN
        RAISE EXCEPTION 'Auth user already has a tenant membership'
            USING ERRCODE = '23505';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM identity.user_tenant_memberships
        WHERE tenant_id = p_tenant_id
          AND status = 'active'
          AND roles @> ARRAY['super_admin']::text[]
    ) THEN
        RAISE EXCEPTION 'tenant already has an active Super Admin'
            USING ERRCODE = '23505';
    END IF;

    INSERT INTO identity.user_tenant_memberships (
        user_id, tenant_id, roles, scopes, data_classes, status
    ) VALUES (
        p_user_id,
        p_tenant_id,
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

    v_chain_date := (v_occurred_at AT TIME ZONE 'UTC')::date;
    PERFORM pg_advisory_xact_lock(
        hashtextextended(
            p_tenant_id::text || ':' || to_char(v_chain_date, 'YYYY-MM-DD'),
            0
        )
    );
    SELECT record_hash INTO v_previous_hash
    FROM audit.events
    WHERE tenant_id = p_tenant_id AND chain_date = v_chain_date
    ORDER BY chain_sequence DESC
    LIMIT 1;
    v_previous_hash := coalesce(v_previous_hash, decode(repeat('00', 32), 'hex'));

    v_occurred_at_text := regexp_replace(
        regexp_replace(
            to_char(v_occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            '(\.\d*?)0+Z$', '\1Z'
        ),
        '\.Z$', 'Z'
    );
    v_after_state := jsonb_build_object(
        'approval_reference', v_approval_reference,
        'roles', jsonb_build_array('super_admin'),
        'status', 'active',
        'tenant_id', p_tenant_id,
        'user_id', p_user_id
    );
    v_payload_text :=
        '{"version":1,"tenant_id":' || to_json(p_tenant_id::text)::text ||
        ',"actor_id":"owner-approved-bootstrap"' ||
        ',"action":"identity.super_admin.bootstrapped"' ||
        ',"resource_type":"user_tenant_membership"' ||
        ',"resource_id":' || to_json(p_user_id::text)::text ||
        ',"request_id":' || to_json(p_request_id::text)::text ||
        ',"occurred_at":' || to_json(v_occurred_at_text)::text ||
        ',"before_state":null' ||
        ',"after_state":' || v_after_state::text ||
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
        pg_catalog.gen_random_uuid(), p_tenant_id, v_chain_date, v_occurred_at,
        'owner-approved-bootstrap', 'identity.super_admin.bootstrapped',
        'user_tenant_membership', p_user_id::text, p_request_id,
        'null'::jsonb, v_after_state, v_payload_text::jsonb,
        convert_to(v_payload_text, 'UTF8'), v_previous_hash, v_record_hash
    );
END;
$$;

-- New functions inherit PUBLIC EXECUTE. Remove it while the migration runner
-- still owns the function, before assigning the restricted owner below.
REVOKE ALL ON FUNCTION identity.bootstrap_initial_super_admin(uuid, uuid, uuid, text)
    FROM PUBLIC, anon, authenticated, service_role, supabase_auth_admin, syncam_app;

-- PostgreSQL allows changing an object's owner only when the migration runner
-- can SET ROLE to that owner. The membership exists only for this migration;
-- the function keeps its narrow owner after the membership is revoked.
DO $$
BEGIN
    EXECUTE format('GRANT syncam_bootstrap_executor TO %I', current_user);
END;
$$;

GRANT USAGE, CREATE ON SCHEMA identity TO syncam_bootstrap_executor;

ALTER FUNCTION identity.bootstrap_initial_super_admin(uuid, uuid, uuid, text)
    OWNER TO syncam_bootstrap_executor;

REVOKE CREATE ON SCHEMA identity FROM syncam_bootstrap_executor;

DO $$
BEGIN
    EXECUTE format('REVOKE syncam_bootstrap_executor FROM %I', current_user);
END;
$$;

GRANT USAGE ON SCHEMA identity, audit TO syncam_bootstrap_executor;
GRANT USAGE ON SCHEMA extensions TO syncam_bootstrap_executor;
GRANT SELECT ON identity.tenants TO syncam_bootstrap_executor;
GRANT SELECT, INSERT ON identity.user_tenant_memberships TO syncam_bootstrap_executor;
GRANT SELECT, INSERT ON audit.events TO syncam_bootstrap_executor;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO syncam_bootstrap_executor;
GRANT USAGE ON SCHEMA auth TO syncam_bootstrap_executor;
GRANT SELECT ON auth.users TO syncam_bootstrap_executor;
