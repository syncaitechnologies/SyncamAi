-- The Go lifecycle boundary needs narrowly scoped access to suspend a tenant
-- membership and its site memberships before it queues provider revocation.
-- Browser roles retain read-only membership access.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, UPDATE ON identity.user_tenant_memberships TO syncam_app';
        EXECUTE 'GRANT SELECT, UPDATE ON identity.user_site_memberships TO syncam_app';
    END IF;
END;
$$;
