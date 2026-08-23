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

The edge verifier accepts only bounded metadata for an already-approved request
and the closed local ordering `decode -> mask -> encode`; it produces a
deterministic candidate hash for later evidence collection. A second local
boundary verifies a signed attestation from an allowlisted physical
camera/encoder HIL harness. The attestation must confirm that masking preceded
encoding, that encoder bypass was denied, and that no raw frames were retained.
It stores hashes and measured booleans only.

The controlled edge release boundary accepts a privacy-only manifest only when
its pre-encode candidate hash, device UUID, and signed HIL evidence agree. It
hands that metadata to an atomic release-slot applier, preserves the prior
accepted release on failure, and reports only fixed safe status categories.
Generic zone configuration cannot use this path. Durable Postgres tables now
store tenant-isolated manifest metadata and the latest safe release status for
each device. A manifest is immutable in the database and versions are unique
per tenant/device; the application role cannot delete either manifest or status
records.

Neither verifier nor the controlled release boundary accepts frames, pixels,
stream credentials, encoder handles, model weights, or mask output. A
registered physical harness must still execute and sign the actual release test;
this code does not manufacture hardware evidence. The following repository and
transport slice must authorize manifest creation against an approved request and
verified HIL evidence, atomically append its audit event, authenticate the
specified device, and persist only safe status reporting before a privacy-mask
configuration can reach a camera.
