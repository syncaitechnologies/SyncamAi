# Phase 8: abandoned-object temporal logic

T-0308 implements the deterministic, camera-local portion of the MVP abandoned-object engine. It follows AI-ARCHITECTURE §3.21: a static object must be associated with a nearby camera-local owner track, remain stationary for T1, and remain without that owner until T2 before it emits an `abandoned_object_review` event.

## Contract and privacy boundary

`AbandonedObjectEngine` accepts normalized object centroids in `[0, 1]`, local object and person tracker IDs, and bounded object classes (`bag`, `box`, or `suitcase`). Tracker IDs and centroids are working state only. The emitted canonical event contains no owner ID, person ID, coordinates, plate, biometric, ReID, identity, risk score, or theft conclusion.

Events use the existing `abandoned_object_review` type and always set `requires_human_review=true` and `review_state=pending`. Event ID and dedupe key derive deterministically from the tenant, camera-local object track, and the start of its static interval, so retries cannot create another alert.

## Baseline evaluation card

| Item | Baseline gate | Status |
|---|---|---|
| T1 static dwell | Configurable only from 30 to 60 seconds | Enforced |
| T2 alert dwell | Configurable and never shorter than T1; development default 60 seconds | Enforced |
| Owner separation | Same camera-local owner track absent at T2 | Enforced |
| Static-object classes | bag, box, suitcase | Enforced |
| Synthetic regression cases | owner leaves, owner remains, object moves | Passing in `test_abandoned_object.py` |
| Production evaluation | Detection rate at least 0.85 and fewer than one false alarm per camera/day on held-out ABODA-class scenes | Release gate, not claimed by this slice |

The synthetic evaluator verifies temporal-state behavior only. It is not a substitute for licensed, held-out scene evaluation, per-zone calibration, camera shake handling, owner-association error analysis, or hardware benchmarks. Parked equipment, allowed objects, occlusions, tracker ID switches, and recurring dock activity remain explicit pre-promotion test categories.

## Release controls

Abandoned-object alerts are medium-risk compliance/security signals. They remain human-review-only; they cannot trigger autonomous action. A production rollout requires the AI promotion and human-oversight gates, a documented per-zone threshold and allowlist, audited operator outcomes, and the evaluation card's held-out accuracy and false-alert checks.
