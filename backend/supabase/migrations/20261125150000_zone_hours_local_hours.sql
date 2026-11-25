-- ADR-020: hourly counts start on the site's local hour, so sites on a
-- half-hour or 45-minute offset (India, Nepal) start them at :30 or :45 UTC.
-- Any whole quarter-hour is allowed; seconds stay zero.

ALTER TABLE platform.zone_hours DROP CONSTRAINT IF EXISTS zone_hours_hour_start_check;
ALTER TABLE platform.zone_hours ADD CONSTRAINT zone_hours_hour_start_check
    CHECK (date_trunc('minute', hour_start AT TIME ZONE 'UTC') = hour_start AT TIME ZONE 'UTC'
           AND extract(minute FROM hour_start AT TIME ZONE 'UTC')::int % 15 = 0);
