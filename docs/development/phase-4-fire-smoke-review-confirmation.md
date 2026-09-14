# Phase 4 fire/smoke review confirmation boundary

T-0422 adds a synthetic-safe temporal boundary for future FR-113 detector
metadata. It accepts only ordered tenant/site/camera/zone-local batches of at
most 64 normalized `fire` or `smoke` candidates. Each candidate has a bounded
confidence, normalized box, and opaque model-version identifier. The boundary
has no input for pixels, crops, sensor state, customer footage, or free-form
classes.

Fire requires three class-, model-version-, and spatially consistent frames.
Smoke requires five such frames and a configurable positive upward center
movement on every unconfirmed transition, reflecting the architecture's
longer rising-pattern gate. Consecutive observations may be at most one second
apart. Event confidence is the minimum across the accepted streak. Class,
provenance, spatial, timing, or smoke-direction changes restart confirmation.

The runtime holds at most 256 candidate states and 64 candidates per frame.
Scope, sequence, timestamp, metadata, and capacity failures are rejected before
state or safe counters change. Emitted state is retained for a bounded one
through 30 frames to suppress immediate duplicates. Metrics expose counts only.

Outputs use the existing `fire_review` and `smoke_review` event vocabulary,
opaque retry-stable IDs, empty evidence references, and mandatory pending
human review. Candidate IDs, boxes, coordinates, pixels, images, identity,
biometrics, plates, embeddings, alarm commands, evacuation instructions,
dispatch, access control, and autonomous action are absent.

Tests use synthetic metadata only and cover both confirmation lengths, minimum
streak confidence, rising-smoke behavior, duplicate suppression, timing and
provenance resets, deterministic retry, replay, malformed input, scope,
configuration bounds, and atomic capacity failure. No detector, model weight,
dataset, customer footage, evidence object, benchmark, activation, promotion,
sensor integration, alarm integration, or deployment is added.

This boundary is not production fire detection and does not replace a
certified fire panel or other required life-safety system. Production use
remains gated on an approved licensed detector/classifier, fire-versus-welding
and smoke-versus-steam held-out evaluation, physical privacy-mask HIL,
per-site threshold review, hardware evidence, human oversight, and controlled
promotion.
