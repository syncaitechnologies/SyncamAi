CREATE TABLE IF NOT EXISTS config.edge_devices (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    serial_number varchar(128) NOT NULL CHECK (length(btrim(serial_number)) BETWEEN 1 AND 128),
    hardware_tier varchar(8) NOT NULL CHECK (hardware_tier IN ('s', 'm', 'l')),
    model varchar(120),
    status varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'offline', 'retired')),
    cert_status varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (cert_status IN ('pending', 'active', 'revoked', 'rotating')),
    activated_at timestamptz,
    last_heartbeat timestamptz,
    created_by varchar(128) NOT NULL,
    updated_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT edge_devices_tenant_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS edge_devices_tenant_serial_unique
    ON config.edge_devices (tenant_id, upper(serial_number));
CREATE INDEX IF NOT EXISTS edge_devices_tenant_site_status_idx
    ON config.edge_devices (tenant_id, site_id, status, id);
CREATE UNIQUE INDEX IF NOT EXISTS edge_devices_tenant_id_unique
    ON config.edge_devices (tenant_id, id);

CREATE TABLE IF NOT EXISTS platform.device_claims (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    device_id uuid NOT NULL,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (statement_timestamp() + interval '24 hours'),
    consumed_at timestamptz,
    CONSTRAINT device_claims_tenant_device_fk FOREIGN KEY (tenant_id, device_id)
        REFERENCES config.edge_devices (tenant_id, id),
    CHECK (expires_at <= created_at + interval '24 hours'),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE INDEX IF NOT EXISTS device_claims_tenant_expiry_idx
    ON platform.device_claims (tenant_id, expires_at) WHERE consumed_at IS NULL;

ALTER TABLE config.edge_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.edge_devices FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_claims FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON config.edge_devices;
CREATE POLICY tenant_isolation ON config.edge_devices
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON platform.device_claims;
CREATE POLICY tenant_isolation ON platform.device_claims
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.edge_devices TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.device_claims TO syncam_app';
    END IF;
END;
$$;
