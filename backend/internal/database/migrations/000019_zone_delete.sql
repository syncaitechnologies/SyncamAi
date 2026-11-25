-- Mirrors the Supabase migration 20261124090000_zone_delete.sql: the API may
-- remove zones (each removal is audited).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT DELETE ON config.zones TO syncam_app';
    END IF;
END;
$$;
