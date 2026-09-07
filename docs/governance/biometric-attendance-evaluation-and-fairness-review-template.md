# Biometric attendance evaluation and fairness review template

## Status

Preparation only. This template records no threshold, metric, dataset, model,
evaluation result, model-card claim, approval, promotion, deployment, or
production use. It does not authorize or implement face attendance, face
recognition, enrollment, matching, or liveness.

## Purpose

T-0386 requires AI, Privacy, and Product approval of pre-declared held-out
evaluation and fairness criteria for the future biometric attendance boundary.
ADR-001 and T-0362/T-0363 remain mandatory for every future model and dataset.
Use this template to structure that future review before an implementation task
is proposed.

The completed review must be retained in the approved AI and governance review
systems. This repository may contain only a non-sensitive evidence reference.
Do not add biometric or personal data, customer footage, datasets, labels,
checksums, model weights, model artifacts, metrics, thresholds, results,
demographic information, credentials, or live environment details here.

## Review boundary and ownership

- [ ] Identify the proposed attendance purpose, authorized sites, eligible
  population, and non-biometric alternative by reference to the approved
  Product decision.
- [ ] Identify the approved jurisdiction-scoped consent, withdrawal,
  retention/deletion, privacy-impact, and human-oversight records by reference
  only.
- [ ] Identify each proposed face-detection, recognition, or liveness model
  through its ADR-001 evidence reference; ADR-001 remains proposed until Legal
  accepts it.
- [ ] Identify each proposed future evaluation dataset through the planned
  T-0362/T-0363 provenance and consent evidence reference only.
- [ ] Record the review owner, reviewers, date, scope, and non-sensitive
  evidence reference.

## Required future evaluation design

For each item below, the completed review must pre-declare the purpose,
population/scope, methodology, success and escalation criteria, evidence
location, and decision owner. Listing an item here creates no data, test, or
result and does not decide an acceptable threshold.

- [ ] Held-out evaluation that is separated from development and respects the
  approved provenance, license, consent, and purpose restrictions.
- [ ] False-match and false-non-match evaluation, including the handling of
  ambiguity and the required fail-closed outcome.
- [ ] Liveness presentation/replay-attack evaluation across the approved threat
  scenarios from T-0388.
- [ ] Demographic fairness review using legally and ethically appropriate,
  approved categories and handling; no demographic data belongs in this
  template or repository.
- [ ] Site, lighting, camera, and operational-condition evaluation to identify
  where a future capability must remain unavailable or require extra review.
- [ ] Consent-decline, consent-withdrawal, retention/deletion, correction, and
  dispute-path evaluation aligned with T-0389 and T-0390.
- [ ] Tenant/site isolation, authorization, safe-audit, and human-oversight
  verification that treats any incomplete or ambiguous result as fail closed.

## Governance and decision boundaries

- [ ] Define how AI, Privacy, Product, Security, Legal, and human oversight
  review future evidence, exceptions, limitations, and follow-up obligations.
- [ ] Confirm that no evaluation result alone authorizes model release,
  promotion, implementation, deployment, or production use.
- [ ] Confirm that a model score or liveness result cannot create an automatic
  attendance, employment, payroll, disciplinary, safety, or access decision.
- [ ] Define the non-sensitive audit and escalation reference for a future
  rejected, incomplete, or concerning evaluation outcome; do not capture
  results or case details in this template.
- [ ] Confirm that any future implementation must keep the generic event
  payload, ordinary analytics, browser-visible logs, and URLs free of biometric
  information.

## Evidence required before implementation is proposed

- An approved Product, Privacy, and Legal decision covering purpose,
  alternatives, consent/withdrawal, human oversight, and proposed lifecycle.
- A Security threat-model and Platform review covering isolation, safe audit,
  failure handling, and deletion/reconciliation.
- ADR-001 Legal approval and required release evidence for each proposed model.
- Planned T-0362/T-0363 provenance and consent evidence for each proposed
  future dataset.
- AI, Privacy, Product, Security, Legal, and human-oversight approval of the
  completed pre-declared evaluation and fairness design.

Until all required evidence exists, no biometric attendance implementation may
be proposed or added. Do not add biometric or personal data, consent records,
enrollment, templates, matching, liveness inference, customer footage,
datasets, model artifacts, evaluation results, APIs, browser routes,
activation, promotion, deployment, or production configuration.
