CREATE TABLE IF NOT EXISTS config.configuration_revisions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    content_hash char(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT configuration_revisions_tenant_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id),
    CONSTRAINT configuration_revisions_tenant_site_revision_unique UNIQUE (tenant_id, site_id, revision)
);

CREATE INDEX IF NOT EXISTS configuration_revisions_tenant_site_idx
    ON config.configuration_revisions (tenant_id, site_id, revision DESC);

CREATE TABLE IF NOT EXISTS config.device_configuration_statuses (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    device_id uuid NOT NULL,
    site_id uuid NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    state varchar(16) NOT NULL CHECK (state IN ('applied', 'failed')),
    error_message varchar(512),
    reported_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    applied_at timestamptz,
    PRIMARY KEY (tenant_id, device_id),
    CONSTRAINT device_configuration_statuses_device_fk FOREIGN KEY (tenant_id, device_id)
        REFERENCES config.edge_devices (tenant_id, id),
    CONSTRAINT device_configuration_statuses_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id),
    CHECK ((state = 'applied' AND error_message IS NULL AND applied_at IS NOT NULL)
        OR (state = 'failed' AND error_message IS NOT NULL AND applied_at IS NULL))
);

CREATE INDEX IF NOT EXISTS device_configuration_statuses_tenant_site_idx
    ON config.device_configuration_statuses (tenant_id, site_id, revision DESC);

ALTER TABLE config.configuration_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.configuration_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE config.device_configuration_statuses ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.device_configuration_statuses FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON config.configuration_revisions;
CREATE POLICY tenant_isolation ON config.configuration_revisions
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON config.device_configuration_statuses;
CREATE POLICY tenant_isolation ON config.device_configuration_statuses
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION edge.pull_device_configuration(p_device_id uuid, p_after_revision bigint)
RETURNS TABLE (
    revision_id uuid,
    tenant_id uuid,
    site_id uuid,
    revision bigint,
    payload jsonb,
    content_hash char(64),
    created_by varchar(128),
    created_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    v_tenant_id uuid;
    v_site_id uuid;
BEGIN
    SELECT d.tenant_id, d.site_id INTO v_tenant_id, v_site_id
      FROM config.edge_devices d
     WHERE d.id = p_device_id AND d.cert_status = 'active' AND d.status IN ('active', 'offline');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000';
    END IF;
    RETURN QUERY
    SELECT r.id, r.tenant_id, r.site_id, r.revision, r.payload, r.content_hash, r.created_by, r.created_at
      FROM config.configuration_revisions r
     WHERE r.tenant_id = v_tenant_id AND r.site_id = v_site_id AND r.revision > GREATEST(p_after_revision, 0)
     ORDER BY r.revision DESC
     LIMIT 1;
END;
$$;

CREATE OR REPLACE FUNCTION edge.desired_device_configuration_revision(p_device_id uuid)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    v_tenant_id uuid;
    v_site_id uuid;
BEGIN
    SELECT d.tenant_id, d.site_id INTO v_tenant_id, v_site_id
      FROM config.edge_devices d
     WHERE d.id = p_device_id AND d.cert_status = 'active' AND d.status IN ('active', 'offline');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000';
    END IF;
    RETURN COALESCE((SELECT MAX(r.revision) FROM config.configuration_revisions r WHERE r.tenant_id = v_tenant_id AND r.site_id = v_site_id), 0);
END;
$$;

CREATE OR REPLACE FUNCTION edge.report_device_configuration(
    p_device_id uuid,
    p_revision bigint,
    p_state varchar(16),
    p_error_message varchar(512)
)
RETURNS TABLE (
    device_id uuid,
    tenant_id uuid,
    site_id uuid,
    revision bigint,
    state varchar(16),
    error_message varchar(512),
    reported_at timestamptz,
    applied_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    v_tenant_id uuid;
    v_site_id uuid;
BEGIN
    SELECT d.tenant_id, d.site_id INTO v_tenant_id, v_site_id
      FROM config.edge_devices d
     WHERE d.id = p_device_id AND d.cert_status = 'active' AND d.status IN ('active', 'offline');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000';
    END IF;
    IF p_state NOT IN ('applied', 'failed') OR p_revision < 1
       OR (p_state = 'applied' AND p_error_message IS NOT NULL)
       OR (p_state = 'failed' AND (p_error_message IS NULL OR length(btrim(p_error_message)) = 0)) THEN
        RAISE EXCEPTION 'configuration status is invalid' USING ERRCODE = '22023';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM config.configuration_revisions r
         WHERE r.tenant_id = v_tenant_id AND r.site_id = v_site_id AND r.revision = p_revision
    ) THEN
        RAISE EXCEPTION 'configuration revision is not available to device' USING ERRCODE = '22023';
    END IF;
    IF EXISTS (
        SELECT 1 FROM config.device_configuration_statuses s
         WHERE s.tenant_id = v_tenant_id AND s.device_id = p_device_id AND s.revision > p_revision
    ) THEN
        RAISE EXCEPTION 'configuration outcome is stale' USING ERRCODE = '22023';
    END IF;
    INSERT INTO config.device_configuration_statuses AS s (
        tenant_id, device_id, site_id, revision, state, error_message, reported_at, applied_at
    ) VALUES (
        v_tenant_id, p_device_id, v_site_id, p_revision, p_state,
        CASE WHEN p_state = 'failed' THEN btrim(p_error_message) ELSE NULL END,
        clock_timestamp(), CASE WHEN p_state = 'applied' THEN clock_timestamp() ELSE NULL END
    ) ON CONFLICT (tenant_id, device_id) DO UPDATE SET
        revision = EXCLUDED.revision,
        state = EXCLUDED.state,
        error_message = EXCLUDED.error_message,
        reported_at = EXCLUDED.reported_at,
        applied_at = EXCLUDED.applied_at;
    RETURN QUERY SELECT s.device_id, s.tenant_id, s.site_id, s.revision, s.state, s.error_message, s.reported_at, s.applied_at
      FROM config.device_configuration_statuses s WHERE s.tenant_id = v_tenant_id AND s.device_id = p_device_id;
END;
$$;

REVOKE ALL ON FUNCTION edge.pull_device_configuration(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION edge.desired_device_configuration_revision(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION edge.report_device_configuration(uuid, bigint, varchar, varchar) FROM PUBLIC;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT ON config.configuration_revisions TO syncam_app';
        EXECUTE 'GRANT SELECT ON config.device_configuration_statuses TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION edge.pull_device_configuration(uuid, bigint) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION edge.desired_device_configuration_revision(uuid) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION edge.report_device_configuration(uuid, bigint, varchar, varchar) TO syncam_app';
    END IF;
END;
$$;
