ALTER TABLE config.zones
    ADD COLUMN IF NOT EXISTS loiter_seconds integer;

UPDATE config.zones
SET loiter_seconds = 30
WHERE kind = 'loitering' AND loiter_seconds IS NULL;

ALTER TABLE config.zones
    DROP CONSTRAINT IF EXISTS zones_loiter_seconds_valid;

ALTER TABLE config.zones
    ADD CONSTRAINT zones_loiter_seconds_valid
    CHECK (
        (kind = 'loitering' AND loiter_seconds BETWEEN 30 AND 600)
        OR (kind <> 'loitering' AND loiter_seconds IS NULL)
    );
