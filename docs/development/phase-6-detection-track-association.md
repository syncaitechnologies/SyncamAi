# Phase 6 bounded detection-to-track association

T-0418 adds a deterministic, camera-local association layer between a future
approved detector and the existing sampled track/zone runtime. It accepts
metadata only: tenant, site, camera and device UUIDs; a positive ordered frame
sequence and UTC-capable timestamp; and at most 256 normalized bounding boxes.
It has no input for frames, crops, identities, faces, plates, embeddings,
cross-camera identifiers, or customer evidence.

Only the seven existing local rule classes are accepted: `person`, `bicycle`,
`bus`, `car`, `motorcycle`, `truck`, and `van`. Each result requires a finite
normalized confidence and a bounded opaque model-version identifier. An
identity-like or unsupported class fails before tracker state changes.

`CameraLocalDetectionTracker` performs deterministic same-class greedy IoU
association. Canonically sorted detections, overlap score, prior track ID, and
detection order provide stable tie-breaking. Track IDs increase locally and
are never reused. Configuration bounds overlap threshold, missed frames to at
most 30, and active state to at most 2,048 tracks (512 by default). Scope,
sequence, timestamp, capacity, and input failures are atomic. Safe metrics
contain counts only.

The output is the existing metadata-only `TrackFrame`, so it can enter
`SampledTrackIngress` and `ZoneRuntime` without another translation. A
synthetic integration test moves one stable person box across an intrusion
boundary and proves the canonical event omits track IDs and coordinates.

This simple association foundation is not ByteTrack, a promoted production
tracker, detector inference, or performance evidence. Occlusion, crowded
crossings, camera motion, detector drift, and hardware throughput still need
licensed held-out evaluation and human promotion approval. No model, weight,
dataset, pixel, customer footage, evaluation result, release, activation, or
promotion is added. Weapon and other specialist classes remain separate gated
capabilities and do not enter the zone tracker through this slice.
