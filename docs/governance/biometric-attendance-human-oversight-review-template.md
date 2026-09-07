# Biometric attendance human-oversight review template

## Status

Preparation only. This template grants no approval, creates no human-review
queue, and does not authorize face attendance, face recognition, enrollment,
matching, liveness, or any attendance outcome.

## Purpose

T-0386 requires a documented human-review path for every future disputed
attendance result and keeps liveness from becoming an automatic employment,
payroll, disciplinary, safety, or authorization decision. Use this template to
structure the future Product, Privacy, Security, AI, and Platform review before
an implementation task is proposed.

The completed review must be stored in the approved review system. This
repository may contain only a non-sensitive reference to that record. Do not
place biometric or personal data, consent records, customer footage, future
case details, templates, model artifacts, credentials, or live operational
details in this template or repository.

## Review scope and ownership

- [ ] Identify the attendance purpose, authorized sites, eligible population,
  and non-biometric alternative by reference to the approved Product decision.
- [ ] Identify the jurisdiction-scoped consent, withdrawal, retention/deletion,
  and privacy-impact approvals by reference to the approved Privacy and Legal
  record.
- [ ] Define the future human roles, separation of duties, authorization, and
  escalation ownership for review, correction, dispute, deletion, and support.
- [ ] Confirm that future reviewers receive only the minimum authorized context
  needed for their assigned work and cannot use the process for surveillance or
  unrelated identity decisions.
- [ ] Record the review owner, reviewers, date, scope, and non-sensitive
  evidence reference.

## Required future review paths

For each path, the completed review must define the proposed authority,
evidence standard, privacy boundary, safe outcome category, and escalation
route. Listing a path here does not approve it or create an operational
procedure.

- [ ] A person declines biometric attendance and uses the defined alternative.
- [ ] A person withdraws consent; future biometric use must stop and the
  approved deletion/reconciliation path must be available.
- [ ] A future attendance result is disputed, missing, duplicated, delayed, or
  otherwise ambiguous.
- [ ] Required consent, site authorization, model approval, liveness result,
  or match certainty is absent or invalid; the result must fail closed rather
  than creating an automatic attendance outcome.
- [ ] A future reviewer corrects a record or declines a proposed outcome.
- [ ] A person requests an explanation, correction, appeal, or other
  jurisdiction-applicable data-subject action.
- [ ] A suspected fairness, misuse, access-control, retention, or deletion
  issue requires escalation to the designated owners.

## Fairness and quality oversight

- [ ] Confirm that pre-declared evaluation and fairness criteria address
  false-match, false-non-match, liveness attack, demographic, site/lighting,
  withdrawal, and deletion scenarios.
- [ ] Define who may review future aggregated evaluation evidence and how
  Privacy, Product, AI, and human oversight determine whether follow-up is
  required. Do not add evaluation evidence or results to this repository.
- [ ] Confirm that a model score, liveness result, or evaluation result alone
  cannot decide attendance, employment, payroll, discipline, safety, or access.
- [ ] Confirm that a future reviewer can escalate rather than override a
  missing approval, ambiguous outcome, or safety/privacy concern.

## Future audit and privacy boundaries

- [ ] Define append-only audit requirements for future privileged actions using
  safe fields only: authorized actor role, action category, time, tenant/site
  scope, request reference, and bounded outcome/reason category.
- [ ] Confirm that biometric information, images, template values, matching
  details, and liveness data do not enter generic events, ordinary analytics,
  browser-visible logs, URLs, or audit payloads.
- [ ] Define the review of privileged access, correction, dispute, withdrawal,
  and deletion/reconciliation audit records without recording case details or
  live system identifiers here.
- [ ] Confirm that the completed review does not by itself authorize a future
  implementation, model promotion, deployment, or production use.

## Evidence required before implementation is proposed

- An approved Product decision covering purpose, alternatives, human review,
  correction/dispute, withdrawal, and proposed retention/deletion behavior.
- Jurisdiction-scoped Privacy and Legal approval, including the privacy impact
  assessment and opt-in/withdrawal design.
- Security threat-model approval and Platform review of future isolation,
  safe-audit, deletion/reconciliation, and fail-closed behavior.
- ADR-001 Legal approval and required release evidence for each proposed model,
  plus planned dataset provenance/consent evidence where a future dataset is
  proposed.
- Approved AI, Privacy, Product, and human-oversight evaluation and fairness
  criteria.

Until all required evidence exists, no biometric attendance implementation may
be proposed or added. Do not add biometric or personal data, consent records,
enrollment, templates, matching, liveness inference, customer footage,
datasets, model artifacts, evaluation results, APIs, browser routes,
activation, promotion, deployment, or production configuration.
