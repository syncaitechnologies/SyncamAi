CREATE TABLE IF NOT EXISTS config.privacy_mask_requests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    name varchar(120) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    geometry jsonb NOT NULL CHECK (jsonb_typeof(geometry) = 'object'),
    status varchar(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved')),
    requested_by varchar(128) NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    approved_at timestamptz,
    CONSTRAINT privacy_mask_requests_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT privacy_mask_requests_tenant_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id),
    CONSTRAINT privacy_mask_requests_tenant_camera_fk FOREIGN KEY (tenant_id, camera_id)
        REFERENCES config.cameras (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS privacy_mask_requests_tenant_site_idx
    ON config.privacy_mask_requests (tenant_id, site_id, requested_at DESC);

CREATE TABLE IF NOT EXISTS config.privacy_mask_approvals (
    request_id uuid NOT NULL,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    approver_id varchar(128) NOT NULL,
    approved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (request_id, approver_id),
    CONSTRAINT privacy_mask_approvals_tenant_request_fk FOREIGN KEY (tenant_id, request_id)
        REFERENCES config.privacy_mask_requests (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS privacy_mask_approvals_tenant_request_idx
    ON config.privacy_mask_approvals (tenant_id, request_id, approved_at);

ALTER TABLE config.privacy_mask_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.privacy_mask_approvals FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON config.privacy_mask_requests;
CREATE POLICY tenant_isolation ON config.privacy_mask_requests
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON config.privacy_mask_approvals;
CREATE POLICY tenant_isolation ON config.privacy_mask_approvals
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION config.reject_privacy_mask_approval_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'privacy mask approvals are immutable' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS privacy_mask_approvals_immutable ON config.privacy_mask_approvals;
CREATE TRIGGER privacy_mask_approvals_immutable
    BEFORE UPDATE OR DELETE ON config.privacy_mask_approvals
    FOR EACH ROW EXECUTE FUNCTION config.reject_privacy_mask_approval_mutation();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.privacy_mask_requests TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON config.privacy_mask_approvals TO syncam_app';
    END IF;
END;
$$;
