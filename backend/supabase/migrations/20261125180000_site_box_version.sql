-- The software version each site box reports in its heartbeat (box 0.1.9 and
-- later), shown on the dashboard's Site box card. NULL until the box reports.
ALTER TABLE platform.site_keys
    ADD COLUMN IF NOT EXISTS box_version text
    CONSTRAINT site_keys_box_version_check CHECK (box_version ~ '^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$');
