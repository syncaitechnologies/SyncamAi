ALTER TABLE config.zones
    ADD COLUMN IF NOT EXISTS subject_classes text[] NOT NULL DEFAULT ARRAY[]::text[];

ALTER TABLE config.zones
    DROP CONSTRAINT IF EXISTS zones_subject_classes_valid;

ALTER TABLE config.zones
    ADD CONSTRAINT zones_subject_classes_valid
    CHECK (
        cardinality(subject_classes) <= 7
        AND subject_classes <@ ARRAY['person', 'bicycle', 'bus', 'car', 'motorcycle', 'truck', 'van']::text[]
    );
