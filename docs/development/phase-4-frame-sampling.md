# Phase 4 bounded analytics frame sampling

T-0416 implements the edge portion of historical T-0160 after the approved
pixel-mask boundary. `AnalyticsFrameSampler` is camera-local and accepts only a
complete RGB24 frame with a positive sequence number and timestamp-ordered
observation time. It implements `MaskedFrameConsumer`, so the approved
`PreAnalyticsPrivacyMask` can feed it directly without an unmasked bypass.

The configured analytics rate must be between 5 and 10 FPS. A frame inside the
sampling interval is suppressed as a whole. Frames admitted by the time gate
are hashed with their dimensions; an identical consecutive sampled frame is
also suppressed. Only that SHA-256 digest, sequence/timestamp state, and safe
counters remain after synchronous processing. Pixels are not copied into
metrics, logs, storage, or a retry queue.

Downstream processing is synchronous per camera. A full inference queue or
other consumer error returns to the producer as backpressure and increments a
payload-free failure counter. The sampler does not retry or persist that live
frame, and never relaxes its rate cap.

Unit tests cover 10 FPS time gating, duplicate suppression, strict camera and
ordering checks, invalid buffer rejection, downstream failure accounting, and
an integration chain proving that an all-frame mask blackens the pixels before
the sampler's analytics consumer receives them.

This code does not yet receive FFmpeg output and its downstream test consumer
is not a detector. No model, weight, customer frame, inference result, event,
evaluation evidence, or release authorization is added. The next slice must
connect FFmpeg's bounded decoded output to this exact mask-then-sampler chain;
model execution remains behind the separate provenance, licensing, evaluation,
signature, human-review, and promotion gates.
