# Release-notes process

## Status

This process establishes an internal, unreleased-only workflow. It does not
announce a product release, publish customer communications, or claim that a
capability is available in any environment.

## Purpose

Phase 7 requires a repeatable release-notes process that gives pilot customers
clear, accurate, and safe information. Every externally visible note must
describe only an approved, deployed, and verified change in the applicable
environment. Do not use release notes to promise dates, activate features,
disclose security-sensitive information, or bypass any Product, Legal, Privacy,
Security, AI, or operational gate.

## Before drafting a note

- Confirm the change has an approved pull request, required test/verification
  evidence, and an approved release/deployment record for the stated
  environment.
- Confirm Product owns the customer-facing wording and intended audience.
- Confirm Security and Privacy have reviewed any change that affects identity,
  permissions, tenant isolation, data handling, retention, audit, or customer
  configuration.
- Confirm Legal approval where the change affects contractual terms, billing,
  tax, privacy, biometric processing, exports, or regulated claims.
- Confirm that no gated capability is described as available. In particular,
  biometric attendance remains unavailable until its separate approval and
  release gates are complete.

## Drafting rules

- State what changed, who it affects, the environment, and any customer action
  required. Use plain language and avoid internal implementation details.
- State limitations, rollout scope, prerequisites, or known operational impact
  when they are material to a customer’s use.
- Never include secrets, credentials, private URLs, customer names, tenant or
  device identifiers, footage, personal data, exploit details, or incident
  evidence.
- Never state an assumed price, tax treatment, invoice behavior, payment
  method, entitlement, service level, retention period, or compliance status
  as final unless it has the documented approving owner.
- Do not describe an unmerged branch, a planned task, a local demo, or a
  synthetic workspace as shipped functionality.

## Approval and publication

1. Engineering supplies the change summary, verification evidence reference,
   rollout scope, rollback reference, and known limitations.
2. Product prepares the customer-facing summary and proposed audience.
3. Security, Privacy, Legal, Support, or Finance review when the change falls
   within their approval area.
4. The named release owner approves publication for the stated environment and
   audience.
5. Customer Success publishes the approved note through the approved channel
   and records the publication reference.

If any input is missing, inconsistent, or outside the stated rollout scope,
the note remains unpublished. A release note does not replace a release gate,
deployment approval, incident notice, security advisory, or customer contract.

## Changelog maintenance

Maintain entries in [CHANGELOG.md](../CHANGELOG.md) only after the approval and
publication steps above are complete. Each entry should contain the release
date, customer-facing scope, approved change summary, customer action (if any),
and a safe reference to the published note. The changelog must remain empty of
unreleased customer claims.
