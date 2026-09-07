# Phase 5 consent-first attendance and liveness boundary

T-0386 begins the Phase 5 planning work for FR-102, consent-based face
attendance. It defines a prerequisite boundary only; it does not implement an
attendance, face-recognition, or liveness feature.

## Scope and non-goals

Future attendance may use the planned face-detection, face-recognition, and
face-liveness capabilities in the canonical model registry. It is limited to a
defined site, a stated attendance purpose, and people who have completed a
documented opt-in process. A liveness result is a required future anti-spoofing
control, not an automatic authorization, payroll, disciplinary, safety, or
employment decision.

This task creates no enrollment flow, consent record, biometric template,
matching threshold, liveness inference, attendance record, model artifact,
dataset, customer footage, evaluation result, API, browser route, activation,
release, promotion, or production configuration.

## Required approvals before implementation

All of the following evidence must be recorded and approved before an
implementation task can begin:

1. Product defines the attendance purpose, authorized sites, permitted users,
   alternatives for people who decline, retention proposal, and a human review
   path for every disputed attendance result.
2. Privacy and Legal approve the jurisdiction-specific biometric basis,
   informed opt-in wording, withdrawal process, retention/deletion schedule,
   processor/subprocessor terms, and the required privacy impact assessment.
   Consent must be voluntary, specific, recorded separately from general terms,
   and withdrawable without silently retaining future-use permission.
3. Security approves the threat model for enrollment fraud, template theft,
   replay/presentation attacks, cross-tenant disclosure, abusive operator
   access, retention/deletion failure, and audit-log exposure. The design must
   use least privilege, tenant/site isolation, append-only audit events, and a
   revocation/deletion reconciliation path.
4. AI and Legal complete the ADR-001 release requirements for each proposed
   face-detection, recognition, and liveness model: allowed license, weight
   provenance, model card, signature, rollback metadata, and Legal approval.
   Every proposed dataset also requires the planned provenance fields and
   consent constraints from T-0362/T-0363.
5. AI, Privacy, and Product approve pre-declared held-out evaluation and
   fairness criteria, including false-match, false-non-match, liveness attack,
   demographic, site/lighting, and withdrawal/deletion scenarios. Passing an
   evaluation does not itself approve production promotion.

## Future implementation constraints

- Enrollment must be an explicit, audited, server-authorized operation; a
  browser must not create or receive a provider or model secret.
- A missing consent, withdrawn consent, expired approval, unsupported site,
  absent liveness result, unavailable model, or ambiguous match must fail
  closed: no attendance record is created automatically.
- Biometric information must never be placed in alert text, ordinary analytics,
  client-visible logs, URLs, or the existing generic event payload. A future
  dedicated contract and retention model need separate review.
- Attendance outcomes are reviewable operational records. A human can correct
  or dispute them, and the correction must retain a bounded, audited reason.
- No identity decision may be inferred from a detector confidence alone. The
  evaluation and release gates in ADR-001, T-0356, and T-0362 remain mandatory.

## Relationship to other Phase 5 roadmap topics

PPE, fall, and fight detection are separate non-biometric capabilities with
their own planned model and human-oversight gates. Loitering is already
camera-local dwell logic under Phase 3 and is not a face-attendance capability.
This boundary does not make any of those capabilities live or change their
release status.

## Next planning slice: T-0387

T-0387 supplies a non-authorizing
[biometric attendance approval-evidence checklist](../governance/biometric-attendance-approval-evidence-checklist.md).
It records the review inputs that must exist before a future implementation
task can be proposed, without recording decisions, consent, biometric data, or
any model, dataset, evaluation, deployment, or activation evidence.

## Next planning slice: T-0388

T-0388 adds a non-authorizing
[biometric attendance threat-model review template](../governance/biometric-attendance-threat-model-review-template.md).
It structures the required future Security review but records no live design,
threat-model decision, risk acceptance, security finding, or implementation
evidence.

## Next planning slice: T-0389

T-0389 adds a non-authorizing
[biometric attendance human-oversight review template](../governance/biometric-attendance-human-oversight-review-template.md).
It structures future human review, correction, dispute, withdrawal, fairness,
and safe-audit decisions without creating a review queue, attendance outcome,
operational procedure, or implementation evidence.
