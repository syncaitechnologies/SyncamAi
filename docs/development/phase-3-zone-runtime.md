# Phase 3 camera-local zone-rule runtime

`T-0332` establishes the deterministic geometry and state-machine boundary for the non-mask rules delivered by `T-0331`.

- Enabled, camera-bound GeoJSON rules can be evaluated as intrusion, restricted-zone, loitering, or tripwire logic using local tracker points in the same configured coordinate space. Loitering uses a versioned per-zone dwell duration between 30 seconds and 10 minutes, defaulting to 30 seconds.
- An entry, dwell-threshold, or line-side transition emits a retry-stable canonical event that remains pending human review. Coordinates and tracker IDs are process-local inputs; they do not appear in emitted events.
- Rules and observations are bounded, tenant/site/camera matched, and timestamp ordered. Invalid geometry, invalid bounds, and privacy-mask configurations fail closed.
- A zone may optionally gate its deterministic evaluation to the bounded canonical local subject classes. A rejected class never advances the rule state or emits an event.
- This is a runtime foundation only. Tracker/detector transport, production calibration, pre-encode privacy verification, and hardware-in-loop validation remain release gates.

Privacy mask zones are deliberately deferred until the final security-approved slice. They are not accepted or executed by this generic rule engine.
