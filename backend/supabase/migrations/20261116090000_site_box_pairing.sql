-- Pairing a site box with a short code instead of copying a site key.
--
-- An administrator asks the dashboard for an 8-character pairing code for a
-- site. The person installing the box types it once; the box redeems it and
-- receives its own revocable site key. Only the SHA-256 hash of the code is
-- stored, a code works once and expires after 30 minutes, and the box's public
-- key is recorded so the dashboard can later encrypt camera passwords for that
-- box alone.

CREATE TABLE IF NOT EXISTS platform.pairing_codes (
    code_hash char(64) PRIMARY KEY CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    tenant_id uuid NOT NULL REFERENCES identity.tenants(id),
    site_id uuid NOT NULL,
    created_by varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    site_key_id uuid,
    CONSTRAINT pairing_codes_tenant_site_fk FOREIGN KEY (tenant_id, site_id) REFERENCES config.sites (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS pairing_codes_tenant_site_idx ON platform.pairing_codes (tenant_id, site_id, created_at);

ALTER TABLE platform.pairing_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.pairing_codes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON platform.pairing_codes;
CREATE POLICY tenant_isolation ON platform.pairing_codes
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The box's public key (PEM, P-256) for end-to-end encrypted camera passwords.
ALTER TABLE platform.site_keys ADD COLUMN IF NOT EXISTS box_public_key text
    CHECK (box_public_key IS NULL OR length(box_public_key) BETWEEN 1 AND 1024);

-- Redemption happens before any tenant is known (the box has only the code),
-- so it is a narrow definer function: it consumes one valid code and creates
-- the site key for that code's site in the same transaction.
CREATE OR REPLACE FUNCTION platform.redeem_pairing_code(
    p_code_hash text, p_key_id uuid, p_key_hash text, p_label text, p_public_key text
)
RETURNS TABLE (tenant_id uuid, site_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $$
#variable_conflict use_column
DECLARE
    pairing platform.pairing_codes%ROWTYPE;
BEGIN
    SELECT * INTO pairing
    FROM platform.pairing_codes AS c
    WHERE c.code_hash = p_code_hash AND c.used_at IS NULL AND c.expires_at > clock_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM config.sites AS s
        WHERE s.tenant_id = pairing.tenant_id AND s.id = pairing.site_id AND s.status <> 'retired'
    ) THEN
        RETURN;
    END IF;
    UPDATE platform.pairing_codes SET used_at = clock_timestamp(), site_key_id = p_key_id WHERE code_hash = p_code_hash;
    INSERT INTO platform.site_keys (id, tenant_id, site_id, label, key_hash, created_by, box_public_key)
    VALUES (
        p_key_id, pairing.tenant_id, pairing.site_id,
        COALESCE(NULLIF(left(btrim(COALESCE(p_label, '')), 120), ''), 'Site box'),
        p_key_hash, left('pairing:' || pairing.created_by, 128), NULLIF(p_public_key, '')
    );
    tenant_id := pairing.tenant_id;
    site_id := pairing.site_id;
    RETURN NEXT;
END;
$$;

REVOKE ALL ON FUNCTION platform.redeem_pairing_code(text, uuid, text, text, text) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        EXECUTE 'REVOKE ALL ON FUNCTION platform.redeem_pairing_code(text, uuid, text, text, text) FROM anon, authenticated';
        EXECUTE 'REVOKE ALL ON platform.pairing_codes FROM anon, authenticated';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON platform.pairing_codes TO syncam_app';
        EXECUTE 'GRANT EXECUTE ON FUNCTION platform.redeem_pairing_code(text, uuid, text, text, text) TO syncam_app';
    END IF;
END;
$$;
