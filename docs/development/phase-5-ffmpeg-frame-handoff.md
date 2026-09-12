# Phase 5 bounded FFmpeg frame handoff

T-0417 adds an opt-in rawvideo output path to the existing supervised RTSP
ingest. The path cannot be configured with a generic callback: it accepts only
a concrete `PreAnalyticsPrivacyMask` whose downstream consumer is the
camera-matched `AnalyticsFrameSampler`. Construction therefore fails before
FFmpeg starts if the chain could bypass either the approved mask or the
bounded sampler.

Framed mode requires a UUIDv4 camera identifier and explicit positive width
and height. The RGB24 payload is capped at 64 MiB and FFmpeg is invoked with a
fixed output size, `rgb24` pixel format, rawvideo muxer, and stdout pipe. The
process adapter allocates exactly one complete buffer at a time and transfers
ownership synchronously. Partial frames, consumer rejection, unsupported
runners, excessive dimensions, and incomplete configuration fail closed.

Sequence numbers remain increasing across bounded FFmpeg restarts. Observation
times are UTC and are advanced by one nanosecond if the host clock repeats or
moves backward, preserving the sampler's strict ordering boundary. A
downstream failure stops the current child process and enters the existing
credential-safe bounded retry path. Pixels, RTSP arguments, URLs, and child
errors are not included in lifecycle status or retained in a queue.

The integration test uses synthetic pixels and signed HIL-shaped fixtures to
prove this library path is exactly:

```text
FFmpeg RGB24 stdout -> approved privacy mask -> 5-10 FPS sampler -> consumer
```

That synthetic test is not physical HIL evidence or permission to process a
camera. The executable edge-agent keeps the null sink until a real controlled
privacy-mask release, allowlisted hardware executor, and trusted local release
loader exist. No model, weight, dataset, inference, customer footage,
evaluation result, activation, or promotion is added.
