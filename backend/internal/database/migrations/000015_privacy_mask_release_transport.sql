CREATE OR REPLACE FUNCTION edge.pull_privacy_mask_release(p_device_id uuid, p_after_version bigint)
RETURNS TABLE (release_id uuid, tenant_id uuid, site_id uuid, camera_id uuid, request_id uuid, device_id uuid, version bigint, candidate jsonb, pipeline jsonb, hil_evidence jsonb, candidate_hash char(64), evidence_hash char(64), created_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_tenant_id uuid;
BEGIN
    SELECT d.tenant_id INTO v_tenant_id FROM config.edge_devices d WHERE d.id = p_device_id AND d.status IN ('active', 'offline') AND d.cert_status = 'active';
    IF NOT FOUND THEN RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000'; END IF;
    RETURN QUERY SELECT r.id, r.tenant_id, r.site_id, r.camera_id, r.request_id, r.device_id, r.version, r.candidate, r.pipeline, r.hil_evidence, r.candidate_hash, r.evidence_hash, r.created_at FROM config.privacy_mask_release_manifests r WHERE r.tenant_id = v_tenant_id AND r.device_id = p_device_id AND r.version > GREATEST(p_after_version, 0) ORDER BY r.version ASC LIMIT 1;
END; $$;

CREATE OR REPLACE FUNCTION edge.report_privacy_mask_release(p_device_id uuid, p_release_id uuid, p_version bigint, p_state varchar(16), p_error_code varchar(64))
RETURNS TABLE (tenant_id uuid, device_id uuid, release_id uuid, version bigint, state varchar(16), error_code varchar(64), reported_at timestamptz, accepted_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_tenant_id uuid;
BEGIN
    SELECT d.tenant_id INTO v_tenant_id FROM config.edge_devices d WHERE d.id = p_device_id AND d.status IN ('active', 'offline') AND d.cert_status = 'active';
    IF NOT FOUND THEN RAISE EXCEPTION 'device is not authorized' USING ERRCODE = '28000'; END IF;
    IF p_version < 1 OR p_state NOT IN ('accepted', 'failed') OR (p_state = 'accepted' AND p_error_code IS NOT NULL) OR (p_state = 'failed' AND p_error_code NOT IN ('verification_failed', 'stale_release', 'apply_failed')) THEN RAISE EXCEPTION 'privacy release status is invalid' USING ERRCODE = '22023'; END IF;
    IF NOT EXISTS (SELECT 1 FROM config.privacy_mask_release_manifests r WHERE r.tenant_id = v_tenant_id AND r.id = p_release_id AND r.device_id = p_device_id AND r.version = p_version) THEN RAISE EXCEPTION 'privacy release is not available to device' USING ERRCODE = '22023'; END IF;
    IF EXISTS (SELECT 1 FROM config.privacy_mask_release_statuses s WHERE s.tenant_id = v_tenant_id AND s.device_id = p_device_id AND s.version > p_version) THEN RAISE EXCEPTION 'privacy release outcome is stale' USING ERRCODE = '22023'; END IF;
    INSERT INTO config.privacy_mask_release_statuses AS s (tenant_id, device_id, release_id, version, state, error_code, reported_at, accepted_at) VALUES (v_tenant_id, p_device_id, p_release_id, p_version, p_state, p_error_code, clock_timestamp(), CASE WHEN p_state = 'accepted' THEN clock_timestamp() ELSE NULL END) ON CONFLICT (tenant_id, device_id) DO UPDATE SET release_id = EXCLUDED.release_id, version = EXCLUDED.version, state = EXCLUDED.state, error_code = EXCLUDED.error_code, reported_at = EXCLUDED.reported_at, accepted_at = EXCLUDED.accepted_at;
    RETURN QUERY SELECT s.tenant_id, s.device_id, s.release_id, s.version, s.state, s.error_code, s.reported_at, s.accepted_at FROM config.privacy_mask_release_statuses s WHERE s.tenant_id = v_tenant_id AND s.device_id = p_device_id;
END; $$;

REVOKE ALL ON FUNCTION edge.pull_privacy_mask_release(uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION edge.report_privacy_mask_release(uuid, uuid, bigint, varchar, varchar) FROM PUBLIC;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN EXECUTE 'GRANT EXECUTE ON FUNCTION edge.pull_privacy_mask_release(uuid, bigint) TO syncam_app'; EXECUTE 'GRANT EXECUTE ON FUNCTION edge.report_privacy_mask_release(uuid, uuid, bigint, varchar, varchar) TO syncam_app'; END IF; END; $$;
