# Phase 3 camera-local track sampling foundation

`SampledTrackIngress` is a bounded, in-process bridge from a camera-local
tracker to the active `ZoneRuntime`. It accepts a timestamped `TrackFrame` with
at most 256 `TrackObservation` values, validates that every value belongs to
the same tenant, site, camera, and timestamp, and rejects duplicate track IDs.

The sampler accepts only a configured 5--10 FPS target. A whole batch that
arrives before the next sample interval is deterministically suppressed; no
individual tracks from that batch are evaluated. Inputs must be ordered per
camera, including suppressed batches. The safe counters are accepted, sampled,
suppressed, and emitted-event totals. They contain no tracker IDs, coordinates,
frames, pixels, credentials, identities, embeddings, or plate data.

This is not detector inference, raw-frame ingestion, quota enforcement,
backpressure, or edge-to-platform event delivery. Those parts of the broader
T-0091/T-0160 roadmap remain separately gated. The sampler only forwards
sampled tracker metadata to a locally active rule runtime; that runtime emits
no events until it has already activated a valid delivered configuration.

Privacy-mask zones remain excluded. They require the dedicated Super Admin,
dual-approval, immutable-audit, edge-side masking verification, and
hardware-in-loop slice.
