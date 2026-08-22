# Phase 3 privacy-mask governance

Privacy masks remain disabled. The `privacy_masks:approve` capability is
reserved for an MFA-authenticated Super Admin only. The metadata-only request
workflow requires two distinct Super Admin approvals and rejects approval by
the requester. A completed approval record is still not sufficient to deliver
or activate a mask.

Privacy-mask request metadata and approval rows have tenant-isolated Postgres
storage. Approval rows are database-immutable, while the application role may
only select or insert them. The production repository records every request and
distinct approval in the append-only, tenant-day hash chain in the same short
transaction as the governed state change; a failed audit insert rolls the state
change back. Duplicate approval replays do not create a second state change or
audit event.

Pre-encode edge-side masking verification and hardware-in-loop release evidence
are still required before a privacy-mask configuration can reach a camera. No
frames, pixels, stream credentials, model weights, or masking execution are
added by this workflow or ledger slice.
