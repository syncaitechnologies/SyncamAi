-- Site boxes, Telegram alert delivery and zone schedules.
--
-- A site box (the on-premises AI runtime) authenticates with a revocable,
-- hashed site key instead of a user token or device certificate. Only the
-- SHA-256 hash of a key is stored; the raw key is shown once at creation.

CREATE TABLE IF NOT EXISTS platform.site_keys (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    label varchar(120) NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 120),
    key_hash char(64) NOT NULL UNIQUE CHECK (key_hash ~ '^[0-9a-f]{64}$'),
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    last_seen_at timestamptz,
    camera_status jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(camera_status) = 'object'),
    CONSTRAINT site_keys_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS site_keys_tenant_site_idx ON platform.site_keys (tenant_id, site_id, created_at);

ALTER TABLE platform.site_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.site_keys FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON platform.site_keys;
CREATE POLICY tenant_isolation ON platform.site_keys
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Key lookup happens before any tenant is known, so it is a narrow
-- SECURITY DEFINER function that returns only the scope of an active key.
CREATE OR REPLACE FUNCTION platform.resolve_site_key(p_key_hash text)
RETURNS TABLE (key_id uuid, tenant_id uuid, site_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ''
AS $$
    SELECT k.id, k.tenant_id, k.site_id
    FROM platform.site_keys AS k
    WHERE k.key_hash = p_key_hash AND k.revoked_at IS NULL
    LIMIT 1;
$$;

CREATE SCHEMA IF NOT EXISTS notify;

CREATE TABLE IF NOT EXISTS notify.telegram_targets (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    chat_id bigint NOT NULL,
    chat_title varchar(256) NOT NULL DEFAULT '',
    min_severity varchar(16) NOT NULL DEFAULT 'medium'
        CHECK (min_severity IN ('low', 'medium', 'high', 'critical')),
    muted boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, site_id, chat_id),
    CONSTRAINT telegram_targets_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS notify.telegram_link_codes (
    code_hash char(64) PRIMARY KEY CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CONSTRAINT telegram_link_codes_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

ALTER TABLE notify.telegram_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE notify.telegram_targets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON notify.telegram_targets;
CREATE POLICY tenant_isolation ON notify.telegram_targets
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE notify.telegram_link_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE notify.telegram_link_codes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON notify.telegram_link_codes;
CREATE POLICY tenant_isolation ON notify.telegram_link_codes
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- A Telegram /start message carries only the one-time code, so redemption
-- also happens before a tenant is known. The code is single-use and expires.
CREATE OR REPLACE FUNCTION notify.redeem_telegram_link(p_code_hash text, p_chat_id bigint, p_chat_title text)
RETURNS TABLE (tenant_id uuid, site_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
#variable_conflict use_column
DECLARE
    link notify.telegram_link_codes%ROWTYPE;
BEGIN
    SELECT * INTO link
    FROM notify.telegram_link_codes AS c
    WHERE c.code_hash = p_code_hash AND c.used_at IS NULL AND c.expires_at > clock_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE notify.telegram_link_codes SET used_at = clock_timestamp() WHERE code_hash = p_code_hash;
    INSERT INTO notify.telegram_targets (id, tenant_id, site_id, chat_id, chat_title)
    VALUES (gen_random_uuid(), link.tenant_id, link.site_id, p_chat_id, left(coalesce(p_chat_title, ''), 256))
    ON CONFLICT (tenant_id, site_id, chat_id) DO UPDATE SET muted = false, chat_title = EXCLUDED.chat_title;
    tenant_id := link.tenant_id;
    site_id := link.site_id;
    RETURN NEXT;
END;
$$;

-- The in-process alert worker serves every tenant. It needs to know which
-- tenants have pending outbox work without reading any message contents.
CREATE OR REPLACE FUNCTION messaging.pending_outbox_tenants()
RETURNS SETOF uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ''
AS $$
    SELECT DISTINCT m.tenant_id
    FROM messaging.outbox_messages AS m
    WHERE m.published_at IS NULL
      AND (m.lease_expires_at IS NULL OR m.lease_expires_at <= clock_timestamp())
    LIMIT 500;
$$;

-- Optional weekly schedule; NULL means the zone is always active.
ALTER TABLE config.zones ADD COLUMN IF NOT EXISTS schedule jsonb
    CHECK (schedule IS NULL OR jsonb_typeof(schedule) = 'object');

REVOKE ALL ON FUNCTION platform.resolve_site_key(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION notify.redeem_telegram_link(text, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION messaging.pending_outbox_tenants() FROM PUBLIC;
-- Hosted projects may carry default grants for browser roles; remove them
-- explicitly so only the Go API (syncam_app) can reach these objects.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON SCHEMA notify FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION platform.resolve_site_key(text) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION notify.redeem_telegram_link(text, bigint, text) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON FUNCTION messaging.pending_outbox_tenants() FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON platform.site_keys, notify.telegram_targets, notify.telegram_link_codes FROM anon, authenticated';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.site_keys TO syncam_app';
        EXECUTE 'GRANT USAGE ON SCHEMA notify TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON notify.telegram_targets TO syncam_app';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON notify.telegram_link_codes TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.resolve_site_key(text) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION notify.redeem_telegram_link(text, bigint, text) TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION messaging.pending_outbox_tenants() TO syncam_app';
    END IF;
END;
$$;
