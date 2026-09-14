# Phase 8 PPE review confirmation boundary

T-0424 adds a synthetic-safe temporal boundary for future FR-106 PPE metadata.
It consumes only already-associated camera-local tracks. It does not accept
pixels, images, crops, boxes, names, employee identifiers, faces, biometrics,
plates, embeddings or ReID data, and it performs no detector or person/PPE
attachment work.

Each boundary instance fixes tenant, site, camera and zone UUID scope plus a
non-empty required-item matrix. The matrix is a subset of exactly six bounded
items: helmet, vest, mask, gloves, glasses and boots. An ordered frame carries
at most 64 unique local track numbers and up to six unique item observations per
track. Each item state is exactly `present`, `absent` or `unknown`, with bounded
confidence and opaque model-version metadata.

Unknown or missing required-item observations fail non-confirming, as does a
frame where all required items are reported present. A potential absence emits
one review event only after three consecutive observations of the same local
track, missing-item set and model version, with no gap greater than one second.
Changing the set or provenance and exceeding the time gap restart confirmation.
The event confidence is the minimum reported absence confidence across the
confirmed streak.

State is capped at 256 tracks and emitted cooldown at 30 frames. Input is fully
validated before state or counters change. Tracks are sorted by their local
number before processing for deterministic multi-event order. Unconfirmed gaps
expire immediately; bounded retained emitted state suppresses short duplicate
bursts and then permits a newly confirmed recurrence.

The output is the existing `ppe_review` contract with an opaque UUIDv5 event ID,
an opaque `ppe_review:<event_id>` dedupe key, empty evidence references and
mandatory pending human review. Track numbers and PPE item/state details affect
only the UUID preimage and are not emitted. The payload makes no PPE violation,
compliance, worker-safety or fitness-for-work conclusion and carries no alarm,
dispatch, notification or access-control instruction.

Tests use synthetic metadata only and cover the six-item matrix boundary,
unknown/incomplete/fully-present input, the three-frame threshold, conservative
confidence, missing-set/model/time resets, stable ordering and retry, replay,
duplicates, malformed metadata, atomic state-capacity rejection, cooldown
expiry and reconfirmation.

This is not PPE detection and does not establish per-class accuracy or safe
deployment. Production remains blocked on controlled privacy-mask release
loading, allowlisted hardware execution, physical signed-mask HIL, verified
mTLS delivery, approved licensed person/PPE models, person-to-item attachment
and occlusion evaluation, garment and demographic held-out slices, per-site
matrix/threshold approval, model promotion and human oversight.
