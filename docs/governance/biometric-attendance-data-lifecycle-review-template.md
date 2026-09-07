# Biometric attendance data-lifecycle review template

## Status

Preparation only. This template records no data-lifecycle decision, consent,
retention schedule, deletion request, or reconciliation result. It does not
authorize or implement face attendance, face recognition, enrollment, matching,
or liveness.

## Purpose

T-0386 requires a jurisdiction-specific consent, withdrawal, retention, and
deletion design before a biometric attendance implementation can begin. Use
this template to structure the future Privacy, Legal, Security, Product, and
Platform review.

The completed review must be kept in the approved privacy and security review
systems. This repository may contain only a non-sensitive evidence reference.
Do not add biometric or personal data, consent records, customer footage,
future record identifiers, templates, model artifacts, credentials, live data
stores, retention values, or infrastructure details here.

## Review boundary and ownership

- [ ] Identify the proposed attendance purpose, authorized tenant/site scope,
  eligible population, and non-biometric alternative by reference to the
  approved Product decision.
- [ ] Identify every jurisdiction in scope and the applicable approved
  biometric basis, data-subject rights, processor/subprocessor, and
  cross-border considerations by reference to Privacy and Legal records.
- [ ] Identify the proposed future sensitive-record categories at a category
  level only. Do not list values, schemas, store names, or sample records.
- [ ] Identify the required privacy impact assessment, decision owners,
  reviewers, date, scope, and non-sensitive evidence reference.
- [ ] Confirm that the review does not approve a data model, storage location,
  retention configuration, implementation, deployment, or production use.

## Required future lifecycle decisions

For each item below, the completed review must identify the lawful basis,
purpose limitation, authority, proposed verification evidence, and escalation
owner. Listing an item here does not make a decision or authorize processing.

- [ ] Define how future voluntary, specific, informed opt-in is kept separate
  from general terms and tied to the approved attendance purpose and site.
- [ ] Define how a person can decline or withdraw without losing the approved
  non-biometric alternative or silently preserving future-use permission.
- [ ] Define how withdrawal blocks future biometric use before any subsequent
  sensitive processing or attendance outcome can be created.
- [ ] Define purpose-bound proposed retention schedules, review triggers, and
  the treatment of jurisdiction-applicable legal holds or exceptions.
- [ ] Define the proposed deletion and deletion-verification path for each
  future sensitive-record category, including the safe outcome when deletion
  is incomplete, unavailable, or ambiguous.
- [ ] Define how future replicas, derived indexes, queued work, backups, and
  audit boundaries will be considered without weakening isolation, audit
  integrity, or deletion obligations.
- [ ] Define correction, dispute, explanation, appeal, and other applicable
  data-subject paths alongside the human-oversight review in T-0389.

## Security and platform review

- [ ] Confirm least-privilege future duties and tenant/site isolation for
  consent, withdrawal, retention, deletion, reconciliation, and support.
- [ ] Define safe append-only audit fields for future privileged lifecycle
  actions: authorized actor role, action category, time, tenant/site scope,
  request reference, and bounded result/reason category only.
- [ ] Confirm that biometric information, images, templates, matching details,
  liveness data, and consent details never enter generic events, ordinary
  analytics, browser-visible logs, URLs, or unbounded error output.
- [ ] Define how a future lifecycle failure is contained, escalated, and
  reconciled without automatic reprocessing or false completion claims.
- [ ] Confirm fail-closed behavior: missing consent, withdrawn consent,
  unavailable deletion/reconciliation state, or unsupported scope must prevent
  a new automatic attendance outcome.

## Evidence required before implementation is proposed

- A Product decision covering purpose, sites, alternatives, human review, and
  proposed retention/deletion behavior.
- Jurisdiction-scoped Privacy and Legal approval of the biometric basis,
  opt-in, withdrawal, privacy impact assessment, and lifecycle design.
- A Security threat-model and Platform review that covers isolation, safe audit,
  lifecycle failure handling, and deletion/reconciliation.
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
