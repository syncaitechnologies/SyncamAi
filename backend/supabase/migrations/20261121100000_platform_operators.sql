-- Owner admin screen (ADR-018): platform operators (the people who run
-- SyncCam) can see every workspace and change its plan, trial end, limits and
-- live-view minutes. Each change is written to that workspace's audit log.
--
-- Operators are listed in platform.operators, added only by the project owner
-- in the SQL editor. The token hook marks them with a server-set claim so the
-- dashboard shows the screen, and every admin routine re-checks the table, so
-- removing someone takes effect immediately.

CREATE TABLE IF NOT EXISTS platform.operators (
    user_id uuid PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    note varchar(200)
);
ALTER TABLE platform.operators ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.operators FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS supabase_auth_admin_operator_read ON platform.operators;
CREATE POLICY supabase_auth_admin_operator_read ON platform.operators
    FOR SELECT TO supabase_auth_admin USING (true);
GRANT USAGE ON SCHEMA platform TO supabase_auth_admin;
GRANT SELECT ON platform.operators TO supabase_auth_admin;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON platform.operators FROM anon, authenticated';
    END IF;
END;
$$;

-- Token hook: unchanged except for the server-set operator flag.
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
    claims := claims #- '{app_metadata,syncam}' #- '{app_metadata,syncam_registration}' #- '{app_metadata,syncam_operator}';

    IF EXISTS (SELECT 1 FROM platform.operators AS o WHERE o.user_id = (event->>'user_id')::uuid) THEN
        claims := jsonb_set(claims, '{app_metadata,syncam_operator}', 'true'::jsonb, true);
    END IF;

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

-- Every workspace with its plan, limits and this month's use.
CREATE OR REPLACE FUNCTION platform.admin_list_workspaces(p_actor uuid)
RETURNS TABLE (
    tenant_id uuid, name text, slug text, plan text, trial_ends_at timestamptz,
    site_limit integer, camera_limit integer, live_view_minutes integer,
    live_view_seconds bigint, sites bigint, cameras bigint, created_at timestamptz
)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = ''
AS $$
#variable_conflict use_column
BEGIN
    IF p_actor IS NULL OR NOT EXISTS (SELECT 1 FROM platform.operators AS o WHERE o.user_id = p_actor) THEN
        RAISE EXCEPTION 'not a platform operator' USING ERRCODE = '42501';
    END IF;
    RETURN QUERY
    SELECT t.id, t.name::text, t.slug::text, t.plan::text, t.trial_ends_at,
           t.site_limit, t.camera_limit, t.live_view_minutes,
           coalesce((SELECT u.seconds_used FROM platform.live_view_usage AS u
                     WHERE u.tenant_id = t.id AND u.month = date_trunc('month', clock_timestamp() AT TIME ZONE 'UTC')::date), 0),
           (SELECT count(*) FROM config.sites AS s WHERE s.tenant_id = t.id AND s.status <> 'retired'),
           (SELECT count(*) FROM config.cameras AS c WHERE c.tenant_id = t.id AND c.lifecycle_status <> 'retired'),
           t.created_at
    FROM identity.tenants AS t
    ORDER BY t.created_at DESC;
END;
$$;

-- Change one workspace. p_changes may hold any of: plan, trial_ends_at,
-- site_limit, camera_limit, live_view_minutes (JSON null = unlimited for the
-- limits). Unknown keys and invalid values are refused.
CREATE OR REPLACE FUNCTION platform.admin_update_workspace(p_actor uuid, p_request_id uuid, p_tenant uuid, p_changes jsonb)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
DECLARE
    v_before jsonb;
    v_after jsonb;
    v_key text;
    v_occurred_at timestamptz := clock_timestamp();
    v_occurred_at_text text;
    v_chain_date date;
    v_previous_hash bytea;
    v_payload_text text;
    v_payload_hash bytea;
    v_record_hash bytea;
