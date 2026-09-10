# Phase 3 pre-analytics pixel mask

T-0415 adds the first concrete pixel-processing implementation behind the
privacy-mask release boundaries. A tightly packed RGB24 frame can reach its
local analytics consumer only through `PreAnalyticsPrivacyMask`, which applies
the configured polygon as opaque black pixels before invoking that consumer.
The frame buffer is transferred synchronously and is never restored after a
downstream failure.

Construction requires both the validated two-approver mask candidate and the
matching `HardwarePrivacyMaskActivation` produced by the existing signed HIL
and controlled-release path. The candidate hash binds the activation to the
camera and geometry. A different candidate, camera, release, malformed frame,
or non-strict decode-mask-encode pipeline fails closed.

The renderer accepts only complete, tightly packed RGB24 buffers with bounded
dimensions. It uses pixel-center coverage for normalized GeoJSON polygons,
masks every polygon boundary conservatively, and preserves the interior of
declared holes. It caches the boolean geometry raster for the active frame
dimensions; it does not cache, copy, log, hash, persist, or transmit pixels.

The tests use small synthetic byte arrays and a synthetic activation that has
the same structural shape as the output of the HIL-gated adapter. They prove
the deterministic software behavior only. They are not physical-camera HIL
evidence, approval of a mask release, or proof of GPU/encoder behavior.

This slice is intentionally not connected to the executable FFmpeg process
yet. Runtime wiring must preserve this ordering:

```text
RTSP decode -> approved pixel mask -> bounded sampling -> local inference
```

No recording, evidence retention, inference, model weights, biometric data,
or cloud frame transport is introduced. Connecting FFmpeg output is the next
implementation slice and remains blocked unless the mask boundary cannot be
bypassed.
