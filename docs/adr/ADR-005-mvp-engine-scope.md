# ADR-005: twelve-engine MVP scope

- Status: Accepted
- Date: 2026-08-12
- Owners: Product, AI lead, CTO

## Decision

The MVP contains twelve engineering engines: shared detector, pose, face detection, face embedding/matching, liveness, fire classifier, smoke classifier, temporal/track/zone logic including abandoned-object behavior, and camera health as enumerated by the canonical module manifest.

The shared detector emits event-only vehicle classes (FR-103a). Full class/color/speed enrichment, LPR, ReID, theft-risk scoring, and cross-camera vehicle tracking are Phase 2.

## Consequences

- Vehicle alerts describe observed activity and require human review; they never state “theft detected.”
- Every probabilistic security or safety event sets `requires_human_review=true`.
- A module that misses its evaluation gate remains in shadow mode and is not advertised.
