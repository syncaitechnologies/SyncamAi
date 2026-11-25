-- Alert retention: alerts are permanently deleted six months after they were
-- raised, whatever their status (removed from the queue or not).
--
-- The API runs a daily cleanup:
--   1. alerts.expired_alert_evidence(limit) lists the oldest expired alerts and
--      their snapshot references, so the API can delete the snapshot files from
--      Storage first (Storage objects cannot be deleted from SQL).
--   2. alerts.purge_expired_alerts(ids) deletes those alerts, their action
--      history and their source detection events.
-- The six-month window is fixed here, not passed in, so the API cannot delete
-- anything younger. The hash-chained audit log (audit.events) is kept.

-- Action history stays append-only except for the retention purge, which
-- marks its own transaction and runs as the table owner.
CREATE OR REPLACE FUNCTION alerts.reject_action_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = ''
AS $$
BEGIN
    IF TG_OP = 'DELETE'
       AND current_setting('syncam.alert_retention_purge', true) = 'on'
       AND current_user = (SELECT pg_catalog.pg_get_userbyid(c.relowner) FROM pg_catalog.pg_class AS c WHERE c.oid = TG_RELID) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'alert actions are append-only' USING ERRCODE = '55000';
END;
$$;

CREATE INDEX IF NOT EXISTS alerts_created_at_idx ON alerts.alerts (created_at);

-- Runs across all workspaces (a background job has no tenant context), so it
-- is a narrow definer function returning only IDs and snapshot references.
CREATE OR REPLACE FUNCTION alerts.expired_alert_evidence(p_limit integer)
RETURNS TABLE (alert_id uuid, tenant_id uuid, evidence_refs text[])
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ''
AS $$
    SELECT a.alert_id, a.tenant_id,
           COALESCE(ARRAY(SELECT jsonb_array_elements_text(e.evidence_refs)), ARRAY[]::text[])
    FROM alerts.alerts AS a
    LEFT JOIN events.detection_events AS e ON e.tenant_id = a.tenant_id AND e.event_id = a.event_id
    WHERE a.created_at < clock_timestamp() - interval '6 months'
    ORDER BY a.created_at, a.alert_id
    LIMIT LEAST(GREATEST(COALESCE(p_limit, 1), 1), 1000);
$$;

-- Deletes the given alerts that are older than six months (younger ones are
-- ignored), with their action history and their detection events. Returns how
-- many alerts were deleted.
CREATE OR REPLACE FUNCTION alerts.purge_expired_alerts(p_alert_ids uuid[])
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
DECLARE
    expired_alerts uuid[];
    expired_events uuid[];
    removed integer;
BEGIN
    IF p_alert_ids IS NULL OR cardinality(p_alert_ids) = 0 THEN
        RETURN 0;
    END IF;
    IF cardinality(p_alert_ids) > 1000 THEN
        RAISE EXCEPTION 'purge at most 1000 alerts at a time' USING ERRCODE = '22023';
    END IF;
    SELECT COALESCE(array_agg(a.alert_id), ARRAY[]::uuid[]), COALESCE(array_agg(a.event_id), ARRAY[]::uuid[])
    INTO expired_alerts, expired_events
    FROM alerts.alerts AS a
    WHERE a.alert_id = ANY(p_alert_ids)
      AND a.created_at < clock_timestamp() - interval '6 months';
    IF cardinality(expired_alerts) = 0 THEN
        RETURN 0;
    END IF;
    PERFORM set_config('syncam.alert_retention_purge', 'on', true);
    DELETE FROM alerts.alert_actions WHERE alert_id = ANY(expired_alerts);
    PERFORM set_config('syncam.alert_retention_purge', 'off', true);
    DELETE FROM alerts.alerts WHERE alert_id = ANY(expired_alerts);
    GET DIAGNOSTICS removed = ROW_COUNT;
    DELETE FROM events.detection_events AS e
    WHERE e.event_id = ANY(expired_events)
      AND NOT EXISTS (SELECT 1 FROM alerts.alerts AS o WHERE o.tenant_id = e.tenant_id AND o.event_id = e.event_id);
    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION alerts.expired_alert_evidence(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION alerts.purge_expired_alerts(uuid[]) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION alerts.expired_alert_evidence(integer) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION alerts.purge_expired_alerts(uuid[]) FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION alerts.expired_alert_evidence(integer) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION alerts.purge_expired_alerts(uuid[]) TO syncam_app';
    END IF;
END;
$$;
