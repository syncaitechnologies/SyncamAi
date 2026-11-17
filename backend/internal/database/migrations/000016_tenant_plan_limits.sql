-- ADR-016 trial plan and limits on tenants. Mirrors the columns added by the
-- Supabase migration 20261117090000_self_service_workspace.sql so the
-- embedded-migration test database matches. Existing tenants stay active with
-- no limits.
ALTER TABLE identity.tenants
    ADD COLUMN IF NOT EXISTS plan varchar(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS trial_ends_at timestamptz,
    ADD COLUMN IF NOT EXISTS site_limit integer,
    ADD COLUMN IF NOT EXISTS camera_limit integer;

ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_plan_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_plan_check
    CHECK (plan IN ('trial', 'active', 'suspended'));
ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_limits_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_limits_check
    CHECK ((site_limit IS NULL OR site_limit >= 0) AND (camera_limit IS NULL OR camera_limit >= 0));
ALTER TABLE identity.tenants DROP CONSTRAINT IF EXISTS tenants_trial_end_check;
ALTER TABLE identity.tenants ADD CONSTRAINT tenants_trial_end_check
    CHECK (plan <> 'trial' OR trial_ends_at IS NOT NULL);
