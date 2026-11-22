-- ADR-019 feature switches. Mirrors the tables of the Supabase migration
-- 20261123090000_feature_switches.sql so the embedded-migration test database
-- matches. The owner routines and site-key capabilities live only in Supabase,
-- like the rest of the owner admin and site-box schema.
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

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT ON identity.tenant_features TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON config.feature_settings TO syncam_app';
    END IF;
END;
$$;
