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
outbox payloads. The worker records a stable delivery operation identifier,
retries safely, and reconciles uncertain provider responses before issuing a
second invitation or a conflicting user change.

The initial implementation order is: invitation intent and durable delivery;
then disablement with session revocation; then site reassignment. Roles,
scopes, and data-class grants remain a separately reviewed operation and are
not an implicit effect of an invitation.

Disablement is not considered complete until the provider worker has revoked
sessions. Until that worker, short token lifetime, and a provider-delivery
failure/recovery procedure are implemented and tested, the application must
continue to report lifecycle operations as unavailable rather than presenting
them as live.

## Consequences

- No Supabase service-role or secret key enters the frontend or the Go HTTP
  process configuration.
- A provider outage leaves a recoverable unpublished outbox message, never a
  partially committed browser operation.
- The local transaction remains the authoritative audit point; provider
  response metadata is minimised and never contains bearer credentials.
- The next implementation slice must add lifecycle persistence, RLS grants
  only for `syncam_app`, pgtAP allow/deny tests, and worker delivery tests
  before any Supabase Admin call is enabled.
