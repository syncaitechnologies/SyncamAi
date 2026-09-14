# Phase 2 pending-review event delivery composition

T-0423 composes the existing canonical, non-biometric review-event payload with
the durable edge spool. The boundary is metadata-only and fixes tenant and site
scope at construction. Each queued event must carry valid event, tenant, site,
camera and zone UUIDs; an allowed existing event type; bounded model metadata,
confidence and evidence references; and mandatory `requires_human_review: true`
with `review_state: pending`.

JSON decoding is strict and bounded to 64 KiB. Missing and unknown properties
fail closed, so alarm, emergency dispatch, access-control, safety-conclusion or
other autonomous-action instructions cannot enter the queue. The generic
biometric `attendance_review` vocabulary remains rejected. Only
`vehicle_activity` may carry the canonical `detected` behavior and bounded
vehicle class; other event types omit those fields.

Every dedupe key must be the opaque, deterministic
`<event_type>:<event_id>` form. The local event ID can still be derived from
camera-local state, but track identifiers, boxes and coordinates do not cross
this boundary. The vehicle-activity and abandoned-object producers were aligned
to this form, and non-vehicle zone events were aligned with the backend's
vehicle-only optional-field rule.

The spool's retained enqueue operation preflights its complete record under the
queue lock. If the quota is full it returns capacity backpressure without a file
write, counter change or eviction. Identical event replay is idempotent; reusing
an event ID for changed canonical content returns a conflict. Metadata is
ordered deterministically and survives restart.

`ReviewEventSender` is injected. It must return success only after durable,
idempotent upstream acceptance. A send failure leaves the item untouched. On
success the boundary acknowledges and removes it locally; if acknowledgement
itself ever fails, the retained item may be resent and the upstream event ID
must make that retry safe. The delivery path does not consume evidence or video
priority items.

There is intentionally no HTTP client, bearer-token fallback, proxy-header
trust or executable wiring in this slice. Render's current TLS termination does
not prove an edge client certificate. A live sender remains blocked until an
approved endpoint verifies the device certificate end to end and returns
durable event acceptance.

Tests use synthetic JSON metadata only. They cover malformed and autonomous
fields, biometric rejection, tenant mismatch, idempotent and conflicting replay,
capacity exhaustion without eviction, priority isolation, ordered delivery,
downstream failure and restart recovery. No secret, customer footage, person,
face, plate, embedding, ReID data, model weight, dataset, evidence object,
notification, alarm, dispatch, access-control action, activation or production
claim is introduced.

Production remains blocked on the controlled privacy-release loader,
allowlisted hardware executor, physical signed-mask HIL, verified mTLS endpoint,
licensed promoted models, held-out evaluation and explicit human approval.