BEGIN
    IF p_actor IS NULL OR NOT EXISTS (SELECT 1 FROM platform.operators AS o WHERE o.user_id = p_actor) THEN
        RAISE EXCEPTION 'not a platform operator' USING ERRCODE = '42501';
    END IF;
    IF p_request_id IS NULL OR p_tenant IS NULL OR jsonb_typeof(p_changes) IS DISTINCT FROM 'object' OR p_changes = '{}'::jsonb THEN
        RAISE EXCEPTION 'nothing to change' USING ERRCODE = '22023';
    END IF;
    FOR v_key IN SELECT jsonb_object_keys(p_changes) LOOP
        IF v_key NOT IN ('plan', 'trial_ends_at', 'site_limit', 'camera_limit', 'live_view_minutes') THEN
            RAISE EXCEPTION 'unknown setting %', v_key USING ERRCODE = '22023';
        END IF;
    END LOOP;

    PERFORM set_config('app.tenant_id', p_tenant::text, true);
    SELECT jsonb_build_object(
        'plan', t.plan, 'trial_ends_at', t.trial_ends_at, 'site_limit', t.site_limit,
        'camera_limit', t.camera_limit, 'live_view_minutes', t.live_view_minutes)
    INTO v_before
    FROM identity.tenants AS t WHERE t.id = p_tenant FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'workspace not found' USING ERRCODE = 'P0002';
    END IF;

    BEGIN
        UPDATE identity.tenants AS t SET
            plan = CASE WHEN p_changes ? 'plan' THEN p_changes->>'plan' ELSE t.plan END,
            trial_ends_at = CASE WHEN p_changes ? 'trial_ends_at' THEN (p_changes->>'trial_ends_at')::timestamptz ELSE t.trial_ends_at END,
            site_limit = CASE WHEN p_changes ? 'site_limit' THEN (p_changes->>'site_limit')::integer ELSE t.site_limit END,
            camera_limit = CASE WHEN p_changes ? 'camera_limit' THEN (p_changes->>'camera_limit')::integer ELSE t.camera_limit END,
            live_view_minutes = CASE WHEN p_changes ? 'live_view_minutes' THEN (p_changes->>'live_view_minutes')::integer ELSE t.live_view_minutes END,
            updated_at = v_occurred_at
        WHERE t.id = p_tenant;
    EXCEPTION
        WHEN check_violation OR not_null_violation OR invalid_text_representation OR invalid_datetime_format OR datetime_field_overflow OR numeric_value_out_of_range THEN
            RAISE EXCEPTION 'invalid value: %', SQLERRM USING ERRCODE = '22023';
    END;

    SELECT jsonb_build_object(
        'plan', t.plan, 'trial_ends_at', t.trial_ends_at, 'site_limit', t.site_limit,
        'camera_limit', t.camera_limit, 'live_view_minutes', t.live_view_minutes)
    INTO v_after
    FROM identity.tenants AS t WHERE t.id = p_tenant;

    -- Hash-chained audit event in the workspace's log, in the same canonical
    -- form as the other database audit writers.
    v_chain_date := (v_occurred_at AT TIME ZONE 'UTC')::date;
    PERFORM pg_advisory_xact_lock(hashtextextended(p_tenant::text || ':' || to_char(v_chain_date, 'YYYY-MM-DD'), 0));
    SELECT e.record_hash INTO v_previous_hash
    FROM audit.events AS e
    WHERE e.tenant_id = p_tenant AND e.chain_date = v_chain_date
    ORDER BY e.chain_sequence DESC LIMIT 1;
    v_previous_hash := coalesce(v_previous_hash, decode(repeat('00', 32), 'hex'));
    v_occurred_at_text := regexp_replace(
        regexp_replace(to_char(v_occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '(\.\d*?)0+Z$', '\1Z'),
        '\.Z$', 'Z');
    v_payload_text :=
        '{"version":1,"tenant_id":' || to_json(p_tenant::text)::text ||
        ',"actor_id":' || to_json('operator:' || p_actor::text)::text ||
        ',"action":"tenant.plan.changed"' ||
        ',"resource_type":"tenant"' ||
        ',"resource_id":' || to_json(p_tenant::text)::text ||
        ',"request_id":' || to_json(p_request_id::text)::text ||
        ',"occurred_at":' || to_json(v_occurred_at_text)::text ||
        ',"before_state":' || v_before::text ||
        ',"after_state":' || v_after::text ||
        '}';
    v_payload_hash := extensions.digest(convert_to(v_payload_text, 'UTF8'), 'sha256');
    v_record_hash := extensions.digest(v_previous_hash || v_payload_hash || convert_to(v_occurred_at_text, 'UTF8'), 'sha256');
    INSERT INTO audit.events (
        event_id, tenant_id, chain_date, occurred_at, actor_id, action,
        resource_type, resource_id, request_id, before_state, after_state,
        canonical_payload, canonical_payload_bytes, previous_hash, record_hash
    ) VALUES (
        pg_catalog.gen_random_uuid(), p_tenant, v_chain_date, v_occurred_at,
        'operator:' || p_actor::text, 'tenant.plan.changed', 'tenant', p_tenant::text, p_request_id,
        v_before, v_after, v_payload_text::jsonb, convert_to(v_payload_text, 'UTF8'),
        v_previous_hash, v_record_hash
    );
END;
$$;

REVOKE ALL ON FUNCTION platform.admin_list_workspaces(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION platform.admin_update_workspace(uuid, uuid, uuid, jsonb) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION platform.admin_list_workspaces(uuid) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION platform.admin_update_workspace(uuid, uuid, uuid, jsonb) FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.admin_list_workspaces(uuid) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.admin_update_workspace(uuid, uuid, uuid, jsonb) TO syncam_app';
    END IF;
END;
$$;
