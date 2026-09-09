-- Credential-free deployment role. Login/password provisioning is a separate
-- private operation, never a password literal in migration history.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app'
          AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
          AND NOT rolreplication AND NOT rolbypassrls AND NOT rolcanlogin
    ) THEN
        RAISE EXCEPTION 'A restricted syncam_app role is required';
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_auth_members WHERE member = 'syncam_app'::regrole
    ) OR EXISTS (
        SELECT 1 FROM pg_shdepend
        WHERE refclassid = 'pg_authid'::regclass
          AND refobjid IN (SELECT oid FROM pg_roles WHERE rolname IN ('syncam_app', 'syncam_render'))
          AND deptype = 'o'
    ) OR EXISTS (
        SELECT 1 FROM pg_shdepend
        WHERE refclassid = 'pg_authid'::regclass
          AND refobjid = (SELECT oid FROM pg_roles WHERE rolname = 'syncam_render') AND deptype = 'a'
          AND classid <> 'pg_database'::regclass
    ) THEN
        RAISE EXCEPTION 'Application ownership or direct runtime grants need operator review';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_render') THEN
        CREATE ROLE syncam_render NOLOGIN INHERIT NOSUPERUSER NOCREATEDB
            NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 20;
    ELSIF EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'syncam_render'
          AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication
               OR rolbypassrls OR NOT rolinherit OR rolconnlimit <> 20)
    ) OR EXISTS (
        SELECT 1 FROM pg_auth_members m JOIN pg_roles p ON p.oid = m.roleid
        WHERE m.member = 'syncam_render'::regrole AND p.rolname <> 'syncam_app'
    ) THEN
        RAISE EXCEPTION 'Existing syncam_render privileges need operator review';
    END IF;

    EXECUTE format('GRANT CONNECT ON DATABASE %I TO syncam_render', current_database());
END;
$$;

GRANT syncam_app TO syncam_render WITH INHERIT TRUE;
GRANT syncam_app TO syncam_render WITH SET FALSE;
GRANT syncam_app TO syncam_render WITH ADMIN FALSE;
