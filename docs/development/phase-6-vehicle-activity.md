# Phase 6 event-only vehicle activity

T-0307 implements the FR-103a boundary between a single-camera tracker and the existing tenant event pipeline. It does not add model weights, a tracker implementation, customer footage, or any Phase 2 vehicle identity feature.

`VehicleTrackObservation` accepts the first confirmed observation of one camera-local track. `build_vehicle_activity_event` validates tenant, site, camera, and zone UUIDs; a bounded tracker identifier; a timezone-aware first-seen time; finite confidence; model provenance; evidence references; and one of the six shared-detector vehicle classes. Event and deduplication identities are deterministic for that camera track and first-seen time, so transport retries reproduce the same logical event.

The emitted contract is deliberately narrow:

- `event_type` is `vehicle_activity`.
- `observed_behavior` is only `detected`.
- `subject_class` is `bicycle`, `bus`, `car`, `motorcycle`, `truck`, or `van`.
- `requires_human_review` is always `true` and `review_state` is always `pending`.
- Plate text, appearance embeddings, ReID, speed, cross-camera identity, risk scores, and theft conclusions are absent.

The additive Avro, Protobuf, and OpenAPI fields remain optional for older and non-vehicle producers. The HTTP ingestion boundary requires both fields for `vehicle_activity`, rejects them on other event types, restricts their values, and continues to reject unknown enrichment fields. Existing non-vehicle canonical payload hashes remain unchanged because empty vehicle metadata is omitted.

Run the focused checks from the repository root:

```text
python -m unittest discover -s ai-services/tests -v
go test ./backend/internal/eventing ./backend/internal/httpapi
python scripts/validate_contracts.py
python scripts/validate_contract_compatibility.py
```

This slice provides the event and safety boundary only. The shared detector and ByteTrack dependency still require their approved model/evaluation artifacts, hardware benchmarks, shadow/canary promotion, and human-oversight release gates before production advertising.
