# Phase 2 device heartbeat transport

The merged T-0324 control-plane slice accepts heartbeat telemetry only from an active registry device whose verified client certificate identifies the same device UUID as the route. T-0325 adds the Go edge transport in `edge/internal/agent`.

The edge process supplies an `https` endpoint and a TLS client configured with the issued device certificate. `NewMTLSHTTPClient` enforces TLS 1.3 and keeps certificate material in memory. The transport does not send OIDC tokens or tenant headers; the device certificate is the authentication boundary.

`HeartbeatClient.Send` validates the server-side bounds before sending. An empty heartbeat ID receives a new UUIDv4; callers that retry a request can supply the original ID so the server can distinguish an exact replay from a conflicting payload. `HeartbeatClient.Run` sends immediately and then on an interval no longer than the canonical 30 seconds. Failed attempts are reported to the caller and do not silently advance liveness.

T-0329 begins T-0155 by extending the certificate-authenticated heartbeat with optional, bounded device health. A platform adapter supplies CPU utilization, GPU utilization, device temperature, and inference latency to the Go health monitor. The monitor samples immediately and at an interval no slower than 30 seconds, rejects NaN, infinity, and out-of-range readings, and derives normal, warning, or critical thermal state at the canonical 80°C and 90°C thresholds. The cloud accepts health as a backward-compatible optional heartbeat object, includes it in idempotency hashing, persists it through the guarded tenant-bound heartbeat function, and exposes the most recently accepted sample in fleet status. Hardware-specific Jetson/NVIDIA probe adapters and production thermal certification remain separate hardware-in-loop work.

AWS IoT Core, tenant CA issuance, certificate rotation, and production device provisioning remain deferred until the AWS account and CA integration are available. No certificate, private key, footage, or customer data belongs in the repository or test fixtures.
