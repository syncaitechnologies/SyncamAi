# Individual privacy-request procedure

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Proposed operational workflow. The inspected baseline has no request API, persistent queue or self-service fulfillment UI. A staffed private intake channel and approved retention are prerequisites to real intake.

## Intake and verification

Accept applicable access, correction/updating, erasure, withdrawal, grievance/complaint, nomination, portability and opt-out/limitation requests. Route customer-directed processing to the responsible customer contact while retaining SyncCam's contractual assistance and own-purpose responsibilities. Do not require every requester to have an app account; provide an accessible, verified alternative for visitors, former employees and authorized agents.

Record receipt time immediately, applicable jurisdiction/purpose, due date and its legal basis, assigned owner and proportionate verification status. Authentication alone does not prove authority over another subject. Use existing trusted identifiers or secure verification, minimize any documents, and avoid collecting additional biometrics to verify a privacy request. Authorized agents/nominees need authority checks where applicable. Requests such as certain opt-outs must not be subjected to unnecessary verification. Never disclose whether another tenant or person's record exists.

## Proposed state machine and audit

`received → verification_pending → under_review → fulfillment_pending → completed`, with explicit `rejected` and `partially_fulfilled` outcomes. Withdrawal/opt-out may require immediate cessation independent of the remaining case review. No state means deletion occurred until appropriate store evidence exists. Escalation, missing identity evidence or a legal hold must not silently reset the statutory receipt clock.

The future tenant-scoped case should contain opaque case/subject IDs, kind, jurisdiction, receipt/update/due timestamps, verified actor, verification method/status, owner, state/version, bounded reason code, private evidence references, audit reference, completion time and response reference. Sensitive narrative/evidence belongs in an approved access-controlled case system, not logs, public repository, URLs or generic events. Mutations must commit with audit evidence and use optimistic concurrency/idempotency to avoid duplicate or conflicting fulfillment. Assignment must verify the owner's authority within the tenant.

## Review and fulfillment

1. Determine which laws, customer instructions and exceptions apply. Inspect each relevant store and recipient without exporting unrelated people's data.
2. Access/correction responses need identity confirmation, safe delivery and protection/redaction of third-party information. Record a correction without silently altering audit history.
3. Withdrawal removes future consent-based permission, records its effective time and propagates to affected processors. Deletion is separately reconciled; do not retain future-use permission while awaiting legal review.
4. Erasure follows the [retention/deletion procedure](data-retention-and-deletion-policy.md). Record each remaining lawful hold or backup lifecycle limitation and its review owner. Never turn an intake acknowledgment into a completion certificate.
5. Give a reasoned response, applicable complaint/appeal channel and completion/partial/refusal explanation. No automated legal denial or consequential employment action is permitted.

## Timing and staffing

Counsel must configure deadlines by request type and current law, including permitted extensions and notice requirements. PIPEDA generally specifies 30 days for access, Alberta PIPA 45 days and BC/Quebec generally 30 days for relevant access responses; applicable extensions differ. California generally uses a 45-day substantive response for covered access/deletion/correction and shorter periods for certain choices. India's future DPDP grievance rule uses a published reasonable period no longer than 90 days; current SPDI grievance duties also require review. These are legal inputs, not a promise that this unimplemented service meets them. See [sources](privacy-jurisdiction-matrix.md).

Before launch, assign individual intake/backup owners, verify the contact channel, define exception handling and escalation, test timers and secure response delivery, and run tenant/authorization/concurrency/partial-erasure/withdrawal/E2E/accessibility tests. No real rights-request or consent evidence may be committed to this repository.
