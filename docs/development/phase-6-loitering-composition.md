# Phase 6 loitering composition

T-0421 closes the synthetic end-to-end proof for FR-108 by composing the
existing bounded `CameraLocalDetectionTracker`, `SampledTrackIngress`, and
`ZoneRuntime`. It does not add a second loitering engine or accept pixels. A
future approved detector can supply only normalized person or permitted
vehicle metadata into this path.

The proof starts a stable camera-local track outside a configured loitering
polygon, observes entry, and requires the full configured dwell window before
one event is emitted. The minimum remains 30 seconds. An early exit clears the
dwell start, re-entry begins a new window, and continued presence after an
event cannot emit a duplicate until the track exits and enters again. Both a
person and an allowlisted vehicle class are covered.

The canonical event preserves tenant, site, camera, and zone UUID scope. It is
retry-stable, requires human review, has `review_state=pending`, and carries an
empty evidence list in the synthetic composition. The opaque dedupe key now
uses the deterministic event UUID rather than embedding the raw camera-local
track number. No track ID, coordinate, bounding box, pixel, image, identity,
biometric, plate, embedding, ReID value, alarm, dispatch, access-control, or
autonomous-action instruction crosses the event boundary.

Tests cover the 30-second threshold, single emission, early exit, re-entry,
deterministic retry, replay, scope and timestamp ordering, malformed metadata,
atomic association capacity failure, and configuration rejection outside the
30--600 second bound or approved subject vocabulary. All inputs are synthetic
metadata. No detector, model weight, dataset, footage, benchmark, evidence
object, activation, promotion, or deployment is included.

This is composition evidence, not production vision evidence. The executable
edge agent still uses its null sink because a controlled privacy-release
loader and real allowlisted hardware executor are unavailable. Production
loitering still requires a licensed and approved detector/tracker, physical
signed privacy-mask HIL, site threshold validation, held-out temporal and
identity-stability evaluation, human oversight, and an mTLS-capable endpoint.
