"""Validate foundation contract invariants without external dependencies."""

from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
OPENAPI = ROOT / "shared/contracts/openapi/v1.yaml"
AVRO = ROOT / "shared/contracts/avro/detection-event-v1.avsc"
PROTO = ROOT / "shared/contracts/proto/events/v1/events.proto"
REALTIME = ROOT / "shared/contracts/jsonschema/realtime-envelope-v1.schema.json"
REQUIRED_FIELDS = {
    "event_id",
    "tenant_id",
    "dedupe_key",
    "occurred_at",
    "site_id",
    "camera_id",
    "zone_id",
    "event_type",
    "model_version",
    "confidence",
    "evidence_refs",
    "requires_human_review",
    "review_state",
}


def main() -> int:
    errors: list[str] = []
    avro = json.loads(AVRO.read_text(encoding="utf-8"))
    avro_fields = {field["name"]: field for field in avro.get("fields", [])}
    missing_avro = sorted(REQUIRED_FIELDS - avro_fields.keys())
    if missing_avro:
        errors.append(f"Avro missing: {', '.join(missing_avro)}")
    if avro_fields.get("requires_human_review", {}).get("default") is not True:
        errors.append("Avro requires_human_review must default to true")

    openapi = OPENAPI.read_text(encoding="utf-8")
    for token in REQUIRED_FIELDS | {"api.sentinelvision.ai", "X-SentinelVision-Tenant-ID"}:
        if token not in openapi:
            errors.append(f"OpenAPI missing invariant {token}")
    if not re.search(r"requires_human_review:\s*\n\s*type: boolean\s*\n\s*const: true", openapi):
        errors.append("OpenAPI must constrain requires_human_review to true")

    proto = PROTO.read_text(encoding="utf-8")
    for field in REQUIRED_FIELDS:
        if not re.search(rf"\b{re.escape(field)}\s*=\s*\d+;", proto):
            errors.append(f"Proto missing field {field}")

    realtime = json.loads(REALTIME.read_text(encoding="utf-8"))
    if realtime.get("properties", {}).get("v", {}).get("const") != 1:
        errors.append("Realtime envelope version must be fixed at 1")
    realtime_types = set(realtime.get("properties", {}).get("type", {}).get("enum", []))
    if realtime_types != {"event", "snapshot", "gap", "pong"}:
        errors.append("Realtime envelope types must be event, snapshot, gap, and pong")
    realtime_topics = set(realtime.get("properties", {}).get("topic", {}).get("enum", []))
    if not {"alerts.*", "alerts.created", "alerts.state"}.issubset(realtime_topics):
        errors.append("Realtime envelope is missing canonical alert topics")

    if errors:
        print("Contract validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print("contracts: ok (OpenAPI, Avro, Proto, and realtime invariants aligned)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
