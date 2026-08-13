CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS config;
CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS identity.tenants (
    id uuid PRIMARY KEY,
    name varchar(120) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    slug varchar(60) NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    data_region varchar(16) NOT NULL DEFAULT 'ap-south-1',
    tier varchar(16) NOT NULL DEFAULT 'local' CHECK (tier IN ('smb', 'enterprise', 'local')),
    retention_days smallint NOT NULL DEFAULT 30 CHECK (retention_days BETWEEN 7 AND 365),
    biometric_mode varchar(24) NOT NULL DEFAULT 'embeddings_only'
        CHECK (biometric_mode IN ('photos', 'embeddings_only')),
    settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(settings) = 'object'),
    kms_key_arn varchar(255),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE IF NOT EXISTS config.sites (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    name varchar(120) NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    address text,
    timezone varchar(64) NOT NULL,
    region_pin varchar(16) NOT NULL DEFAULT 'ap-south-1',
    status varchar(16) NOT NULL DEFAULT 'provisioning'
        CHECK (status IN ('provisioning', 'active', 'offline', 'retired')),
    settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(settings) = 'object'),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS sites_tenant_status_idx ON config.sites (tenant_id, status, id);

CREATE TABLE IF NOT EXISTS platform.idempotency_keys (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    idempotency_key varchar(128) NOT NULL,
    request_hash char(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    response_status smallint NOT NULL CHECK (response_status BETWEEN 200 AND 299),
    resource_type varchar(64) NOT NULL,
    resource_id uuid NOT NULL,
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body) = 'object'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (clock_timestamp() + interval '24 hours'),
    PRIMARY KEY (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idempotency_expiry_idx ON platform.idempotency_keys (expires_at);

CREATE TABLE IF NOT EXISTS audit.events (
    chain_sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    chain_date date NOT NULL,
    occurred_at timestamptz NOT NULL,
    actor_id varchar(128) NOT NULL,
    action varchar(96) NOT NULL,
    resource_type varchar(64) NOT NULL,
    resource_id varchar(128) NOT NULL,
    request_id uuid NOT NULL,
    before_state jsonb,
    after_state jsonb,
    canonical_payload jsonb NOT NULL CHECK (jsonb_typeof(canonical_payload) = 'object'),
    previous_hash bytea NOT NULL CHECK (octet_length(previous_hash) = 32),
    record_hash bytea NOT NULL CHECK (octet_length(record_hash) = 32)
);

CREATE INDEX IF NOT EXISTS audit_events_tenant_time_idx
    ON audit.events (tenant_id, occurred_at DESC, chain_sequence DESC);
CREATE UNIQUE INDEX IF NOT EXISTS audit_events_tenant_day_hash_idx
    ON audit.events (tenant_id, chain_date, record_hash);

ALTER TABLE identity.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE config.sites ENABLE ROW LEVEL SECURITY;
ALTER TABLE config.sites FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.idempotency_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE audit.events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.events FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON identity.tenants;
CREATE POLICY tenant_isolation ON identity.tenants
    USING (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON config.sites;
CREATE POLICY tenant_isolation ON config.sites
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON platform.idempotency_keys;
CREATE POLICY tenant_isolation ON platform.idempotency_keys
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON audit.events;
CREATE POLICY tenant_isolation ON audit.events
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION audit.reject_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit events are append-only' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS audit_events_append_only ON audit.events;
CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit.events
    FOR EACH ROW EXECUTE FUNCTION audit.reject_event_mutation();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA identity, config, platform, audit TO syncam_app';
        EXECUTE 'GRANT SELECT ON identity.tenants TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON config.sites TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, DELETE ON platform.idempotency_keys TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON audit.events TO syncam_app';
        EXECUTE 'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO syncam_app';
    END IF;
END;
$$;
