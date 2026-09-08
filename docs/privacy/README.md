# Privacy review package

> Draft operational privacy policy — requires review and approval by qualified legal/privacy counsel before production use in the applicable jurisdiction.

Version: `2026-09-08.draft-2`. Research checked: 2026-09-08. Owner: Privacy and Legal (individuals unassigned).

This package describes the inspected repository, separates intended controls from implemented controls, and supplies proposed notices for review. It is not an approved customer policy, consent form, deployment instruction, or compliance determination. No notice is served by the application by this change. Publication in this source repository does not authorize operational use.

- [Global policy](global-privacy-policy.md)
- [India](india-privacy-supplement.md), [Canada](canada-privacy-supplement.md), [United States](us-privacy-supplement.md)
- [Biometric notice](biometric-and-face-recognition-notice.md), [employee attendance notice](employee-attendance-privacy-notice.md), [camera notice](camera-video-surveillance-notice.md)
- [Retention and deletion](data-retention-and-deletion-policy.md), [individual requests](data-subject-request-procedure.md), [incidents](privacy-incident-and-breach-procedure.md)
- [Subprocessors](subprocessor-governance.md), [cross-border processing](cross-border-data-transfer-policy.md)
- [Jurisdiction and primary-source matrix](privacy-jurisdiction-matrix.md)
- [Implementation traceability and blockers](privacy-implementation-traceability.md)
- [Fresh repository audit](../development/2026-09-08-current-state-audit.md)

Reviewers must resolve legal identity/contact details, deployment jurisdictions and roles, category-specific schedules, actual processor/region inventory, and operational rights/incident ownership before approving notices. Bracketed fields in proposed notices are intentionally unresolved and must not be published as completed notices. Store personal records and approval evidence only in an approved private system; this repository may hold bounded, non-sensitive references.

The existing [biometric checklist](../governance/biometric-attendance-approval-evidence-checklist.md), [session checklist](../governance/session-revocation-approval-checklist.md), [data-path boundary](../development/phase-9-data-path-delivery-boundary.md), and [GA boundary](../development/sprint-12-ga-readiness-delivery-boundary.md) continue to apply. T-0309 remains unresolved. Do not use the draft to enable a capability.

Changes to a notice require a new version, a reviewed processing inventory, and assessment of whether renewed notice or consent is necessary. Consent to an old purpose must not silently authorize a new one. Git history preserves draft changes; it is not a consent ledger.

## Document inventory check — T-0407

The [policy inventory](policy-inventory.json) records each of the 14 documents, its individual draft version and SHA-256 of the UTF-8 file bytes with LF line endings (the repository checkout convention). Versions can differ by document. The [validator](../../scripts/validate_privacy_policies.py) runs through both canonical verification commands and rejects missing/duplicate/unlisted policy documents, unsafe paths, mismatched versions or content, missing prominent warnings, and non-draft status. The inventory references canonical local traceability; the existing Markdown validator separately checks document links.

For a substantive change, increment the affected document's dated draft version, explain the changed claims and implementation evidence, then update its inventory version and digest in the same PR. Compute the digest after saving the final LF file using `python -c "import hashlib,pathlib; print(hashlib.sha256(pathlib.Path('docs/privacy/global-privacy-policy.md').read_bytes()).hexdigest())"` with the affected path. Run `python scripts/validate_privacy_policies.py` and `python -m unittest discover -s tests/validators -v` before canonical verification. Do not use a digest refresh to hide an unreviewed change.

The digest detects accidental content drift; it is neither a signature nor proof of legal review. Reviewers remain responsible for assessing changes and requiring a new version; the validator cannot judge whether a textual change is substantive or stop someone from deliberately updating a digest without incrementing a version. This is source-document versioning, not immutable notice storage, a consent ledger, automated rights fulfillment, or an operational policy activation mechanism. An approval workflow requires a separately authorized proposal. Draft 2 of this index documents the validator; it does not change the legal research dates of the individual drafts.
