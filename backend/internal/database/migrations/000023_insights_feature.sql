-- ADR-021: visitor and table insights become a plan feature. Mirrors the
-- constraint changes of the Supabase migration 20261126090000_insights_feature.sql;
-- the owner routine lives only in Supabase.
ALTER TABLE identity.tenant_features DROP CONSTRAINT IF EXISTS tenant_features_feature_check;
ALTER TABLE identity.tenant_features ADD CONSTRAINT tenant_features_feature_check
    CHECK (feature IN ('zone_alerts', 'loitering', 'attendance', 'weapon', 'insights'));

ALTER TABLE config.feature_settings DROP CONSTRAINT IF EXISTS feature_settings_feature_check;
ALTER TABLE config.feature_settings ADD CONSTRAINT feature_settings_feature_check
    CHECK (feature IN ('zone_alerts', 'loitering', 'attendance', 'weapon', 'live_view', 'insights'));
