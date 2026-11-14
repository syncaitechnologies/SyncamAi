-- Staff attendance from consenting, enrolled staff.
--
-- Face templates never reach the cloud: enrollment and matching happen on the
-- site box, which stores templates encrypted on premises. The control plane
-- keeps only the staff roster, who recorded consent and when, and sightings.

CREATE SCHEMA IF NOT EXISTS attendance;

CREATE TABLE IF NOT EXISTS attendance.staff (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    display_name varchar(120) NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    employee_code varchar(64) NOT NULL DEFAULT '',
    consent_recorded_at timestamptz NOT NULL,
    consent_recorded_by varchar(128) NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT staff_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT staff_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS staff_tenant_site_idx ON attendance.staff (tenant_id, site_id, display_name);

CREATE TABLE IF NOT EXISTS attendance.records (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    staff_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    confidence real NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT records_staff_fk FOREIGN KEY (tenant_id, staff_id) REFERENCES attendance.staff (tenant_id, id),
    CONSTRAINT records_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS records_tenant_site_time_idx ON attendance.records (tenant_id, site_id, occurred_at DESC);

ALTER TABLE attendance.staff ENABLE ROW LEVEL SECURITY;
ALTER TABLE attendance.staff FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON attendance.staff;
CREATE POLICY tenant_isolation ON attendance.staff
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE attendance.records ENABLE ROW LEVEL SECURITY;
ALTER TABLE attendance.records FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON attendance.records;
CREATE POLICY tenant_isolation ON attendance.records
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON SCHEMA attendance FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON attendance.staff, attendance.records FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA attendance TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON attendance.staff TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON attendance.records TO syncam_app';
    END IF;
END;
$$;
