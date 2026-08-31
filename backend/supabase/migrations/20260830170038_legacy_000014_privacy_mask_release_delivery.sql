CREATE TABLE IF NOT EXISTS config.privacy_mask_release_manifests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    request_id uuid NOT NULL,
    device_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    candidate jsonb NOT NULL CHECK (jsonb_typeof(candidate) = 'object'),
    pipeline jsonb NOT NULL CHECK (jsonb_typeof(pipeline) = 'object'),
    hil_evidence jsonb NOT NULL CHECK (jsonb_typeof(hil_evidence) = 'object'),
    candidate_hash char(64) NOT NULL CHECK (candidate_hash ~ '^[0-9a-f]{64}$'),
    evidence_hash char(64) NOT NULL CHECK (evidence_hash ~ '^[0-9a-f]{64}$'),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT privacy_mask_release_manifests_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT privacy_mask_release_manifests_tenant_release_device_version_unique
        UNIQUE (tenant_id, id, device_id, version),
    CONSTRAINT privacy_mask_release_manifests_tenant_device_version_unique
        UNIQUE (tenant_id, device_id, version),
    CONSTRAINT privacy_mask_release_manifests_tenant_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id),
    CONSTRAINT privacy_mask_release_manifests_tenant_camera_fk FOREIGN KEY (tenant_id, camera_id)
        REFERENCES config.cameras (tenant_id, id),
    CONSTRAINT privacy_mask_release_manifests_tenant_request_fk FOREIGN KEY (tenant_id, request_id)
        REFERENCES config.privacy_mask_requests (tenant_id, id),
    CONSTRAINT privacy_mask_release_manifests_tenant_device_fk FOREIGN KEY (tenant_id, device_id)
        REFERENCES config.edge_devices (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS privacy_mask_release_manifests_tenant_device_version_idx
    ON config.privacy_mask_release_manifests (tenant_id, device_id, version DESC);
CREATE INDEX IF NOT EXISTS privacy_mask_release_manifests_tenant_request_idx
    ON config.privacy_mask_release_manifests (tenant_id, request_id);
CREATE INDEX IF NOT EXISTS privacy_mask_release_manifests_tenant_camera_idx
    ON config.privacy_mask_release_manifests (tenant_id, camera_id);

CREATE TABLE IF NOT EXISTS config.privacy_mask_release_statuses (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    device_id uuid NOT NULL,
    release_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    state varchar(16) NOT NULL CHECK (state IN ('accepted', 'failed')),
    error_code varchar(64),
    reported_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    accepted_at timestamptz,
    PRIMARY KEY (tenant_id, device_id),
    CONSTRAINT privacy_mask_release_statuses_tenant_device_fk FOREIGN KEY (tenant_id, device_id)
        REFERENCES config.edge_devices (tenant_id, id),
    CONSTRAINT privacy_mask_release_statuses_tenant_release_device_version_fk
        FOREIGN KEY (tenant_id, release_id, device_id, version)
        REFERENCES config.privacy_mask_release_manifests (tenant_id, id, device_id, version),
    CHECK ((state = 'accepted' AND error_code IS NULL AND accepted_at IS NOT NULL)
        OR (state = 'failed' AND length(btrim(error_code)) > 0 AND accepted_at IS NULL))
);

CREATE INDEX IF NOT EXISTS privacy_mask_release_statuses_tenant_release_idx
    ON config.privacy_mask_release_statuses (tenant_id, release_id);

ALTER TABLE config.privacy_mask_release_manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_release_manifests FORCE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_release_statuses ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_release_statuses FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON config.privacy_mask_release_manifests;
CREATE POLICY tenant_isolation ON config.privacy_mask_release_manifests
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON config.privacy_mask_release_statuses;
CREATE POLICY tenant_isolation ON config.privacy_mask_release_statuses
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION config.reject_privacy_mask_release_manifest_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'privacy mask release manifests are immutable' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS privacy_mask_release_manifests_immutable
    ON config.privacy_mask_release_manifests;
CREATE TRIGGER privacy_mask_release_manifests_immutable
    BEFORE UPDATE OR DELETE ON config.privacy_mask_release_manifests
    FOR EACH ROW EXECUTE FUNCTION config.reject_privacy_mask_release_manifest_mutation();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT ON config.privacy_mask_release_manifests TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.privacy_mask_release_statuses TO syncam_app';
    END IF;
END;
$$;
