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
this code does not manufacture hardware evidence. The platform release
repository now recomputes the bounded pre-encode candidate and verifies the
signed physical-HIL metadata against an injected trusted-harness allowlist. In
one tenant-scoped transaction it requires the immutable approved request, both
recorded approvals, and an active mTLS-ready device at the same site before
inserting the manifest and its audit-chain event. It is not an HTTP/device
transport, does not register trust keys, and does not perform a physical test.
The following transport slice must authenticate the specified device and expose
only its approved manifest plus safe status reporting. Dedicated database
procedures now restrict privacy-mask manifest pull and status reporting to an
active, certificate-authorized device, reject cross-device or stale outcomes,
and accept only the fixed `verification_failed`, `stale_release`, and
`apply_failed` failure categories. The edge agent now uses only this dedicated
mTLS route: it pulls one newer manifest, invokes the controlled local gate, and
reports the gate's safe status. It does not reuse generic configuration
synchronization, create releases, process media, or execute masking.

## Proposed next slice: T-0351

T-0351 is planned to add a hardware-bound adapter that can enforce a release
only for an owner-approved, allowlisted physical camera/encoder profile. It is
intentionally separate from generic configuration delivery and will preserve
the required `decode -> mask -> encode` order without an encoder-bypass path.

This planning entry is not authorization to activate a mask or to claim a
hardware result. Implementation requires selection of the physical
camera/encoder profile and a registered hardware-in-loop harness. A simulated
test may validate the adapter contract, but cannot satisfy the existing
production evidence gate; a signed physical HIL result remains required.
