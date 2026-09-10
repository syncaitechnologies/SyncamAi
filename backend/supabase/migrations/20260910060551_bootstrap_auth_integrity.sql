-- T-0413: preserve ADR-010's private audited boundary without direct Auth reads.
-- Hosted Supabase owns auth; its postgres operator cannot delegate schema USAGE.
-- Fail closed if the immediate Auth-user integrity boundary has drifted.
DO $migration$
DECLARE
    runner text := current_user;
    previous_set boolean;
    had_membership boolean;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint c
        WHERE c.conrelid = 'identity.user_tenant_memberships'::regclass
          AND c.confrelid = 'auth.users'::regclass
          AND c.contype = 'f' AND c.convalidated AND NOT c.condeferrable
          AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
              WHERE attrelid = c.conrelid AND attname = 'user_id')]::smallint[]
          AND c.confkey = ARRAY[(SELECT attnum FROM pg_attribute
              WHERE attrelid = c.confrelid AND attname = 'id')]::smallint[]
    ) THEN
        RAISE EXCEPTION 'Validated immediate Auth-user foreign key required';
    END IF;
    IF (SELECT proowner FROM pg_proc WHERE oid =
        'identity.bootstrap_initial_super_admin(uuid,uuid,uuid,text)'::regprocedure)
        <> 'syncam_bootstrap_executor'::regrole THEN
        RAISE EXCEPTION 'Unexpected bootstrap owner';
    END IF;
    IF has_schema_privilege('syncam_bootstrap_executor', 'identity', 'CREATE') THEN
        RAISE EXCEPTION 'Unexpected bootstrap schema-create privilege';
    END IF;
    IF (SELECT count(*) FROM pg_auth_members
        WHERE member = runner::regrole
          AND roleid = 'syncam_bootstrap_executor'::regrole) > 1 THEN
        RAISE EXCEPTION 'Ambiguous migration operator membership';
    END IF;
    SELECT set_option INTO previous_set FROM pg_auth_members
        WHERE member = runner::regrole
          AND roleid = 'syncam_bootstrap_executor'::regrole;
    had_membership := FOUND;

    EXECUTE format('GRANT syncam_bootstrap_executor TO %I WITH SET TRUE', runner);
    GRANT CREATE ON SCHEMA identity TO syncam_bootstrap_executor;
    EXECUTE 'SET LOCAL ROLE syncam_bootstrap_executor';
    EXECUTE $definition$
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

    -- The immediate, validated user_id foreign key checks Auth-user existence
    -- at INSERT (including concurrent deletion), without auth schema access.
    -- An absent user raises 23503 before any audit event can be written.

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
$definition$;
    EXECUTE format('SET LOCAL ROLE %I', runner);
    REVOKE CREATE ON SCHEMA identity FROM syncam_bootstrap_executor;
    IF had_membership THEN
        EXECUTE format('GRANT syncam_bootstrap_executor TO %I WITH SET %s',
            runner, CASE WHEN previous_set THEN 'TRUE' ELSE 'FALSE' END);
    ELSE
        EXECUTE format('REVOKE syncam_bootstrap_executor FROM %I', runner);
    END IF;
END;
$migration$;

-- No new EXECUTE grant: CREATE OR REPLACE retains the existing function ACL.
-- Remove the now-unneeded table read; do not modify Supabase-owned schema ACLs.
REVOKE SELECT ON auth.users FROM syncam_bootstrap_executor;
