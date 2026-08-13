CREATE SCHEMA IF NOT EXISTS events;
CREATE SCHEMA IF NOT EXISTS messaging;

CREATE UNIQUE INDEX IF NOT EXISTS sites_tenant_id_unique_idx
    ON config.sites (tenant_id, id);

CREATE TABLE IF NOT EXISTS events.detection_events (
    event_id uuid NOT NULL,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    dedupe_key varchar(256) NOT NULL,
    request_hash char(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    occurred_at timestamptz NOT NULL,
    site_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    zone_id uuid NOT NULL,
    event_type varchar(48) NOT NULL,
    model_version varchar(128) NOT NULL,
    confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_refs) = 'array'),
    requires_human_review boolean NOT NULL CHECK (requires_human_review),
    review_state varchar(16) NOT NULL DEFAULT 'pending' CHECK (review_state IN ('pending', 'acknowledged', 'confirmed', 'dismissed')),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, event_id),
    UNIQUE (tenant_id, dedupe_key),
    FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites(tenant_id, id)
);

CREATE INDEX IF NOT EXISTS detection_events_tenant_site_time_idx
    ON events.detection_events (tenant_id, site_id, occurred_at DESC, event_id);

CREATE TABLE IF NOT EXISTS messaging.outbox_messages (
    message_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    aggregate_type varchar(64) NOT NULL,
    aggregate_id uuid NOT NULL,
    topic varchar(128) NOT NULL,
    partition_key varchar(256) NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    headers jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(headers) = 'object'),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    published_at timestamptz,
    publish_attempts integer NOT NULL DEFAULT 0 CHECK (publish_attempts >= 0),
    last_error text,
    UNIQUE (tenant_id, aggregate_type, aggregate_id, topic)
);

CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
    ON messaging.outbox_messages (created_at, message_id)
    WHERE published_at IS NULL;

ALTER TABLE events.detection_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE events.detection_events FORCE ROW LEVEL SECURITY;
ALTER TABLE messaging.outbox_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE messaging.outbox_messages FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON events.detection_events;
CREATE POLICY tenant_isolation ON events.detection_events
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON messaging.outbox_messages;
CREATE POLICY tenant_isolation ON messaging.outbox_messages
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA events, messaging TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON events.detection_events TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON messaging.outbox_messages TO syncam_app';
    END IF;
END;
$$;
