-- ADR-019: feature switches.
--
-- Two layers decide whether an AI feature runs for a workspace:
--   * included: the platform owner decides which features each workspace's
--     plan has (identity.tenant_features). This is the future billing source.
--     Only the owner's audited routine writes it.
--   * enabled: the workspace's own admins switch included features on or off,
--     for all cameras or chosen ones (config.feature_settings).
-- A missing row means the catalog default (see backend/internal/features).
-- Site boxes report which features they have loaded in
-- platform.site_keys.capabilities.

CREATE TABLE IF NOT EXISTS identity.tenant_features (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    feature varchar(32) NOT NULL CHECK (feature IN ('zone_alerts', 'loitering', 'attendance', 'weapon')),
    included boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, feature)
);
ALTER TABLE identity.tenant_features ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.tenant_features FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON identity.tenant_features;
CREATE POLICY tenant_isolation ON identity.tenant_features
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TABLE IF NOT EXISTS config.feature_settings (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    feature varchar(32) NOT NULL CHECK (feature IN ('zone_alerts', 'loitering', 'attendance', 'weapon', 'live_view')),
    enabled boolean NOT NULL,
    -- NULL = every camera in the workspace.
    camera_ids uuid[] CHECK (camera_ids IS NULL OR cardinality(camera_ids) <= 1024),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_by varchar(200) NOT NULL,
    PRIMARY KEY (tenant_id, feature)
);
ALTER TABLE config.feature_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.feature_settings FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON config.feature_settings;
CREATE POLICY tenant_isolation ON config.feature_settings
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE platform.site_keys
    ADD COLUMN IF NOT EXISTS capabilities jsonb CHECK (capabilities IS NULL OR jsonb_typeof(capabilities) = 'array');

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON identity.tenant_features FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON config.feature_settings FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT ON identity.tenant_features TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.feature_settings TO syncam_app';
    END IF;
END;
$$;

-- Which features each workspace's plan includes, for the owner admin screen.
-- Only rows that differ from the catalog default exist.
CREATE OR REPLACE FUNCTION platform.admin_list_features(p_actor uuid)
RETURNS TABLE (tenant_id uuid, feature text, included boolean)
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
    SELECT f.tenant_id, f.feature::text, f.included
    FROM identity.tenant_features AS f
    ORDER BY f.tenant_id, f.feature;
END;
$$;

-- Set which features a workspace's plan includes. p_changes maps a feature
-- key to true or false; unknown keys and non-boolean values are refused. The
-- change is written to the workspace's hash-chained audit log.
CREATE OR REPLACE FUNCTION platform.admin_set_features(p_actor uuid, p_request_id uuid, p_tenant uuid, p_changes jsonb)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
DECLARE
    v_before jsonb;
    v_after jsonb;
    v_key text;
    v_value jsonb;
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
    FOR v_key, v_value IN SELECT key, value FROM jsonb_each(p_changes) LOOP
        IF v_key NOT IN ('zone_alerts', 'loitering', 'attendance', 'weapon') THEN
            RAISE EXCEPTION 'unknown feature %', v_key USING ERRCODE = '22023';
        END IF;
        IF jsonb_typeof(v_value) IS DISTINCT FROM 'boolean' THEN
            RAISE EXCEPTION 'feature % must be true or false', v_key USING ERRCODE = '22023';
        END IF;
    END LOOP;

    PERFORM set_config('app.tenant_id', p_tenant::text, true);
    PERFORM 1 FROM identity.tenants AS t WHERE t.id = p_tenant FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'workspace not found' USING ERRCODE = 'P0002';
    END IF;
    SELECT coalesce(jsonb_object_agg(f.feature, f.included), '{}'::jsonb) INTO v_before
    FROM identity.tenant_features AS f WHERE f.tenant_id = p_tenant;

    INSERT INTO identity.tenant_features (tenant_id, feature, included, updated_at)
    SELECT p_tenant, c.key, (c.value)::text::boolean, v_occurred_at
    FROM jsonb_each(p_changes) AS c
    ON CONFLICT (tenant_id, feature) DO UPDATE
    SET included = EXCLUDED.included, updated_at = EXCLUDED.updated_at;

    SELECT coalesce(jsonb_object_agg(f.feature, f.included), '{}'::jsonb) INTO v_after
    FROM identity.tenant_features AS f WHERE f.tenant_id = p_tenant;

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
        ',"action":"tenant.features.changed"' ||
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
        'operator:' || p_actor::text, 'tenant.features.changed', 'tenant', p_tenant::text, p_request_id,
        v_before, v_after, v_payload_text::jsonb, convert_to(v_payload_text, 'UTF8'),
        v_previous_hash, v_record_hash
    );
END;
$$;

REVOKE ALL ON FUNCTION platform.admin_list_features(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION platform.admin_set_features(uuid, uuid, uuid, jsonb) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION platform.admin_list_features(uuid) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION platform.admin_set_features(uuid, uuid, uuid, jsonb) FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.admin_list_features(uuid) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.admin_set_features(uuid, uuid, uuid, jsonb) TO syncam_app';
    END IF;
END;
$$;
