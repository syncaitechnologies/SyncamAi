-- Supabase Auth does not offer an invitation idempotency key. An ambiguous
-- provider result must therefore stop automatic retries until reconciled.
ALTER TABLE identity.lifecycle_delivery_requests
    DROP CONSTRAINT lifecycle_delivery_requests_status_check;

ALTER TABLE identity.lifecycle_delivery_requests
    ADD CONSTRAINT lifecycle_delivery_requests_status_check
    CHECK (status IN ('pending', 'delivering', 'delivered', 'failed', 'reconciliation_required'));
