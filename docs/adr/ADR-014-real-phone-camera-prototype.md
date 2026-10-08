# ADR-014: real phone-camera prototype, separate from scripted walkthroughs

- Status: Engineering decision for local camera capture; inference topology proposed
- Date: 2026-09-29
- Related: FR-201, FR-101, FR-102, T-0433, ADR-001, ADR-013

## Context

The owner clarified that the client presentation must analyse actual camera
images: iPhone Chrome, person boxes, firearm/knife candidates, and names for
previously enrolled consenting people. The target is three weeks, not the full
registry. Canada is the intended jurisdiction; province, test participants,
exact iPhone/iOS version and final hosting arrangement are not yet confirmed.
ADR-013's synthetic walkthrough does not meet this revised presentation goal.
It remains labelled and is not evidence of inference readiness.

Canonical architecture describes Go edge agents with Python 3.12 AI processes
and approved model releases. A single-phone controlled prototype is not the
8-32-stream production tier. Its eventual browser/laptop/hosted topology must
be evaluated and recorded without silently replacing that architecture.

## Decision for T-0433

Add a separate **Phone camera** workspace using the browser's real
`getUserMedia` video stream. Access requires an explicit scene-authorization
acknowledgement and Start action. Prefer the rear camera, permit an explicit
front-camera selection, request no microphone, and render muted inline video.
No auto-start, recording, still extraction, upload, persistence, model loading,
identity lookup, attendance, alert creation or inference is included.

Stop and detach every track when the user stops, changes camera selection,
revokes the local acknowledgement, navigates away, backgrounds the page,
leaves the document, or when a camera track ends. Invalidate pending requests
so late permission grants or playback completion cannot restart capture.
Returning to the page requires another Start action. Error text is bounded;
device identifiers and raw browser error details are not logged.

The local input is not a registered tenant/site camera, authenticated edge
device, or audit-backed live CCTV session. Existing tenant/site APIs, RLS,
authentication and wire identifiers are unchanged. The direct camera query
entry works only in demo data mode; it cannot bypass live authentication.
There are no simulated detections in the camera workspace. Model capabilities
are explicitly unavailable until actual approved runtimes are connected.

## Proposed inference direction, not activation authority

Start by benchmarking person inference on an approved local process or a
small browser model. Evaluate browser WASM compatibility before assuming
iPhone WebGPU support; the Transformers.js skill highlights version pinning,
bounded loading, pipeline reuse and disposal. A laptop-assisted version and a
phone-accessible hosted version remain targets, not deployed capabilities.
Any image transfer needs an authenticated, authorized, bounded HTTPS contract
and a privacy/security review before implementation.

ADR-001 remains Proposed and blocks external-model promotion. No model,
license exception, paid host, artifact download or biometric approval is
authorized by this ADR. The Phase 5 prerequisite boundary applies before
face-recognition implementation, including enrollment, matching and liveness.
Pointing a camera at a person is not enrollment or biometric consent.

The source documents' MIT references for InsightFace describe library code,
not blanket permission for its pretrained weights. The publisher's current
[model-license policy](https://github.com/deepinsight/insightface/blob/master/python-package/docs/model_zoo.md)
restricts supplied weights to non-commercial research. Apply D6 and ADR-001
to exact artifacts; do not auto-download them for a commercial client demo.

## Consequences and verification

This slice enables an actual local camera preview but completes none of the
three detection capabilities. Unit tests exercise permission/error paths,
cleanup, late-request races, visibility and direct-entry isolation without
using footage or a physical camera. Browser and physical iPhone checks are
distinct: desktop viewport emulation does not establish iPhone compatibility.
See the [three-week delivery plan](../development/real-camera-prototype-plan.md)
for milestones, approval blockers and acceptance checks.
