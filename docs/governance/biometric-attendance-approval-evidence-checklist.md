# Biometric attendance approval-evidence checklist

## Status

Preparation only. This checklist grants no approval and does not permit the
implementation, configuration, deployment, or operation of face attendance,
face recognition, enrollment, or liveness.

## Purpose

T-0386 defines the consent-first boundary for the future FR-102 attendance
capability. Complete, record, and approve every item below before proposing a
biometric implementation task. References to evidence must identify an
approved review record without copying consent records, personal data,
biometric data, customer footage, datasets, model artifacts, credentials, or
security-sensitive details into this repository.

This checklist does not replace jurisdiction-specific legal advice, a privacy
impact assessment, security review, ADR-001 Legal approval, or the future
implementation task's own design and test review.

## Product decision

- [ ] Define the specific attendance purpose and the authorized tenant sites.
- [ ] Identify the eligible population and a non-biometric alternative for
  every person who declines or withdraws consent.
- [ ] Define the proposed review, correction, dispute, and appeal path; no
  result may be used as an automatic payroll, disciplinary, employment,
  safety, or access-control decision.
- [ ] Propose a retention and deletion schedule, including the operational
  owner and the requested deletion-verification evidence.
- [ ] Record the product decision owner, date, scope, and non-sensitive review
  reference.

## Privacy and Legal approval

- [ ] Identify every jurisdiction in scope and record Privacy and Legal
  approval of the permitted biometric basis for each one.
- [ ] Approve informed, voluntary, specific opt-in wording that is separate
  from general terms and names the attendance purpose, sites, and retention
  proposal.
- [ ] Approve an accessible withdrawal process and confirm that withdrawal
  blocks future biometric use pending the defined deletion/reconciliation path.
- [ ] Complete and approve the required privacy impact assessment, including
  retention/deletion, processor/subprocessor, cross-border transfer, and
  data-subject-rights considerations where applicable.
- [ ] Record only the decision owner, date, scope, and non-sensitive evidence
  reference. Do not store any consent record in this checklist or repository.

## Security and platform approval

- [ ] Approve a threat model covering enrollment fraud, template theft,
  replay/presentation attacks, cross-tenant disclosure, abusive operator
  access, audit-log exposure, and retention/deletion failure.
- [ ] Define least-privilege roles, tenant/site isolation boundaries, and the
  append-only audit events required for each future privileged action.
- [ ] Define how withdrawn consent, deletion requests, unavailable approvals,
  unavailable models, missing liveness, and ambiguous matches fail closed with
  no automatic attendance result.
- [ ] Define the deletion/reconciliation ownership, safe observability fields,
  incident response, and access-review expectations without recording live
  infrastructure identifiers or secrets.
- [ ] Record the security and platform reviewers, date, scope, and
  non-sensitive evidence reference.

## AI, model, and evaluation approval

- [ ] For every proposed face-detection, recognition, or liveness model,
  complete ADR-001's allowed-license, weight-provenance, model-card,
  signature, rollback-metadata, and Legal-approval requirements.
- [ ] For every proposed future dataset, complete the planned T-0362/T-0363
  provenance and consent prerequisites. Do not add a dataset, pointer, remote,
  checksum, label, footage, or evaluation result to this repository.
- [ ] Pre-declare held-out evaluation and fairness criteria for false-match,
  false-non-match, liveness attack, demographic, site/lighting, consent
  withdrawal, and deletion scenarios.
- [ ] Confirm that evaluation evidence will be subject to AI, Privacy, Product,
  and human-oversight review and that a passing result alone cannot authorize
  promotion or production use.
- [ ] Record only the approvers, date, scope, and non-sensitive evidence
  reference. ADR-001 remains proposed until Legal accepts it.

## Required evidence before implementation is proposed

- A Product decision record covering purpose, scope, alternatives, human
  review, correction/dispute, and proposed retention/deletion behavior.
- A jurisdiction-scoped Privacy and Legal approval record, including approved
  opt-in and withdrawal design and the privacy impact assessment.
- A Security and Platform threat-model sign-off covering isolation, audit,
  failure, deletion/reconciliation, and incident controls.
- Per-model ADR-001 release evidence and Legal approval, plus planned dataset
  provenance/consent evidence where a future dataset is proposed.
- AI, Privacy, Product, and human-oversight approval of pre-declared
  evaluation and fairness criteria.

Until every item exists and is approved, the repository must not add biometric
implementation, consent or attendance records, enrollment, templates,
matching, liveness inference, customer footage, datasets, model artifacts,
evaluation results, API or browser changes, activation, promotion, deployment,
or production configuration.
