# ADR-004: Go edge agent

- Status: Accepted
- Date: 2026-08-12
- Owners: Edge lead, CTO

## Decision

The `edge-agent` is a Go single binary. Python 3.12/Triton vision and face engines are separate supervised processes with versioned local interfaces.

## Consequences

- Go owns device identity, config convergence, RTSP lifecycle, buffering, telemetry, retries, and process supervision.
- Python owns model inference and evaluation, not device control.
- Privacy masking occurs before recording or transmission; hardware-in-loop tests gate release.
