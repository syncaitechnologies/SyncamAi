-- T-0413 follow-up: GRANT can create a second membership with a different
-- grantor. Restoring SET alone does not remove that grant's default INHERIT.
-- Remove only the migration-created self-grant, preserving the platform's
-- ADMIN-only grant. Never revoke another grantor's membership.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_auth_members
        WHERE roleid = 'syncam_bootstrap_executor'::regrole
          AND member = 'postgres'::regrole AND grantor = 'postgres'::regrole
          AND (admin_option OR set_option)
    ) THEN
        RAISE EXCEPTION 'Unexpected bootstrap self-grant requires operator review';
    END IF;
    REVOKE syncam_bootstrap_executor FROM postgres GRANTED BY postgres;
    IF EXISTS (
        SELECT 1 FROM pg_auth_members
        WHERE roleid = 'syncam_bootstrap_executor'::regrole
          AND member = 'postgres'::regrole AND (inherit_option OR set_option)
    ) THEN
        RAISE EXCEPTION 'Bootstrap operator retains effective executor access';
    END IF;
END;
$$;
