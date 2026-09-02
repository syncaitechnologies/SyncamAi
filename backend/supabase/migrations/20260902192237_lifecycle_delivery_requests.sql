-- Durable provider-delivery intents only. A later backend worker owns all
-- external Supabase Admin calls; this table never contains a bearer credential.
CREATE TABLE identity.lifecycle_delivery_requests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id) ON DELETE CASCADE,
    request_id uuid NOT NULL,
    action varchar(24) NOT NULL
        CHECK (action IN ('invite', 'disable', 'reassign')),
    target_user_id uuid,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    status varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivering', 'delivered', 'failed')),
    provider_operation_id varchar(128),
    delivered_at timestamptz,
    last_error varchar(2000),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, request_id),
    CHECK ((status = 'delivered') = (delivered_at IS NOT NULL))
);

CREATE INDEX lifecycle_delivery_requests_pending_idx
    ON identity.lifecycle_delivery_requests (tenant_id, created_at, id)
    WHERE status IN ('pending', 'failed');

ALTER TABLE identity.lifecycle_delivery_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.lifecycle_delivery_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY lifecycle_delivery_requests_tenant_isolation
    ON identity.lifecycle_delivery_requests
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE ALL ON TABLE identity.lifecycle_delivery_requests FROM anon, authenticated;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON identity.lifecycle_delivery_requests TO syncam_app';
    END IF;
END;
$$;
