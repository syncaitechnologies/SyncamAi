-- Mirrors the Supabase migration 20261125090000_restaurant_insights.sql:
-- door_line and table zones, and anonymous hourly counts per zone.
ALTER TABLE config.zones DROP CONSTRAINT IF EXISTS zones_kind_check;
ALTER TABLE config.zones ADD CONSTRAINT zones_kind_check
    CHECK (kind IN ('intrusion', 'restricted_zone', 'loitering', 'abandoned', 'tripwire', 'door_line', 'table'));

CREATE TABLE IF NOT EXISTS platform.zone_hours (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    site_id uuid NOT NULL,
    zone_id uuid NOT NULL,
    hour_start timestamptz NOT NULL CHECK (hour_start = date_trunc('hour', hour_start AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
    run_id uuid NOT NULL,
    entries integer NOT NULL DEFAULT 0 CHECK (entries BETWEEN 0 AND 100000),
    exits integer NOT NULL DEFAULT 0 CHECK (exits BETWEEN 0 AND 100000),
    occupied_seconds integer NOT NULL DEFAULT 0 CHECK (occupied_seconds BETWEEN 0 AND 3600),
    sessions integer NOT NULL DEFAULT 0 CHECK (sessions BETWEEN 0 AND 1000),
    session_seconds bigint NOT NULL DEFAULT 0 CHECK (session_seconds BETWEEN 0 AND 86400000),
    peak_people integer NOT NULL DEFAULT 0 CHECK (peak_people BETWEEN 0 AND 1000),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (zone_id, hour_start, run_id)
);
CREATE INDEX IF NOT EXISTS zone_hours_tenant_site_hour_idx ON platform.zone_hours (tenant_id, site_id, hour_start);

ALTER TABLE platform.zone_hours ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.zone_hours FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON platform.zone_hours;
CREATE POLICY tenant_isolation ON platform.zone_hours
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.zone_hours TO syncam_app';
    END IF;
END;
$$;
