# Phase 6 vehicle track confirmation

T-0419 composes the camera-local track output with the existing FR-103a
review-required vehicle event builder. `VehicleActivityConfirmation` is bound
to one tenant, site, camera, and configured monitoring-zone identifier. It
accepts only ordered metadata-only `TrackFrame` values from that scope and
ignores person observations.

A vehicle must remain on the same local track for a configurable two through
ten consecutive sampled frames (three by default) before one event is emitted.
The event uses the first observation in that uninterrupted streak and the
minimum confidence across the streak. A gap resets an unconfirmed streak;
already emitted state prevents duplicate alerts until the track expires.
Subject class and model-version provenance cannot change under an existing
track identifier.

State is capped at 2,048 vehicle tracks (512 by default), with at most 30
missed frames retained. Frame scope, strictly increasing timestamp, duplicate
track, normalized confidence/coordinate, class, provenance, capacity, and
configuration failures occur before state or metrics change. Metrics expose
counts only. Track identifiers remain local deduplication inputs and are not
included in emitted events.

The configured zone identifier supplies event context; this component does
not infer a zone, plate, identity, speed, route, risk, or theft conclusion.
The canonical output remains `observed_behavior=detected`, pending human
review, and contains no plate, ReID, embedding, track ID, or coordinate.

Synthetic tests compose the T-0418 association output through three stable
car observations, verify one deterministic event, reject provenance changes
and replay, restart confirmation after a gap, and preserve atomic capacity
failure. This is not detector inference, model evaluation, customer footage,
an activated feature, or a production accuracy claim. Model release and the
real monitoring-zone assignment remain separately controlled.
