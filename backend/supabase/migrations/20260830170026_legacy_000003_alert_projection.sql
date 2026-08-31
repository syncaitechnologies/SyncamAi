ALTER TABLE messaging.outbox_messages
    ADD COLUMN IF NOT EXISTS lease_owner uuid,
    ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz;

CREATE INDEX IF NOT EXISTS outbox_claim_idx
    ON messaging.outbox_messages (tenant_id, created_at, message_id)
    WHERE published_at IS NULL;

CREATE SCHEMA IF NOT EXISTS alerts;

CREATE TABLE IF NOT EXISTS alerts.alerts (
    alert_id uuid NOT NULL,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    event_id uuid NOT NULL,
    site_id uuid NOT NULL,
    camera_id uuid NOT NULL,
    zone_id uuid NOT NULL,
    event_type varchar(48) NOT NULL,
    severity varchar(16) NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'info')),
    status varchar(24) NOT NULL DEFAULT 'unacknowledged'
        CHECK (status IN ('unacknowledged', 'acknowledged', 'dispatched', 'arrived', 'resolved', 'dismissed', 'snoozed')),
    confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    priority integer NOT NULL CHECK (priority BETWEEN 0 AND 500),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, alert_id),
    UNIQUE (tenant_id, event_id),
    FOREIGN KEY (tenant_id, event_id) REFERENCES events.detection_events(tenant_id, event_id),
    FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites(tenant_id, id)
);

CREATE INDEX IF NOT EXISTS alerts_queue_idx
    ON alerts.alerts (tenant_id, status, priority DESC, occurred_at, alert_id);
CREATE INDEX IF NOT EXISTS alerts_site_queue_idx
    ON alerts.alerts (tenant_id, site_id, status, priority DESC, occurred_at, alert_id);

CREATE TABLE IF NOT EXISTS messaging.consumer_receipts (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    consumer_name varchar(128) NOT NULL,
    message_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, consumer_name, message_id)
);

ALTER TABLE alerts.alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts.alerts FORCE ROW LEVEL SECURITY;
ALTER TABLE messaging.consumer_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE messaging.consumer_receipts FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON alerts.alerts;
CREATE POLICY tenant_isolation ON alerts.alerts
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON messaging.consumer_receipts;
CREATE POLICY tenant_isolation ON messaging.consumer_receipts
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA alerts TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON alerts.alerts TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON messaging.consumer_receipts TO syncam_app';
        EXECUTE 'GRANT UPDATE ON messaging.outbox_messages TO syncam_app';
    END IF;
END;
$$;
