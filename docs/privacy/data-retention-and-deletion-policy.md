# Data retention and deletion policy

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Owner: Privacy/Legal and Platform. **No automated cross-store deletion promise is supported by the current runtime.**

[ADR-006](../adr/ADR-006-retention.md) selects 7–365 days, UI presets and a 30-day default. Tenant SQL stores that range/default. The later [data-path boundary](../development/phase-9-data-path-delivery-boundary.md) requires actual category/jurisdiction schedules, exceptions and recovery decisions before lifecycle implementation. An expiry column or a bounded spool does not enforce time-based deletion. Camera retirement changes status; it is not erasure.

## Schedule to approve before collection

For each category require tenant/purpose, authoritative and derived stores, duration, start trigger, legal minimum/maximum, subject locator, deletion method, owner, exception/review date and verification evidence. No new indefinite default is authorized. Unresolved schedules block affected collection; they must not be filled with a generic 30-day promise.

| Category | Current evidence | Required schedule decision |
|---|---|---|
| Account/membership | Supabase identity and membership SQL | Active relationship, closure and legal/security record limits |
| Site/camera/device configuration | Configuration/device tables and revisions | Retirement/decommission trigger; credential/certificate revocation; old revisions |
| Event metadata | Event store, outbox and idempotency data | Purpose window and expiry across source/delivery copies |
| Alerts/realtime | Projections and delivery state | Coordinated expiry with source events and review needs |
| Evidence clips/images | References and edge opaque spool library; cloud storage deferred | Object versions, replicas, thumbnails, export links and access records |
| Audit/security/access records | Hash-chain mutations; ordinary application logs | Legal/security minimum, privacy minimization, bounded exception and archive design |
| Attendance | Not implemented | Employment/legal obligations separated from optional biometrics |
| Biometric templates, images, liveness and indexes | Not implemented; blocked | Purpose completion/withdrawal and jurisdiction ceilings; separate image and template treatment |
| Consent/withdrawal evidence | Not implemented | Proof-of-choice necessity after withdrawal; minimal data and explicit expiry |
| Rights requests/deletion manifests | Not implemented | Case closure, complaint/claim obligations, bounded pseudonymous proof |
| Backups/replicas | No tested lifecycle/restore deployment | Finite rotation, restricted access and pre-reentry deletion reconciliation |

India's staged Rules include retention provisions requiring separate assessment; CERT-In may require specific logs now. BC decision-record retention and U.S. biometric destruction rules differ. Do not force a conflicting legal minimum/maximum into ADR-006 without an approved exception/ADR. The [source matrix](privacy-jurisdiction-matrix.md) provides inputs, not a chosen customer schedule.

## Proposed deletion orchestration

First authenticate the requester/authority and tenant, determine applicable scope and legal holds, and assign a human owner. Freeze future consent-based use where required. Create a bounded operation identifier and private store inventory. Process the primary database, event/alert/realtime projections, idempotency/outbox copies, search/analytics indexes, object versions, caches, edge spools, eventual biometric indexes and relevant processors. A store that is not deployed is explicitly not applicable; it is not reported as successfully erased.

Track each store as pending, completed, failed, lawful hold or backup expiry pending, with safe reason codes and private evidence references. A missing store, expired credential, failed processor instruction or ambiguous response must prevent a full-erasure completion claim. Retries must be idempotent and scoped; no browser can directly run a privileged deletion. Test foreign-tenant requests, partial failures and retries before rollout.

Legal holds require authority, exact scope, purpose, restricted access, owner and review/expiry date. They cannot become a hidden indefinite retention mode. Explain lawful exceptions to the person; seek counsel where disclosure itself is restricted. Preserve only the justified minimum of audit/consent proof. Immutability does not automatically trump erasure rights.

Backups must have an approved finite lifecycle. Any restored copy stays isolated until deletion/withdrawal manifests are reapplied and tenant isolation verified. No erased subject may silently return to active processing. Keep separately approved recovery measurements; do not claim RTO/RPO from a template.

This procedure is a proposed engineering specification. No worker, schedule, legal hold, erasure operation, backup or deployment is created. Implementation remains blocked on T-0397 approvals.
