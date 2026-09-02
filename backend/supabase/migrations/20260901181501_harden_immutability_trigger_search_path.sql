-- These trigger functions have no application-object references.  Pinning their
-- lookup path removes the session-dependent resolution warned about by the
-- Supabase security advisor while preserving their append-only guarantees.
ALTER FUNCTION audit.reject_event_mutation() SET search_path = '';
ALTER FUNCTION alerts.reject_action_mutation() SET search_path = '';
ALTER FUNCTION config.reject_privacy_mask_approval_mutation() SET search_path = '';
ALTER FUNCTION config.reject_privacy_mask_release_manifest_mutation() SET search_path = '';
