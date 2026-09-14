# Phase 8 fight-review temporal confirmation

T-0426 adds a synthetic, metadata-only boundary for future FR-112 motion
output. It does not add a fight model, pose source, model weight, image,
footage, evidence object, identity, biometric processing or autonomous action.

## Accepted boundary

`FightReviewConfirmation` is constructed for exactly one tenant, site, camera
and zone. It accepts strictly ordered, timezone-aware metadata frames containing
at most 64 already-associated camera-local tracks. Every track carries only a
bounded local track ID, bounded local cluster ID, one explicit motion state,
normalized confidence and opaque model-version provenance.

A cluster becomes a review candidate only when it contains at least two tracks,
every member is explicitly `aggressive`, the member set remains unchanged and
all members use the same model version. The complete cluster must persist for
at least one second, with no observation gap greater than one second. The
minimum confidence across the qualifying window becomes the event confidence.

`ordinary`, `hugging`, `jostling` and `unknown` motion never confirm. A missing
track or cluster, participant change, model-provenance change, mixed provenance,
non-qualifying motion or excessive observation gap resets the candidate. This
conservative boundary does not infer intent, violence, injury or danger.

## Output and safety properties

The only output is the existing generic `fight_review` event. It contains an
opaque retry-stable UUID, tenant/site/camera/zone scope, event type, model
version, minimum confidence, no evidence references and mandatory
`requires_human_review: true` / `review_state: pending` fields.

Track IDs, cluster IDs and motion labels remain local and are never emitted.
The component does not notify, alarm, dispatch, control access, make a safety
conclusion or authorize any automated response. A successful confirmation is
retained for a bounded cooldown so identical continuing metadata cannot emit a
duplicate event. A reset is required before another event can be considered.

Frames, state and emitted cooldowns are bounded. Scope changes, replayed or
non-increasing sequences, non-increasing timestamps, duplicate track IDs,
unsupported states, malformed confidence or provenance and capacity exhaustion
fail atomically without advancing metrics or state.

## Verification and remaining gates

Synthetic unit tests cover the exact one-second boundary, minimum two-track
requirement, ordinary movement, hugs, jostling, unknown motion, missing tracks,
gaps, provenance and participant changes, deterministic ordering and retry,
duplicate suppression, reset/reconfirmation, replay, malformed input and
capacity backpressure.

Production use remains blocked until licensed pose/motion sources are approved
and promoted; lawfully approved held-out datasets cover play-fighting, hugs,
crowd movement, jostling, occlusion, lighting, camera angle and per-site
behavior; thresholds and human-review procedures are signed off; and the
hardware, privacy-mask, mTLS delivery and operational gates are verified.
