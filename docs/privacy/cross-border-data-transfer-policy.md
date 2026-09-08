# Cross-border data-transfer policy

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Owner: Privacy/Legal and Platform.

No universal residency guarantee is established. Database region fields and architecture targets do not pin hosted databases, backups, logs, support access, identity traffic or browser third-party requests. ADR-009 retains Supabase for the temporary MVP and defers cloud evidence storage and later adapters. Do not provision a country/region from this draft.

## Required deployment record

Before cross-border processing, identify the tenant/purpose and role, originating country and affected populations, category, exporter/importer, storage/replica/cache locations, remote access locations, onward recipients, network path, encryption and key location, legal mechanism/restriction, risk assessment, contract, retention/deletion and incident cooperation. Record reviewer, date and review trigger. Keep the operational map private; publish only approved notices and non-sensitive references.

- India: assess DPDP section 16 and staged rule 15, current government orders and sector-specific/local requirements. Do not describe a universal whitelist. CERT-In's applicable ICT-log location duties require independent assessment now. Future obligations do not suspend current safeguards.
- Canada: accountability for service-provider processing persists. Evaluate PIPEDA cross-border commercial flows and provincial requirements; Quebec section 17 requires an assessment before communicating personal information outside Quebec and appropriate contractual protection. Alberta requires specific consideration of service providers outside Canada. Customer choice alone does not establish legal adequacy.
- United States: determine applicable service-provider/contractor terms, state/sector obligations and other restrictions for the actual data and recipients. Consent is not a universal export permission. No broad U.S. transfer clearance is asserted.

The [source matrix](privacy-jurisdiction-matrix.md) records the reviewed instruments. Any new country, subprocessor, support path, backup region, category or purpose triggers re-review before use. Where an intended transfer lacks a required assessment or restriction check, hold the transfer and escalate; do not fall back to an arbitrary region.

## Engineering verification required

Verify actual runtime endpoints/regions and browser requests using synthetic data; confirm least privilege, encryption, regional backup behavior and deletion/restore reconciliation. Test that configuration cannot silently route a tenant to an unapproved region and that missing approved configuration fails closed. No such production verification has been performed by this package. The current application must not promise data stays in India, Canada or the United States merely because a setting names that country.
