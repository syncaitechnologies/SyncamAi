# Privacy implementation traceability

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Baseline: `06626a1`. T-0405 adds this draft only. [Audit](../development/2026-09-08-current-state-audit.md) contains requirement → task → code → tests → runtime → status → remaining work. Every policy claim must be rechecked against the proposed release SHA.

| Promise/control | Requirement/task | Implementation and tests at baseline | Status | Release-blocking dependency |
|---|---|---|---|---|
| Tenant/site isolation and auditable mutation | FR-204, FR-206; T-0310–T-0319 | Go authz, tenant transactions, Supabase RLS, audit chain and corresponding tests | IMPLEMENTED_NEEDS_VALIDATION | Deployed authorization/isolation and access review |
| Notice and policy versioning | FR-206; T-0405, T-0407 | Draft files only; no served privacy notice/version binding | PARTIAL | Counsel/contact approval, versioned inventory validation, accessible UI and renewal rules |
| Consent and withdrawal | FR-102, FR-206; T-0386–T-0392 | No consent ledger/service; no withdrawal propagation | BLOCKED_APPROVAL | Purpose/jurisdiction/PIA/alternative and lifecycle approvals before biometric implementation |
| Rights intake and human fulfillment | FR-206 | No endpoint, persistent case/owner queue or UI | NOT_IMPLEMENTED | Approved intake, verification, schedule and private case/evidence store |
| Retention configuration | FR-206; T-0397–T-0398 | Tenant 7–365/default-30 SQL setting; no category enforcement | PARTIAL | Category and jurisdiction approval, workers, scheduler and monitored failures |
| Complete erasure | FR-206; T-0397–T-0398 | No multi-store deletion/restore reconciliation | BLOCKED_APPROVAL | Store inventory, hold/backup design, atomic audit/outbox and completeness testing |
| Biometrics unavailable until approved | FR-102; T-0386–T-0392, T-0406 | Dedicated functionality absent, but generic event vocabulary accepts attendance review | PARTIAL | Reject generic attendance intake; dedicated implementation remains blocked |
| Physical masking and signed evidence | FR-117, FR-206; T-0340–T-0354 | Tested metadata/approval/HIL-signature boundaries; no certified physical pixel path | PARTIAL | Approved hardware/HIL, encrypted evidence store, access audit and release evidence |
| Safe logs/browser egress | FR-206; T-0406 | Raw entrypoint error logs and remote font import found | PARTIAL | Fixed codes, no provider/config details; remove unnecessary font request and test |
| Session invalidation | FR-204; T-0380–T-0385 | Local suspension and pending intent; invitation-only worker | BLOCKED_APPROVAL | Product/Security/Infrastructure checklist; existing token treatment |
| Processor/transfer transparency | FR-206; T-0405 | SDK/target references, no verified production register | BLOCKED_APPROVAL | Actual entities/regions/DPA/telemetry inventory |
| Incident and recovery procedures | T-0398, T-0400–T-0403 | Proposed documents and CI, no exercised operational evidence | BLOCKED_APPROVAL | Named response team, environment, private evidence, tests and approved communications |
| GA/publication | T-0309, T-0404 | Public source/no LICENSE; planning boundary; frontend deployment metadata | BLOCKED_APPROVAL | Final source/legal/commercial and GA human decisions |

## Proposed foundation contracts after approval

These are review inputs, not a new architecture or operational database. Follow ADR-009: authoritative Supabase migrations, business mutations in Go, tenant-scoped transaction with audit/outbox, no direct browser mutation or privileged provider call.

**Consent:** opaque consent ID, tenant, subject, consent type, immutable policy/notice version, jurisdiction, explicit purpose IDs, granted timestamp, affirmative method, actor, audit reference, status and withdrawal timestamp. Retain the notice version actually accepted; prevent broad-purpose or version substitution. Derived activation must check both current consent and independent deployment/model approval. Revocation must override delayed or replayed grants. Store no face template, token or raw identity evidence in generic consent/audit payloads. No real or synthetic biometric consent ledger is implemented while the checklist blocks it.

**Rights case:** opaque case ID, tenant and subject, request kind, verified actor, verification state/method, receipt/update/due timestamps, legal-clock reference, assigned authorized owner, workflow state/version, bounded evidence reference and completion/partial/rejection reason. A withdrawal request is not equivalent to completing withdrawal; an erasure request is not an erasure manifest. Refuse foreign-tenant assignments, stale transitions, unauthorized subject changes and fabricated completion. Provide accessible private intake for non-account holders.

**Retention policy:** per-tenant category, purpose, period/start trigger, applicable minimum/maximum, approved exception, version, effective date, review owner/date and evidence reference. Unknown category/approval fails closed. Do not set an unreviewed schedule for existing personal records or rewrite ADR-006 silently.

**Deletion operation:** tenant/subject scope, approval/request reference, immutable store coverage list, per-store outcomes and retry state, legal holds, backup-expiry state, audit reference and explicit partial/failure result. No “complete” until all applicable stores reconcile. Isolate restore copies until erasure/withdrawal is reapplied. Production activation requires tests of partial failures and foreign-tenant paths.

## Evidence required for subsequent engineering PRs

Use the current task-ID allocator in `traceability/tasks.json`. Link the relevant FR and existing gated dependency; do not mark a template as runtime completion. Each approved workflow slice needs contract compatibility, unit tests, durable transaction/rollback tests, tenant/site and subject authorization negatives, audit integrity, migration/RLS tests, retry/concurrency cases, safe errors, UI/accessibility and E2E. Consent tests must cover missing/withdrawn/stale consent and purpose/version changes. Deletion tests must cover every applicable store, held records, failures, backup expiry and restored copies. AI work additionally needs approved provenance, evaluation and human promotion; fabricated metrics are forbidden.

Evidence references and approval status belong in a private approved system where required. Public documentation should link only safe review references, not incident logs, consent, footage, biometric data, customer identities, credentials or generated production evidence. A future validator can check document completeness and draft/version consistency; it cannot grant legal approval.
