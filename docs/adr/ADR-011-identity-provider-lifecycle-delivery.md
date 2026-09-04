# ADR-011: Deliver user lifecycle changes through a backend-only identity outbox

## Status

Accepted on 2026-09-03 by the product owner.

## Context

The bootstrap procedure in ADR-010 safely establishes the first Super Admin,
but it is deliberately not a general invitation, disablement, or reassignment
tool. Supabase administrative Auth operations require a secret credential and
are externally observable, so they cannot be part of the browser request or
used as a substitute for SyncCam's tenant transaction, audit chain, and
outbox.

## Decision

All user lifecycle commands first enter the existing Go `usermanagement`
boundary, which derives the actor from a verified MFA-authenticated principal
with `users:manage`. A tenant-scoped database transaction must validate the
requested state transition, persist the local lifecycle state, append the
audit event, and add one durable provider-delivery outbox message before it
commits. The HTTP request never calls Supabase Admin directly.

A separately deployed backend worker consumes only those messages. It first
acquires a short database lease, assigns the stable local operation identifier
to the delivery attempt, and can reclaim an expired lease after a crash. Its
Supabase Admin credential is supplied at runtime through an untracked secret
store, never browser configuration, source control, logs, audit payloads, or
outbox payloads. The worker records a stable delivery operation identifier.
Because the Supabase invite endpoint does not document an idempotency key, an
ambiguous response is held in `reconciliation_required` rather than retried; a
future reconciliation procedure must establish provider state before any
second invitation or conflicting user change.

The initial implementation order is: invitation intent and durable delivery;
then disablement with session revocation; then site reassignment. Roles,
scopes, and data-class grants remain a separately reviewed operation and are
not an implicit effect of an invitation.

Disablement is not considered complete until the provider worker has revoked
sessions. Until that worker, short token lifetime, and a provider-delivery
failure/recovery procedure are implemented and tested, the application must
continue to report lifecycle operations as unavailable rather than presenting
them as live.

The durable disablement step suspends the local tenant and site memberships,
which prevents the custom access-token hook from issuing fresh SyncCam claims.
It records an audit event and queues provider session revocation, but the
invitation-only worker deliberately leaves that request pending. The HTTP API
and browser remain unable to initiate this transition.

Delivery requests expose the existing target Auth-user UUID only to the
separately deployed, action-scoped server worker when that action carries one.
This transport identity is not a provider-session revocation, authorization
claim, secret, or browser-visible value. The current invitation provider
receives invitation requests only and continues to leave disablement pending.

## Consequences

- No Supabase service-role or secret key enters the frontend or the Go HTTP
  process configuration.
- A provider outage leaves a recoverable unpublished outbox message, never a
  partially committed browser operation.
- The local transaction remains the authoritative audit point; provider
  response metadata is minimised and never contains bearer credentials.
- The server-only invitation provider calls no browser route. The
  `lifecycle-delivery-worker` binary is separately deployable from the HTTP
  service and receives its runtime secret only through its process environment.
  This repository contains no deployment configuration, secret value,
  checked-in secret configuration, or frontend environment for that worker.
