-- Zones can be removed from the dashboard (Zones & rules > Remove). The API
-- deletes the row under the workspace's row-level security and writes the
-- removed zone to the hash-chained audit log in the same transaction. Past
-- alerts keep their zone ID; nothing references config.zones by key.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT DELETE ON config.zones TO syncam_app';
    END IF;
END;
$$;
