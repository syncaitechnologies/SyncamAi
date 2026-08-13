CREATE SCHEMA IF NOT EXISTS edge;

ALTER TABLE config.edge_devices
    ADD COLUMN IF NOT EXISTS firmware_version varchar(128),
    ADD COLUMN IF NOT EXISTS store_forward_depth bigint NOT NULL DEFAULT 0
        CHECK (store_forward_depth BETWEEN 0 AND 1000000000),
    ADD COLUMN IF NOT EXISTS uptime_seconds bigint NOT NULL DEFAULT 0
        CHECK (uptime_seconds BETWEEN 0 AND 3155760000);

CREATE INDEX IF NOT EXISTS edge_devices_tenant_heartbeat_idx
    ON config.edge_devices (tenant_id, last_heartbeat, id);

CREATE TABLE IF NOT EXISTS edge.device_heartbeats (
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    device_id uuid NOT NULL,
    heartbeat_id uuid NOT NULL,
    request_hash char(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    reported_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    uptime_seconds bigint NOT NULL CHECK (uptime_seconds BETWEEN 0 AND 3155760000),
    store_forward_depth bigint NOT NULL CHECK (store_forward_depth BETWEEN 0 AND 1000000000),
    firmware_version varchar(128) NOT NULL,
    PRIMARY KEY (tenant_id, device_id, heartbeat_id),
    CONSTRAINT device_heartbeats_tenant_device_fk FOREIGN KEY (tenant_id, device_id)
        REFERENCES config.edge_devices (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS device_heartbeats_retention_idx
    ON edge.device_heartbeats (observed_at);

ALTER TABLE edge.device_heartbeats ENABLE ROW LEVEL SECURITY;
ALTER TABLE edge.device_heartbeats FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON edge.device_heartbeats;
CREATE POLICY tenant_isolation ON edge.device_heartbeats
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE OR REPLACE FUNCTION edge.record_device_heartbeat(
    p_device_id uuid,
    p_heartbeat_id uuid,
    p_request_hash char(64),
    p_reported_at timestamptz,
    p_uptime_seconds bigint,
    p_store_forward_depth bigint,
    p_firmware_version varchar(128)
)
RETURNS TABLE (
    device_id uuid,
    tenant_id uuid,
    site_id uuid,
    serial_number varchar(128),
    hardware_tier varchar(8),
    model varchar(120),
    device_status varchar(16),
    certificate_status varchar(16),
    firmware_version varchar(128),
    store_forward_depth bigint,
    uptime_seconds bigint,
    last_heartbeat timestamptz,
    activated_at timestamptz,
    created_at timestamptz,
    updated_at timestamptz,
    observed_at timestamptz,
    replayed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    v_tenant_id uuid;
    v_certificate_status varchar(16);
    v_device_status varchar(16);
    v_stored_hash char(64);
    v_observed_at timestamptz;
    v_inserted boolean := false;
BEGIN
    SELECT d.tenant_id, d.cert_status, d.status
      INTO v_tenant_id, v_certificate_status, v_device_status
      FROM config.edge_devices d
     WHERE d.id = p_device_id;

    IF NOT FOUND OR v_certificate_status <> 'active' OR v_device_status NOT IN ('active', 'offline') THEN
        RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000';
    END IF;

    INSERT INTO edge.device_heartbeats AS inserted (
        tenant_id, device_id, heartbeat_id, request_hash, reported_at,
        uptime_seconds, store_forward_depth, firmware_version
    ) VALUES (
        v_tenant_id, p_device_id, p_heartbeat_id, p_request_hash, p_reported_at,
        p_uptime_seconds, p_store_forward_depth, p_firmware_version
    )
    ON CONFLICT (tenant_id, device_id, heartbeat_id) DO NOTHING
    RETURNING inserted.observed_at INTO v_observed_at;

    v_inserted := FOUND;
    IF NOT v_inserted THEN
        SELECT h.request_hash, h.observed_at
          INTO v_stored_hash, v_observed_at
          FROM edge.device_heartbeats h
         WHERE h.tenant_id = v_tenant_id
           AND h.device_id = p_device_id
           AND h.heartbeat_id = p_heartbeat_id;
        IF v_stored_hash <> p_request_hash THEN
            RAISE EXCEPTION 'heartbeat identifier conflict' USING ERRCODE = '23505';
        END IF;
    ELSE
        UPDATE config.edge_devices d
           SET status = 'active',
               firmware_version = p_firmware_version,
               store_forward_depth = p_store_forward_depth,
               uptime_seconds = p_uptime_seconds,
               last_heartbeat = v_observed_at,
               updated_at = v_observed_at,
               updated_by = 'device:' || p_device_id::text
         WHERE d.id = p_device_id;
    END IF;

    RETURN QUERY
    SELECT d.id, d.tenant_id, d.site_id, d.serial_number, d.hardware_tier,
           d.model, d.status, d.cert_status, d.firmware_version,
           d.store_forward_depth, d.uptime_seconds, d.last_heartbeat,
           d.activated_at, d.created_at, d.updated_at, v_observed_at, NOT v_inserted
      FROM config.edge_devices d
     WHERE d.id = p_device_id;
END;
$$;

REVOKE ALL ON FUNCTION edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128)) FROM PUBLIC;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA edge TO syncam_app';
        EXECUTE 'GRANT SELECT ON edge.device_heartbeats TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128)) TO syncam_app';
    END IF;
END;
$$;
