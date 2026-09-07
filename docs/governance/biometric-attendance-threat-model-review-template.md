# Biometric attendance threat-model review template

## Status

Preparation only. This template records neither a completed threat model nor a
risk acceptance. It does not authorize or implement face attendance, face
recognition, enrollment, matching, or liveness.

## Purpose

T-0386 requires Security approval of a threat model before a future biometric
attendance implementation task can begin. T-0387 identifies the approval
evidence that must exist. Use this template to structure that future review.

The completed review must be stored in the approved security-review system.
This repository may contain only a non-sensitive reference to the approved
review. Do not attach or copy personal or biometric data, consent records,
customer footage, datasets, templates, model artifacts, credentials, live
architecture details, infrastructure identifiers, security findings, or
exploit information here.

## Review boundary

- [ ] Identify the proposed attendance purpose, tenant/site scope, and
  non-biometric alternative by reference to the approved Product decision.
- [ ] Identify the applicable jurisdiction, approved consent/withdrawal design,
  retention/deletion proposal, and privacy impact assessment by reference to
  the approved Privacy and Legal record.
- [ ] Identify each proposed model only through its approved ADR-001 release
  evidence reference. ADR-001 remains proposed until Legal accepts it.
- [ ] Confirm that the review covers the proposed future lifecycle without
  recording implementation design: consent, enrollment, processing, human
  review, correction/dispute, withdrawal, deletion/reconciliation, and audit.
- [ ] Name the review owner, reviewers, decision date, scope, and a
  non-sensitive evidence reference.

## Required threat scenarios

For each scenario, the completed review must record the decision owner,
severity method, mitigations, validation approach, residual-risk disposition,
and non-sensitive evidence reference in the approved security-review system.
No scenario is accepted merely by appearing in this template.

- [ ] Enrollment fraud or an unauthorized attempt to establish a biometric
  association.
- [ ] Replay or presentation attack against the future liveness control.
- [ ] Theft, misuse, or unauthorized disclosure of future biometric templates
  or related sensitive records.
- [ ] Cross-tenant or cross-site disclosure through a future data, model,
  queue, cache, log, export, or human-review boundary.
- [ ] Abusive or excessive privileged-operator access, including review,
  correction, and dispute handling.
- [ ] Missing, withdrawn, expired, or invalid consent; unsupported site;
  unavailable model; missing liveness; and ambiguous-match conditions.
- [ ] Retention, deletion, or withdrawal-reconciliation failure, including
  safely handling an incomplete or ambiguous outcome.
- [ ] Exposure of sensitive information through future audit events, logs,
  alerts, ordinary analytics, URLs, browser state, or error handling.
- [ ] Degraded availability, integrity, or time/order behavior that could lead
  to an incorrect automatic attendance result.

## Required security conclusions

- [ ] Define least-privilege duties and separation for any future enrollment,
  review, correction, deletion, and operational support activities.
- [ ] Define tenant/site isolation and deny-by-default authorization
  expectations for every future sensitive boundary.
- [ ] Confirm future privileged actions require append-only audit events with
  safe fields only; biometric information must not enter generic event payloads
  or client-visible logs.
- [ ] Define the fail-closed behavior for every required scenario: no automatic
  attendance record may be created when consent, approval, liveness, model
  availability, scope, or match confidence is inadequate.
- [ ] Define safe incident, access-review, and deletion/reconciliation
  ownership. Do not place runbook internals, credentials, or live identifiers
  in this template.
- [ ] Confirm that an approved threat model does not by itself authorize
  implementation, model promotion, deployment, or production use.

## Evidence required before implementation is proposed

- A Product, Privacy, and Legal-approved attendance purpose, consent,
  withdrawal, human-review, and retention/deletion decision.
- A completed Security threat-model review that addresses every required
  scenario and documents accepted residual risk through the approved process.
- A Platform review of future tenant/site isolation, safe audit boundaries,
  deletion/reconciliation, and fail-closed operational behavior.
- Per-model ADR-001 release evidence with Legal approval and the planned
  dataset provenance/consent evidence required by T-0362/T-0363 where a future
  dataset is proposed.
- AI, Privacy, Product, and human-oversight approval of the pre-declared
  evaluation and fairness criteria.

Until all required evidence exists, no biometric attendance implementation may
be proposed or added. In particular, do not add biometric or personal data,
consent records, enrollment, templates, matching, liveness inference, customer
footage, datasets, model artifacts, evaluation results, APIs, browser routes,
activation, promotion, deployment, or production configuration.
