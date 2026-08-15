# Shared contracts

This directory is the compatibility boundary between edge, platform, AI, and web components.

- `openapi/v1.yaml`: external REST API contract; canonical wire branding is preserved.
- `avro/detection-event-v1.avsc`: event stream envelope.
- `proto/events/v1/events.proto`: internal gRPC transport.
- `jsonschema/realtime-envelope-v1.schema.json`: versioned WebSocket event, snapshot, gap, and pong envelope.

Foundation schemas intentionally expose one minimal event endpoint. Product APIs are added phase by phase with additive compatibility checks.

FR-103a vehicle activity uses the additive `observed_behavior=detected` and canonical `subject_class` fields. They remain optional for older and non-vehicle producers, while the ingestion boundary requires both on `vehicle_activity` and rejects identity, plate, speed, ReID, risk-score, and theft-claim enrichment.
