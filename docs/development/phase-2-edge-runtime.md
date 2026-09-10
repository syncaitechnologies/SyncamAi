# Phase 2 executable edge runtime

T-0414 replaces the print-only `edge-agent` command with a cancellable runtime
that composes the existing T-0325--T-0328 transport libraries. It starts the
certificate-authenticated heartbeat and configuration loops, reports durable
spool depth, and supervises one bounded FFmpeg process for each configured RTSP
source. A termination signal cancels every loop before the process exits.

This is an edge control and ingest-lifecycle slice. FFmpeg still writes decoded
frames to its null sink. The runtime does not store footage, load model weights,
run person or vehicle inference, create detections, or activate biometric
features. Hardware-in-loop camera evidence and the later frame-to-inference
handoff remain separate release gates.

## Trusted startup boundary

The edge service account receives non-secret identity values through the
environment and secret material through mounted files. See `.env.example` for
the complete variable names. Startup fails closed when any required path is
missing, the certificate/key pair is invalid, the server CA cannot be parsed,
or the source inventory is empty or malformed. TLS is restricted to TLS 1.3 by
the existing mTLS transport.

The RTSP source file is limited to 128 KiB and 64 sources. Keep it outside Git,
restrict it to the edge service account, and never include it in support logs.
Its shape is:

```json
{
  "sources": [
    {
      "id": "camera-loading-bay",
      "url": "rtsps://camera.internal.example/live",
      "transport": "tcp",
      "codec": "h264",
      "decode_preference": "auto",
      "available_decoders": []
    }
  ]
}
```

`available_decoders` must come from the trusted local FFmpeg capability probe,
not browser or tenant input. Forced hardware decoding fails when the named
backend is unavailable; automatic selection falls back to the allowlisted
software decoder. Runtime JSON logs contain source IDs and lifecycle states,
but never RTSP URLs, subprocess arguments, certificate contents, keys, or raw
transport errors.

## Deployment limitation

The deployed Render Free service terminates TLS before the Go process and does
not provide a verified client-certificate chain to the current application
verifier. Consequently, its edge routes correctly remain fail-closed. Do not
point a real edge device at that public endpoint or weaken certificate checks.
An mTLS-capable gateway or equivalent approved device-authentication design is
required before remote edge enrollment and hardware validation. This task does
not authorize paid infrastructure.
