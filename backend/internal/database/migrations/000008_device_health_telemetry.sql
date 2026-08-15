ALTER TABLE config.edge_devices
    ADD COLUMN IF NOT EXISTS cpu_utilization_percent double precision
        CHECK (cpu_utilization_percent BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS gpu_utilization_percent double precision
        CHECK (gpu_utilization_percent BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS temperature_celsius double precision
        CHECK (temperature_celsius BETWEEN -40 AND 150),
    ADD COLUMN IF NOT EXISTS inference_latency_ms double precision
        CHECK (inference_latency_ms BETWEEN 0 AND 600000),
    ADD COLUMN IF NOT EXISTS thermal_state varchar(16)
        CHECK (thermal_state IN ('normal', 'warning', 'critical'));

ALTER TABLE edge.device_heartbeats
    ADD COLUMN IF NOT EXISTS cpu_utilization_percent double precision
        CHECK (cpu_utilization_percent BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS gpu_utilization_percent double precision
        CHECK (gpu_utilization_percent BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS temperature_celsius double precision
        CHECK (temperature_celsius BETWEEN -40 AND 150),
    ADD COLUMN IF NOT EXISTS inference_latency_ms double precision
        CHECK (inference_latency_ms BETWEEN 0 AND 600000),
    ADD COLUMN IF NOT EXISTS thermal_state varchar(16)
        CHECK (thermal_state IN ('normal', 'warning', 'critical'));

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'edge_devices_health_complete' AND conrelid = 'config.edge_devices'::regclass) THEN
        ALTER TABLE config.edge_devices ADD CONSTRAINT edge_devices_health_complete CHECK (
            (cpu_utilization_percent IS NULL AND gpu_utilization_percent IS NULL AND temperature_celsius IS NULL AND inference_latency_ms IS NULL AND thermal_state IS NULL)
            OR
            (cpu_utilization_percent IS NOT NULL AND gpu_utilization_percent IS NOT NULL AND temperature_celsius IS NOT NULL AND inference_latency_ms IS NOT NULL
                AND thermal_state = CASE WHEN temperature_celsius >= 90 THEN 'critical' WHEN temperature_celsius >= 80 THEN 'warning' ELSE 'normal' END)
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'device_heartbeats_health_complete' AND conrelid = 'edge.device_heartbeats'::regclass) THEN
        ALTER TABLE edge.device_heartbeats ADD CONSTRAINT device_heartbeats_health_complete CHECK (
            (cpu_utilization_percent IS NULL AND gpu_utilization_percent IS NULL AND temperature_celsius IS NULL AND inference_latency_ms IS NULL AND thermal_state IS NULL)
            OR
            (cpu_utilization_percent IS NOT NULL AND gpu_utilization_percent IS NOT NULL AND temperature_celsius IS NOT NULL AND inference_latency_ms IS NOT NULL
                AND thermal_state = CASE WHEN temperature_celsius >= 90 THEN 'critical' WHEN temperature_celsius >= 80 THEN 'warning' ELSE 'normal' END)
        );
    END IF;
END;
$$;

DROP FUNCTION IF EXISTS edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128));
DROP FUNCTION IF EXISTS edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128), double precision, double precision, double precision, double precision, varchar(16));

CREATE FUNCTION edge.record_device_heartbeat(
    p_device_id uuid,
    p_heartbeat_id uuid,
    p_request_hash char(64),
    p_reported_at timestamptz,
    p_uptime_seconds bigint,
    p_store_forward_depth bigint,
    p_firmware_version varchar(128),
    p_cpu_utilization_percent double precision,
    p_gpu_utilization_percent double precision,
    p_temperature_celsius double precision,
    p_inference_latency_ms double precision,
    p_thermal_state varchar(16)
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
    cpu_utilization_percent double precision,
    gpu_utilization_percent double precision,
    temperature_celsius double precision,
    inference_latency_ms double precision,
    thermal_state varchar(16),
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
        uptime_seconds, store_forward_depth, firmware_version,
        cpu_utilization_percent, gpu_utilization_percent, temperature_celsius,
        inference_latency_ms, thermal_state
    ) VALUES (
        v_tenant_id, p_device_id, p_heartbeat_id, p_request_hash, p_reported_at,
        p_uptime_seconds, p_store_forward_depth, p_firmware_version,
        p_cpu_utilization_percent, p_gpu_utilization_percent, p_temperature_celsius,
        p_inference_latency_ms, p_thermal_state
    )
    ON CONFLICT ON CONSTRAINT device_heartbeats_pkey DO NOTHING
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
               cpu_utilization_percent = COALESCE(p_cpu_utilization_percent, d.cpu_utilization_percent),
               gpu_utilization_percent = COALESCE(p_gpu_utilization_percent, d.gpu_utilization_percent),
               temperature_celsius = COALESCE(p_temperature_celsius, d.temperature_celsius),
               inference_latency_ms = COALESCE(p_inference_latency_ms, d.inference_latency_ms),
               thermal_state = COALESCE(p_thermal_state, d.thermal_state),
               last_heartbeat = v_observed_at,
               updated_at = v_observed_at,
               updated_by = 'device:' || p_device_id::text
         WHERE d.id = p_device_id;
    END IF;

    RETURN QUERY
    SELECT d.id, d.tenant_id, d.site_id, d.serial_number, d.hardware_tier,
           d.model, d.status, d.cert_status, d.firmware_version,
           d.store_forward_depth, d.uptime_seconds, d.cpu_utilization_percent,
           d.gpu_utilization_percent, d.temperature_celsius, d.inference_latency_ms,
           d.thermal_state, d.last_heartbeat, d.activated_at, d.created_at,
           d.updated_at, v_observed_at, NOT v_inserted
      FROM config.edge_devices d
     WHERE d.id = p_device_id;
END;
$$;

REVOKE ALL ON FUNCTION edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128), double precision, double precision, double precision, double precision, varchar(16)) FROM PUBLIC;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION edge.record_device_heartbeat(uuid, uuid, char(64), timestamptz, bigint, bigint, varchar(128), double precision, double precision, double precision, double precision, varchar(16)) TO syncam_app';
    END IF;
END;
$$;
