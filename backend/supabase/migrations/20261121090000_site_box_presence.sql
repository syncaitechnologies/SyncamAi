-- Site box offline alerts.
--
-- A site box sends a heartbeat every two minutes, which sets
-- platform.site_keys.last_seen_at. The API's watchdog calls
-- platform.site_box_transitions() every minute: it marks boxes that have been
-- silent longer than the threshold as offline (offline_since = their last
-- heartbeat) and clears the mark when they report again, returning each change
-- exactly once so each one is announced once.

ALTER TABLE platform.site_keys ADD COLUMN IF NOT EXISTS offline_since timestamptz;

-- Boxes already silent when this ships are treated as already announced, so the
-- first run does not alert about old, unused keys.
UPDATE platform.site_keys
SET offline_since = last_seen_at
WHERE revoked_at IS NULL AND last_seen_at IS NOT NULL
  AND last_seen_at < clock_timestamp() - interval '10 minutes';

-- Runs across all tenants (no tenant context exists for a background check),
-- so it is a narrow definer function that returns only what a notice needs.
CREATE OR REPLACE FUNCTION platform.site_box_transitions(p_offline_after interval)
RETURNS TABLE (
    kind text, key_id uuid, tenant_id uuid, site_id uuid,
    workspace text, site_name text, timezone text, box_label text,
    last_seen_at timestamptz, offline_since timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
#variable_conflict use_column
BEGIN
    IF p_offline_after IS NULL OR p_offline_after < interval '4 minutes' THEN
        RAISE EXCEPTION 'offline threshold must be at least 4 minutes' USING ERRCODE = '22023';
    END IF;
    RETURN QUERY
    WITH went_offline AS (
        UPDATE platform.site_keys AS k
        SET offline_since = k.last_seen_at
        WHERE k.revoked_at IS NULL AND k.last_seen_at IS NOT NULL AND k.offline_since IS NULL
          AND k.last_seen_at < clock_timestamp() - p_offline_after
        RETURNING 'offline'::text AS kind, k.id, k.tenant_id, k.site_id, k.label, k.last_seen_at, k.last_seen_at AS since
    ),
    returning_boxes AS (
        SELECT k.id, k.offline_since AS since
        FROM platform.site_keys AS k
        WHERE k.offline_since IS NOT NULL
          AND (k.revoked_at IS NOT NULL OR k.last_seen_at >= clock_timestamp() - p_offline_after)
        FOR UPDATE
    ),
    came_back AS (
        UPDATE platform.site_keys AS k
        SET offline_since = NULL
        FROM returning_boxes AS r
        WHERE k.id = r.id
        RETURNING CASE WHEN k.revoked_at IS NULL THEN 'online' ELSE 'revoked' END AS kind,
                  k.id, k.tenant_id, k.site_id, k.label, k.last_seen_at, r.since
    ),
    changes AS (
        SELECT * FROM went_offline
        UNION ALL
        SELECT * FROM came_back
    )
    SELECT c.kind, c.id, c.tenant_id, c.site_id,
           t.name::text, s.name::text, s.timezone::text, c.label::text,
           c.last_seen_at, c.since
    FROM changes AS c
    JOIN identity.tenants AS t ON t.id = c.tenant_id
    JOIN config.sites AS s ON s.tenant_id = c.tenant_id AND s.id = c.site_id
    WHERE c.kind <> 'revoked';
END;
$$;

REVOKE ALL ON FUNCTION platform.site_box_transitions(interval) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION platform.site_box_transitions(interval) FROM anon, authenticated';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.site_box_transitions(interval) TO syncam_app';
    END IF;
END;
$$;
