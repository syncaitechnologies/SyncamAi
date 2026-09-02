-- Provider delivery is an at-least-once backend concern. A lease makes a
-- crashed worker recoverable without granting a browser any access.
ALTER TABLE identity.lifecycle_delivery_requests
    ADD COLUMN lease_owner uuid,
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN delivery_attempts integer NOT NULL DEFAULT 0
        CHECK (delivery_attempts >= 0);

ALTER TABLE identity.lifecycle_delivery_requests
    ADD CONSTRAINT lifecycle_delivery_requests_lease_state_check
    CHECK (
        (status = 'delivering') = (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
    );

CREATE INDEX lifecycle_delivery_requests_claimable_idx
    ON identity.lifecycle_delivery_requests (tenant_id, created_at, id)
    WHERE status IN ('pending', 'failed') OR lease_expires_at IS NOT NULL;
