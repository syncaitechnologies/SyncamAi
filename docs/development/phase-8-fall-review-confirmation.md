# Phase 8 fall review confirmation boundary

T-0425 adds a synthetic-safe temporal boundary for future FR-111 posture and
motion metadata. It consumes only already-associated camera-local tracks. It
does not accept pixels, images, crops, boxes, names, employee identifiers,
faces, biometrics, plates, embeddings or ReID data, and it performs no pose or
fall inference.

## Contract

Each boundary instance is fixed to one canonical tenant, site, camera and zone.
Frames must have strictly increasing positive sequence numbers and
timezone-aware timestamps, contain no duplicate track IDs, and carry no more
than 64 tracks. A track record contains only a bounded camera-local integer,
one posture state, one motion state, normalized confidence and an opaque model
version.

The allowed posture vocabulary is `upright`, `transitioning`, `sitting`,
`lying` and `unknown`. The allowed motion vocabulary is `stable`, `downward`,
`ambiguous` and `unknown`. These values are metadata contract states, not
independently verified facts or safety conclusions.

## Conservative confirmation

Confirmation requires one continuous, provenance-consistent sequence:

1. `upright` with `stable` motion arms the track.
2. `transitioning` with `downward` motion starts and continues the transition.
3. `lying` with `stable` motion establishes the final candidate stage.
4. A later continuous stable-lying observation confirms only when at least 1.5
   seconds have elapsed since the first downward-transition observation.

No adjacent observations may be more than one second apart. Confidence is the
minimum of every accepted observation from the upright baseline through the
confirming observation; no per-site threshold is invented by this boundary.
Site threshold selection remains a separate approval and evaluation gate.

Sitting, unknown posture, unknown or ambiguous motion, stable motion during the
transition stage, downward motion in the final stage, lying without the full
ordered transition, a missing observation, a tracking gap, provenance change
or reversed stage order does not confirm or resets the candidate. A stable
upright observation may arm a fresh candidate after recovery.

## Bounded and review-only behavior

The boundary retains at most 256 active track states and at most 30 missing
frames of already-emitted cooldown state. Capacity failure, replay, malformed
metadata, scope mismatch, duplicate track IDs and ordering failure are atomic.
Inputs are sorted by camera-local track ID so output ordering and opaque UUIDv5
event IDs are deterministic. Continuous stable-lying observations after an
emission are suppressed; a new event requires a fresh upright/downward/lying
transition.

Every event uses the existing `fall_review` contract with an empty evidence
list, `requires_human_review: true` and `review_state: pending`. The event omits
track IDs, posture and motion detail, identity, biometric data, images, medical
or safety conclusions, and action instructions.

## Deliberate exclusions and remaining gates

This slice adds no model, weights, dataset, footage, evidence object, live
transport, credential, notification, emergency contact action, alarm, dispatch,
access-control action, activation, deployment or production-readiness claim.
Production use remains blocked by the controlled privacy-release loader, real
allowlisted hardware executor, physical signed-mask HIL evidence, verified mTLS
endpoint and sender wiring, licensed model approval, sit/lie and occlusion
held-out evaluation, per-site calibration, model promotion and explicit human
approval.
