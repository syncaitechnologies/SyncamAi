# ADR-015: development-only local person-inference bridge

## Status

Engineering decision, 2026-10-07. Related task: T-0434.

## Decision

For an authorized development demonstration only, the phone-camera workspace
may send a bounded, transient RGBA frame to one explicitly configured HTTPS
endpoint on a developer-controlled machine. The endpoint accepts only
`POST /local-dev/v1/person-detections?width={width}&height={height}`, requires
one configured browser Origin, enforces a maximum 640 × 480 frame, and returns
only bounded person boxes and confidence values.

The local Python process requires caller-provided TLS material and the
checksum-verified Intel `person-detection-0200` model pair from ADR-001. It
does not start by default, stores nothing, and suppresses request logging. The
browser sends no frame until a user has separately started the camera and
enabled person detection. Stopping, hiding, navigating away from, or disposing
the camera workspace cancels in-flight inference and clears boxes.

This endpoint is a local development surface. It does not modify SentinelVision
domains, routes, or wire-level headers, and it is not an API for production,
tenant integration, edge devices, alerts, evidence, or customer footage.

## Consequences

- A real person may be detected on an authorized developer test scene from a
  phone browser when the app is served over HTTPS and the phone trusts the
  developer endpoint's TLS certificate.
- No certificate, private key, model artifact, image, footage, identity,
  biometric template, event, alert, recording, or customer data enters Git.
- Weapon/firearm/knife results are not requested or emitted. They require a
  separately approved model, negative-tool evaluation, and human-review path.
- Face recognition, enrollment, matching, liveness, attendance, and any name
  result remain blocked by the Phase 5 consent and biometric approval boundary.
- Promotion, deployment, tenant authorization, durable delivery, evaluation,
  model-release evidence, and legal approval remain separate future work.
