-- Mirrors the Supabase migration 20261125150000_zone_hours_local_hours.sql:
-- hourly counts may start on any quarter-hour (site-local hours in India and Nepal).
ALTER TABLE platform.zone_hours DROP CONSTRAINT IF EXISTS zone_hours_hour_start_check;
ALTER TABLE platform.zone_hours ADD CONSTRAINT zone_hours_hour_start_check
    CHECK (date_trunc('minute', hour_start AT TIME ZONE 'UTC') = hour_start AT TIME ZONE 'UTC'
           AND extract(minute FROM hour_start AT TIME ZONE 'UTC')::int % 15 = 0);
