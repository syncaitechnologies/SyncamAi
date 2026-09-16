# Phase 12 test-only privacy-rights idempotency boundary

T-0431 adds a single-case, in-memory test store around the T-0430 transition
function. It verifies the future persistence contract without claiming that a
database, queue or audit transaction exists.

## Synthetic mutation rules

Each `TransitionCommand` includes only an opaque UUIDv4 request identifier, an
expected version, a next state and a UTC transition time. Under one mutex:

1. an exact request replay returns the original copied transition result;
2. reuse of that request identifier with any different detail fails with a
   conflict;
3. a new request must name the current expected version; and
4. only then can the existing test-only state machine advance the draft.

Concurrent synthetic requests with the same expected version therefore produce
one accepted transition and one version conflict. No failed command changes the
stored draft. The store retains only metadata from valid terminal-bounded state
paths; it accepts no narrative, evidence, consent, biometric, fulfillment or
external payload.

## Deliberate boundaries

This is process-local test code, not a repository. It does not survive restart,
coordinate across processes, set tenant context, enforce RLS, append an audit
event, publish an outbox message, verify identity/authority, assign an owner,
send a response, change consent, erase records or claim a request outcome. It
is not wired into HTTP, UI, worker or production runtime.

A later approved production slice must use the authoritative database under a
tenant-scoped transaction, preserve the same request-hash and optimistic-lock
semantics, atomically append audit/outbox evidence, and add RLS, migration,
authorization, privacy, accessibility and end-to-end evidence. Those steps
remain gated on Privacy/Legal and product approval.
