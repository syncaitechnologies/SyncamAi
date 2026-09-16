# Phase 12 test-only privacy-rights case state machine

T-0430 adds a pure in-memory transition boundary for the test-only case draft
created by T-0429. It represents only the proposed workflow from the draft
individual privacy-request procedure.

## Permitted synthetic transitions

The library starts at `verification_pending` because T-0429 validates only the
post-receipt metadata draft; it does not store or acknowledge a real request.

```text
verification_pending -> under_review -> fulfillment_pending -> completed
        |                    |                   |
        +-> rejected         +-> rejected        +-> partially_fulfilled
```

Each transition requires the same caller-injected `test_only` policy used to
create the draft, a strictly later UTC timestamp and the next integer version.
The function returns a new value and never mutates its input. It preserves the
opaque identifiers, request kind, receipt/due timestamps and synthetic policy
references. Skipped, repeated, terminal, stale, policy-mismatched and
non-test-mode transitions fail closed.

## Deliberate boundaries

These strings do not verify a person, reject a legal request, fulfill a right,
calculate an approved legal deadline, stop consent-based processing, delete
data, apply a legal hold or represent an erasure manifest. In particular,
`completed` is unavailable until a real approved fulfillment system has
store-level evidence. A withdrawal or opt-out state change has no effect on
consent or processors. There is no HTTP, UI, persistence, migration/RLS, audit,
outbox, queue, external communication, personal payload or biometric handling.

Production work remains gated on Privacy/Legal and product approval of the
intake channel, verification, jurisdictional schedule, authorized owners,
private case/evidence storage, legal holds, response delivery and escalation.
