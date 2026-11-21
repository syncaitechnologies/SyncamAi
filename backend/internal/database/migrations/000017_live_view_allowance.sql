-- ADR-017 live-view allowance and monthly usage. Mirrors the Supabase
-- migration 20261120090000_live_view_allowance.sql so the embedded-migration
-- test database matches.
ALTER TABLE identity.tenants
    ADD COLUMN IF NOT EXISTS live_view_minutes integer DEFAULT 10;
ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_live_view_minutes_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_live_view_minutes_check
    CHECK (live_view_minutes IS NULL OR live_view_minutes >= 0);

UPDATE identity.tenants SET live_view_minutes = NULL WHERE plan = 'active';

CREATE TABLE IF NOT EXISTS platform.live_view_usage (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    month date NOT NULL CHECK (month = date_trunc('month', month)::date),
    seconds_used bigint NOT NULL DEFAULT 0 CHECK (seconds_used >= 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, month)
);

ALTER TABLE platform.live_view_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.live_view_usage FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON platform.live_view_usage;
CREATE POLICY tenant_isolation ON platform.live_view_usage
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.live_view_usage TO syncam_app';
    END IF;
END;
$$;
