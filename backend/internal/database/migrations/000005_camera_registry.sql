DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'sites_tenant_id_id_unique'
          AND conrelid = 'config.sites'::regclass
    ) THEN
        ALTER TABLE config.sites
            ADD CONSTRAINT sites_tenant_id_id_unique UNIQUE (tenant_id, id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS config.cameras (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    serial_number varchar(128) NOT NULL CHECK (length(btrim(serial_number)) BETWEEN 1 AND 128),
    name varchar(120) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    group_name varchar(120),
    tags text[] NOT NULL DEFAULT '{}'::text[] CHECK (cardinality(tags) <= 32),
    lifecycle_status varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (lifecycle_status IN ('pending', 'active', 'offline', 'retired')),
    config_version bigint NOT NULL DEFAULT 1 CHECK (config_version > 0),
    created_by varchar(128) NOT NULL,
    updated_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT cameras_tenant_site_fk FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS cameras_tenant_serial_unique
    ON config.cameras (tenant_id, upper(serial_number));
CREATE INDEX IF NOT EXISTS cameras_tenant_site_status_idx
    ON config.cameras (tenant_id, site_id, lifecycle_status, id);

ALTER TABLE config.cameras ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.cameras FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON config.cameras;
CREATE POLICY tenant_isolation ON config.cameras
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.cameras TO syncam_app';
    END IF;
END;
$$;
