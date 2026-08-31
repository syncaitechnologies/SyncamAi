-- Application data access always runs under a role that cannot log in until
-- the local-only bootstrap procedure assigns a password. It must never bypass
-- the transaction-local tenant RLS policies.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        CREATE ROLE syncam_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END;
$$;
