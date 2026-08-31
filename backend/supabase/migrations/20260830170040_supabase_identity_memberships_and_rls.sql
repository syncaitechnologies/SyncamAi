-- Supabase Auth owns users. SyncCam owns the tenant/site authorizations that
-- are copied into trusted app_metadata.syncam at token issue time.
CREATE TABLE IF NOT EXISTS identity.user_tenant_memberships (
    user_id uuid PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    roles text[] NOT NULL CHECK (cardinality(roles) > 0),
    scopes text[] NOT NULL CHECK (cardinality(scopes) > 0),
    data_classes text[] NOT NULL CHECK (cardinality(data_classes) > 0),
    status varchar(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS user_tenant_memberships_tenant_lookup_idx
    ON identity.user_tenant_memberships (tenant_id, status, user_id);

CREATE TABLE IF NOT EXISTS identity.user_site_memberships (
    user_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    site_id uuid NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, site_id),
    FOREIGN KEY (user_id, tenant_id)
        REFERENCES identity.user_tenant_memberships(user_id, tenant_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, site_id)
        REFERENCES config.sites(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS user_site_memberships_user_status_idx
    ON identity.user_site_memberships (user_id, status, tenant_id, site_id);

ALTER TABLE identity.user_tenant_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.user_tenant_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE identity.user_site_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.user_site_memberships FORCE ROW LEVEL SECURITY;

CREATE POLICY user_tenant_membership_self_read ON identity.user_tenant_memberships
    FOR SELECT TO authenticated
    USING (user_id = (select auth.uid()) AND status = 'active');

CREATE POLICY user_site_membership_self_read ON identity.user_site_memberships
    FOR SELECT TO authenticated
    USING (
        user_id = (select auth.uid())
        AND status = 'active'
        AND EXISTS (
            SELECT 1
            FROM identity.user_tenant_memberships membership
            WHERE membership.user_id = identity.user_site_memberships.user_id
              AND membership.tenant_id = identity.user_site_memberships.tenant_id
              AND membership.status = 'active'
        )
    );

CREATE POLICY supabase_auth_admin_tenant_membership_read ON identity.user_tenant_memberships
    FOR SELECT TO supabase_auth_admin USING (true);
CREATE POLICY supabase_auth_admin_site_membership_read ON identity.user_site_memberships
    FOR SELECT TO supabase_auth_admin USING (true);

CREATE POLICY authenticated_tenant_read ON identity.tenants
    FOR SELECT TO authenticated
    USING (
        EXISTS (
            SELECT 1 FROM identity.user_tenant_memberships membership
            WHERE membership.user_id = (select auth.uid())
              AND membership.tenant_id = identity.tenants.id
              AND membership.status = 'active'
        )
    );

CREATE POLICY authenticated_assigned_site_read ON config.sites
    FOR SELECT TO authenticated
    USING (
        EXISTS (
            SELECT 1
            FROM identity.user_site_memberships membership
            WHERE membership.user_id = (select auth.uid())
              AND membership.tenant_id = config.sites.tenant_id
              AND membership.site_id = config.sites.id
              AND membership.status = 'active'
        )
    );

REVOKE ALL ON SCHEMA identity, config, platform, audit, alerts, events, syncam_realtime FROM anon, authenticated;
GRANT USAGE ON SCHEMA identity, config TO authenticated;
GRANT SELECT ON identity.tenants, identity.user_tenant_memberships, identity.user_site_memberships, config.sites TO authenticated;

-- Browser clients receive only membership/context reads. All business writes,
-- audit writes and outbox access remain available only through the Go API.
REVOKE ALL ON ALL TABLES IN SCHEMA platform, audit, events, alerts, syncam_realtime FROM anon, authenticated;

CREATE OR REPLACE FUNCTION identity.syncam_custom_access_token(event jsonb)
RETURNS jsonb
LANGUAGE plpgsql
SET search_path = ''
AS $$
DECLARE
    membership identity.user_tenant_memberships%ROWTYPE;
    allowed_site_ids uuid[];
    claims jsonb;
BEGIN
    SELECT * INTO membership
    FROM identity.user_tenant_memberships
    WHERE user_id = (event->>'user_id')::uuid
      AND status = 'active';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'SyncCam user is not provisioned or active' USING ERRCODE = '42501';
    END IF;

    SELECT coalesce(array_agg(site_id ORDER BY site_id), ARRAY[]::uuid[])
    INTO allowed_site_ids
    FROM identity.user_site_memberships
    WHERE user_id = membership.user_id
      AND tenant_id = membership.tenant_id
      AND status = 'active';

    claims := jsonb_set(
        event->'claims',
        '{app_metadata,syncam}',
        jsonb_build_object(
            'tenant_id', membership.tenant_id,
            'site_ids', to_jsonb(allowed_site_ids),
            'roles', to_jsonb(membership.roles),
            'scopes', to_jsonb(membership.scopes),
            'data_class', to_jsonb(membership.data_classes)
        ),
        true
    );
    RETURN jsonb_build_object('claims', claims);
END;
$$;

REVOKE ALL ON FUNCTION identity.syncam_custom_access_token(jsonb) FROM PUBLIC, anon, authenticated;
GRANT USAGE ON SCHEMA identity TO supabase_auth_admin;
GRANT SELECT ON identity.user_tenant_memberships, identity.user_site_memberships TO supabase_auth_admin;
GRANT EXECUTE ON FUNCTION identity.syncam_custom_access_token(jsonb) TO supabase_auth_admin;
