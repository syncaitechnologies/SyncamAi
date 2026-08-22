# Phase 3 camera-local track sampling foundation

`SampledTrackIngress` is a bounded, in-process bridge from a camera-local
tracker to the active `ZoneRuntime`. It accepts a timestamped `TrackFrame` with
one tenant, site, camera, and device, and at most 256 `TrackObservation`
values. It validates that every track belongs to the frame tenant, site,
camera, and timestamp, and rejects duplicate track IDs.

The sampler accepts only a configured 5--10 FPS target and explicit positive,
bounded limits for sampled frames per tenant/device/second and sampled tracks
per tenant/second. A whole batch that arrives before the next sample interval
is deterministically suppressed; no individual tracks from that batch are
evaluated or counted against those quotas. Sampled quota accounting is ordered
per tenant/device and per tenant. The `pressure` result exposes only remaining
unit counts and whether the upstream edge loop must apply backpressure. The
safe counters are accepted, sampled, suppressed, quota-rejected, and
emitted-event totals. They contain no tracker IDs, coordinates, frames, pixels,
credentials, identities, embeddings, or plate data.

This is not detector inference, raw-frame ingestion, a cloud quota service,
network backpressure, or edge-to-platform event delivery. It is the local
quota and pressure-control foundation for the broader T-0091/T-0160 work. The
sampler only forwards admitted tracker metadata to a locally active rule
runtime; that runtime emits no events until it has already activated a valid
delivered configuration.

Privacy-mask zones remain excluded. They require the dedicated Super Admin,
dual-approval, immutable-audit, edge-side masking verification, and
hardware-in-loop slice.
