# Privacy incident and breach procedure

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-1`. Proposed runbook; no staffed incident-response coverage, notification service or exercised runbook is proved. Replace responsible-role placeholders privately before operational use.

## Response workflow

The designated incident lead should register discovery and awareness timestamps, affected tenant/system/category and a safe private reference. Notify Security, Privacy/Legal and the responsible customer promptly under the approved contract. Preserve minimal evidence with integrity and access controls, contain ongoing exposure, and coordinate recovery. Avoid copying raw provider errors, personal information, footage, templates or credentials into logs or public issues. The private reporting direction in [SECURITY.md](../../SECURITY.md) applies.

Assign an incident commander, privacy decision owner, technical containment owner, communications approver and backup contact. Determine controller/processor roles, affected jurisdictions/residents, nature/scope, encryption/key exposure, likely harms, recipients, applicable legal trigger and earliest due time. Do not wait for a complete forensic account before making a required initial notification. Record decisions, including a reasoned non-notification decision, in the approved private system.

## Distinct legal clocks to evaluate

| Framework | Trigger/timing input | Operational implication |
|---|---|---|
| India CERT-In directions | Specified reportable cyber incidents: six hours from noticing/being brought to notice; applicable ICT log obligations | Assess now, separately from future DPDP duties; preserve/report available information and supplement it |
| India DPDP Rules rule 7 (staged; not yet operative as checked) | Affected individuals and initial Board intimation without delay; detailed Board information within 72 hours unless an extension is allowed | Build distinct initial/update timers; the old 30-day DPDP statement is incorrect |
| PIPEDA | Real risk of significant harm: report to OPC and notify affected individuals as soon as feasible after determination | No blanket 72-hour deadline; preserve breach records for at least 24 months under the Regulations |
| Alberta PIPA | Real risk of significant harm: notify Commissioner without unreasonable delay | Evaluate individual notification duties/directions and other applicable laws |
| Quebec private-sector Act | Risk of serious injury: notify CAI and affected people promptly; incident register required | No generic 72-hour rule; assess sensitivity, consequences and likelihood |
| BC private-sector PIPA | Review OIPC guidance; no equivalent universal statutory mandatory regulator clock identified in the inspected PIPA | Consider voluntary notice, safeguards and other legal/contract duties; do not import public-sector rules |
| United States | Applicable state/sector laws differ in personal-data definition, harm test, resident/AG thresholds and deadlines | Counsel records each applicable current rule and action; there is no national 72-hour rule |

Primary instruments and dates are listed in the [source matrix](privacy-jurisdiction-matrix.md). Revalidate law at incident time. Internal escalation targets do not extend legal deadlines. Processor/customer agreements may require earlier notice than statute. No customer communication is sent by creating this document.

## Special handling and closure

Biometric exposure can have persistent identity and discriminatory harms even if no password was involved. Assess template reconstructability, linkage, key exposure, affected sites, false-match risks and withdrawal/deletion failures. Human oversight and purpose restrictions remain in effect during containment. Do not automatically notify law enforcement or make employment decisions from an AI alert.

Approved customer/individual notices should describe known nature/timing, categories, risks, mitigation, steps the person can take and an operational contact; use accessible language and disclose uncertainty. Legal must review permitted delay/exceptions, recipients and secure delivery without suppressing required notices. Record send/receipt and update references privately.

Recovery requires tenant-isolation checks, key/session remediation through authorized procedures, tested restoration and deletion reconciliation. Existing session-revocation worker approval cannot be bypassed by invoking this draft. Before closure, review root cause, affected processing, control gaps, notification completeness and lessons learned; assign remediation owners and due dates. Exercise the runbook only in an approved isolated environment, with no customer data or secrets.
