ALTER TABLE alerts.alerts
    ADD COLUMN IF NOT EXISTS acked_at timestamptz,
    ADD COLUMN IF NOT EXISTS acked_by varchar(128);

CREATE TABLE IF NOT EXISTS alerts.alert_actions (
    action_id uuid NOT NULL,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    alert_id uuid NOT NULL,
    action varchar(32) NOT NULL CHECK (action IN (
        'acknowledge', 'escalate', 'dismiss', 'assign', 'dispatch',
        'snooze', 'mute', 'note', 'resolve'
    )),
    actor_type varchar(16) NOT NULL CHECK (actor_type IN ('user', 'edge', 'system', 'schedule')),
    actor_id varchar(128) NOT NULL,
    request_id uuid NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, action_id),
    UNIQUE (tenant_id, request_id),
    FOREIGN KEY (tenant_id, alert_id) REFERENCES alerts.alerts(tenant_id, alert_id)
);

CREATE INDEX IF NOT EXISTS alert_actions_alert_time_idx
    ON alerts.alert_actions (tenant_id, alert_id, created_at, action_id);

CREATE SCHEMA IF NOT EXISTS syncam_realtime;

CREATE TABLE IF NOT EXISTS syncam_realtime.site_sequences (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, site_id),
    FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites(tenant_id, id)
);

CREATE TABLE IF NOT EXISTS syncam_realtime.messages (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    topic varchar(128) NOT NULL CHECK (topic IN ('alerts.created', 'alerts.state')),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (clock_timestamp() + interval '5 minutes'),
    PRIMARY KEY (tenant_id, site_id, sequence),
    FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites(tenant_id, id)
);

CREATE INDEX IF NOT EXISTS realtime_messages_replay_idx
    ON syncam_realtime.messages (tenant_id, site_id, sequence, expires_at);
CREATE INDEX IF NOT EXISTS realtime_messages_expiry_idx
    ON syncam_realtime.messages (expires_at);

ALTER TABLE alerts.alert_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts.alert_actions FORCE ROW LEVEL SECURITY;
ALTER TABLE syncam_realtime.site_sequences ENABLE ROW LEVEL SECURITY;
ALTER TABLE syncam_realtime.site_sequences FORCE ROW LEVEL SECURITY;
ALTER TABLE syncam_realtime.messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE syncam_realtime.messages FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON alerts.alert_actions;
CREATE POLICY tenant_isolation ON alerts.alert_actions
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON syncam_realtime.site_sequences;
CREATE POLICY tenant_isolation ON syncam_realtime.site_sequences
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON syncam_realtime.messages;
CREATE POLICY tenant_isolation ON syncam_realtime.messages
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION alerts.reject_action_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'alert actions are append-only' USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS alert_actions_append_only ON alerts.alert_actions;
CREATE TRIGGER alert_actions_append_only
    BEFORE UPDATE OR DELETE ON alerts.alert_actions
    FOR EACH ROW EXECUTE FUNCTION alerts.reject_action_mutation();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA syncam_realtime TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT ON alerts.alert_actions TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON syncam_realtime.site_sequences TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, DELETE ON syncam_realtime.messages TO syncam_app';
        EXECUTE 'GRANT UPDATE ON alerts.alerts TO syncam_app';
    END IF;
END;
$$;
