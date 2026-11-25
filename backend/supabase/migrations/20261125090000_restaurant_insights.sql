-- ADR-020: anonymous visitor and table insights.
--
-- Two zone kinds that never raise alerts:
--   door_line  a line across an entrance; people crossing it are counted in
--              or out (inside is on the right of the first-to-second point
--              arrow).
--   table      a polygon around a table; the site box measures how long it
--              is occupied and how many seated sessions it has.
-- The site box sends hourly counts only: no pictures, track ids or
-- positions, and nothing that identifies a person.

ALTER TABLE config.zones DROP CONSTRAINT IF EXISTS zones_kind_check;
ALTER TABLE config.zones ADD CONSTRAINT zones_kind_check
    CHECK (kind IN ('intrusion', 'restricted_zone', 'loitering', 'abandoned', 'tripwire', 'door_line', 'table'));

-- One row per zone, hour and site-box run. A box sends running totals for
-- its own run, so a retry replaces the row and runs simply add up. There is
-- no foreign key to config.zones, so history survives a removed zone.
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
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON platform.zone_hours FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.zone_hours TO syncam_app';
    END IF;
END;
$$;
