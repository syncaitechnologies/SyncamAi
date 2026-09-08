# Subprocessor governance

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Owner: Privacy/Legal and vendor-management owner (unassigned).

This is a review inventory, not an approved production subprocessor list. A dependency, SDK or architecture diagram does not establish an executed DPA, actual recipient, processing country, retention schedule or permission to send customer information.

| Provider/path | Repository evidence | Known/unknown boundary |
|---|---|---|
| Supabase | ADR-009, Auth SDK, authoritative migrations, invitation provider | Temporary MVP integration; actual project region, contractual entity, subprocessors, support access and retention not verified |
| Vercel | `vercel.json`, GitHub frontend deployment records | Web build/hosting target; account configuration, request logs, analytics, regions and vendor terms not verified |
| Google Fonts | Baseline `frontend/apps/web/src/styles.css` external CSS import | Browser network request; must be disclosed in baseline inventory or removed and verified |
| Cloudflare | Reserved adapter boundary in ADR-009 | No Worker/Queue/R2 integration or data recipient created by that ADR |
| AWS and other providers in original architecture | Planning sources and non-provisioning Terraform scaffold | No current runtime recipient inferred from a diagram |
| Customer identity, camera and support systems | Deployment-dependent | Customer must inventory independent processing; no real customer endpoint inspected |

Before approval record the legal provider name, service, purpose, data categories, customer/SyncCam role, countries and remote support locations, onward providers, access boundaries, retention and deletion/return arrangements, contractual incident deadline, audit/assurance review, exit plan and approving owner/date. Keep private account identifiers, credentials and contractual evidence outside the public repository. A public register may include only reviewed non-sensitive facts.

Contracts must restrict processing to documented instructions, impose confidentiality and safeguards, control further subprocessors, require assistance with requests/incidents and deletion, and allocate change notification/objection where applicable. Assess government-access and cross-border issues, including backups and vendor diagnostics. Evidence of reasonable safeguards must cover the actual service used, not a generic provider certification badge.

Review before onboarding or material change and on the approved review cadence. A new browser SDK or remote resource can create a recipient even when no backend API changes. Review egress, telemetry and retention before adding it. If information about a recipient is missing, block the affected deployment or remove the unnecessary transfer; do not invent its location or DPA status.

For customer processing, SyncCam remains responsible for processor selection/supervision as applicable. For independently determined purposes, assess its separate role and notice obligations. Link approved transfers to the [cross-border policy](cross-border-data-transfer-policy.md) and store-specific deletion evidence to the [retention policy](data-retention-and-deletion-policy.md).
