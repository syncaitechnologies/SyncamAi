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

The T-0351 adapter boundary now binds a release to one configured profile, its
device UUID, and its HIL harness ID before it can call a hardware-specific
executor. It passes only release metadata, the opaque candidate hash, and the
strict pipeline declaration. It refuses a mismatched device or harness,
simulated evidence, raw-frame retention, encoder bypass, invalid pipeline, or
failed executor call, preserving the previously active metadata release.
This is a fail-closed integration contract, not a claim that any physical
camera or encoder is now controlled; the configured profile and signed physical
HIL result remain mandatory production gates.

## Next slice: T-0352

T-0352 makes reconnect reconciliation fail closed. A hardware adapter treats
only an exact replay of its already-active, independently verified release as a
no-op, so a reconnect cannot invoke the hardware executor twice. A release with
the same version but a different release ID, candidate hash, profile, device,
or pipeline is rejected, as is every older release. This protects the active
release while the dedicated transport re-establishes its connection.

This does not persist, restore, or claim control of physical hardware state
across a power loss. Any durable hardware recovery design needs a vendor-backed
atomic activation and rollback contract, plus a newly signed physical HIL test;
neither raw frames nor credentials are stored by this reconciliation boundary.

## Next slice: T-0353

T-0353 adds a supervised, cancellable worker for the dedicated privacy-release
synchronizer. It performs one initial reconciliation and then serializes later
cycles at a caller-configured positive interval. A transport or gate failure is
returned to its supervisor rather than being redirected to generic
configuration delivery. The worker accepts no media, frames, stream
credentials, encoder handles, or privacy-mask geometry.


