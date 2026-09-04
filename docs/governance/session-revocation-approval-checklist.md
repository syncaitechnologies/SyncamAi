# Session-revocation approval checklist

## Status

Preparation only. This checklist grants no permission, deploys no worker, and
does not enable session revocation.

## Purpose

ADR-011 keeps user disablement local and queues an external session-revocation
intent. A separately reviewed provider worker is required before SyncCam can
claim that sessions were revoked. Complete and record every item below before
that worker is implemented, configured, or deployed.

## Product-owner decision

- [ ] Confirm the product need for server-side session revocation when a user
  is disabled.
- [ ] Confirm the intended environments: development, staging, and production.
- [ ] Confirm that the existing browser and HTTP API remain unable to trigger
  the provider action directly.
- [ ] Confirm that re-enabling a user is a separate, audited product action;
  it is not an automatic rollback of a disablement.
- [ ] Record the decision owner, date, and reference in the delivery PR or
  security-review record.

This checklist does not decide T-0309. The temporary public-publication policy
and its outstanding Legal review remain governed by
[`source-publication-policy.md`](source-publication-policy.md).

## Security review

- [ ] Review the exact provider API and its current session-revocation
  semantics, including the treatment of existing access tokens.
- [ ] Require a verified MFA-authenticated principal with `users:manage`; keep
  the existing self-disablement and last-Super-Admin protections.
- [ ] Define the shortest acceptable access-token lifetime and, if immediate
  enforcement is required for sensitive operations, a server-side session-ID
  validation strategy.
- [ ] Confirm that a dedicated server worker is the only process able to use
  the provider credential. The browser, Go HTTP process, source tree, logs,
  audit payloads, and outbox payloads must not contain it.
- [ ] Confirm least-privilege tenant queue access and that the worker accepts
  only its declared lifecycle action.
- [ ] Confirm audit fields: tenant, actor, target user, request ID, timestamp,
  and a safe result code. Do not store provider response bodies, bearer tokens,
  or credentials.
- [ ] Approve the failure policy: ambiguous results enter
  `reconciliation_required`; no automatic retry occurs until provider state is
  reconciled through a documented, audited procedure.
- [ ] Review the incident procedure for credential rotation, provider outage,
  erroneous disablement, and emergency worker shutdown.

## Infrastructure approval

- [ ] Name the separately deployable worker runtime and non-production test
  environment.
- [ ] Store the provider secret only in that runtime's encrypted secret manager
  with restricted write and read access. Do not put it in GitHub Actions,
  checked-in environment files, or public environment variables.
- [ ] Define secret rotation, access review, and revocation procedures.
- [ ] Give the worker only the database and queue access it needs; do not reuse
  the HTTP service identity.
- [ ] Provide monitored logs and alerts that use request IDs and safe result
  codes, with no secret or provider-response capture.
- [ ] Verify the design against a disposable non-production Supabase project
  before any production credential is introduced.

## Required evidence before implementation

- A product-owner decision record.
- A security-review sign-off naming the approved API behavior and token policy.
- An infrastructure plan naming the worker runtime, secret manager, and
  least-privilege identities.
- Threat-model, unit-test, integration-test, reconciliation-runbook, and
  rollback/incident-review updates in the implementation PR.

Until all evidence exists, lifecycle delivery remains invitation-only and
disablement continues to be reported as pending external session revocation.
