# Phase 2 RTSP ingest

T-0326 begins T-0152 with a supervised FFmpeg RTSP pull in the Go edge agent. The slice establishes source validation, TCP-by-default transport, bounded connection timeouts, lifecycle reporting, and bounded exponential restart backoff. FFmpeg decodes the primary video stream into a null sink for now; codec-specific hardware decode, frame delivery, store-and-forward, and vendor hardware-in-loop certification remain separate roadmap tasks.

Camera URLs may contain credentials supplied by the runtime secret boundary. They are passed only to the child process and never returned through status callbacks or subprocess errors. Applications must log `RTSPStatus.SourceID`, state, attempt, and the sanitized error only. They must not log the source URL, FFmpeg argument list, environment, or camera credentials.

The wrapper accepts `rtsp` and `rtsps` sources and `tcp` or `udp` transport. TCP is the default because it is deterministic across common site networks; RTSP-over-TLS is preferred when the camera supports it. Plain RTSP belongs only on the segmented camera VLAN described in the security architecture.

Run the focused tests with:

```text
go test ./edge/internal/agent
```
