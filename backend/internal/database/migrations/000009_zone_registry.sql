DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'cameras_tenant_id_id_unique' AND conrelid = 'config.cameras'::regclass) THEN
        ALTER TABLE config.cameras ADD CONSTRAINT cameras_tenant_id_id_unique UNIQUE (tenant_id, id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS config.zones (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    camera_id uuid,
    floor varchar(120),
    name varchar(120) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    kind varchar(32) NOT NULL CHECK (kind IN ('intrusion', 'restricted_zone', 'loitering', 'abandoned', 'tripwire')),
    geometry jsonb NOT NULL CHECK (jsonb_typeof(geometry) = 'object'),
    enabled boolean NOT NULL DEFAULT true,
    config_version bigint NOT NULL DEFAULT 1 CHECK (config_version > 0),
    created_by varchar(128) NOT NULL,
    updated_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT zones_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id),
    CONSTRAINT zones_tenant_camera_fk FOREIGN KEY (tenant_id, camera_id) REFERENCES config.cameras (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS zones_tenant_site_idx ON config.zones (tenant_id, site_id, id);
CREATE INDEX IF NOT EXISTS zones_geometry_gin_idx ON config.zones USING gin (geometry);

ALTER TABLE config.zones ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.zones FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON config.zones;
CREATE POLICY tenant_isolation ON config.zones
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.zones TO syncam_app';
    END IF;
END;
$$;
