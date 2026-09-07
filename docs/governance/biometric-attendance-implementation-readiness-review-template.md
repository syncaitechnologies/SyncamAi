# Biometric attendance implementation-readiness review template

## Status

Preparation only. This template grants no implementation approval, readiness
decision, model promotion, deployment, or production use. It does not authorize
or implement face attendance, face recognition, enrollment, matching, or
liveness.

## Purpose

T-0386 through T-0391 define the prerequisite boundaries for a future
consent-based biometric attendance capability. Use this template only after
their required reviews exist, to determine whether a future implementation
proposal may be considered. A completed template is not itself an approval to
start implementation.

The completed review must be retained in the approved product, privacy, legal,
security, and AI-governance systems. This repository may contain only a
non-sensitive reference to it. Do not add biometric or personal data, consent
records, customer footage, datasets, model artifacts, evaluation results,
credentials, live design details, or infrastructure identifiers here.

## Required evidence review

Every item below must be complete, approved by its named owners, in scope for
the proposed sites and jurisdictions, and referenced through the approved
review systems. A missing, expired, ambiguous, or conflicting item keeps the
capability blocked.

- [ ] Product approval: attendance purpose, authorized sites, eligible
  population, non-biometric alternatives, human review, correction/dispute,
  and proposed lifecycle behavior.
- [ ] Privacy and Legal approval: jurisdiction-specific biometric basis,
  voluntary opt-in, withdrawal, retention/deletion, processor/subprocessor,
  privacy impact assessment, and applicable data-subject rights.
- [ ] Security and Platform approval: threat model, least privilege,
  tenant/site isolation, safe audit fields, failure containment, and
  deletion/reconciliation expectations.
- [ ] AI and Legal approval for each proposed model: ADR-001 allowed license,
  weight provenance, model card, signature, rollback metadata, and Legal
  approval. ADR-001 remains proposed until Legal accepts it.
- [ ] Dataset evidence for each proposed future dataset: T-0362/T-0363
  provenance, license, consent, and held-out-evaluation prerequisites.
- [ ] AI, Privacy, Product, Security, Legal, and human-oversight approval of
  the completed held-out, liveness-attack, fairness, operational, and
  lifecycle evaluation design.
- [ ] Human-oversight approval of the future review, correction, dispute,
  withdrawal, escalation, and safe-audit design.

## Required review conclusions

- [ ] Record the evidence owners, approval dates, scope, expiry or re-review
  triggers, and non-sensitive references in the approved review record.
- [ ] Resolve any inconsistency between evidence sources under the repository's
  documented precedence and ADR process before a future proposal is considered.
- [ ] Confirm that the proposed future scope remains limited to approved
  jurisdictions, tenants/sites, purposes, and alternatives.
- [ ] Confirm that any absent consent, withdrawn consent, unsupported scope,
  unavailable approval, unavailable model, missing liveness, ambiguous match,
  incomplete deletion/reconciliation, or inconclusive evaluation fails closed.
- [ ] Confirm that biometric information remains outside generic event payloads,
  ordinary analytics, client-visible logs, URLs, and browser state.
- [ ] Confirm that a future attendance result cannot independently make an
  employment, payroll, disciplinary, safety, or access-control decision.

## Decision boundary

The completed review has only two permissible recommendations:

- **Remain blocked:** one or more prerequisites are missing, unapproved,
  expired, ambiguous, or out of scope.
- **Consider a future implementation proposal:** every prerequisite is approved
  and in scope. A separate, reviewable task must still define its own contract,
  data-minimization design, test plan, threat-model updates, and approval gates
  before any implementation begins.

Neither recommendation enables processing, creates a consent or attendance
record, releases a model, or authorizes deployment.

Until all required evidence exists and the appropriate owners expressly approve
a separate implementation proposal, no biometric attendance implementation may
be added. Do not add biometric or personal data, consent records, enrollment,
templates, matching, liveness inference, customer footage, datasets, model
artifacts, evaluation results, APIs, browser routes, activation, promotion,
deployment, or production configuration.
