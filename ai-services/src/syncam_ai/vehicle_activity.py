"""Build bounded FR-103a vehicle-activity events from single-camera tracks."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from math import isfinite
from typing import Final
from uuid import UUID, uuid5

OBSERVED_BEHAVIOR: Final = "detected"
VEHICLE_CLASSES: Final = frozenset(
    {"bicycle", "bus", "car", "motorcycle", "truck", "van"}
)
_EVENT_NAMESPACE: Final = UUID("9ab73957-2db1-4ed8-a022-23c2ccb76cb8")
_MAX_TRACK_ID: Final = (1 << 63) - 1
_MAX_EVIDENCE_REFS: Final = 32


@dataclass(frozen=True, slots=True)
class VehicleTrackObservation:
    """A confirmed single-camera tracker observation.

    ``track_id`` is deliberately local to one camera. It is used only to make
    retries deterministic and is not emitted as identity or ReID metadata.
    """

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    track_id: int
    first_seen_at: datetime
    subject_class: str
    confidence: float
    model_version: str
    evidence_refs: tuple[str, ...] = ()


def build_vehicle_activity_event(observation: VehicleTrackObservation) -> dict[str, object]:
    """Return a canonical, retry-stable, human-review-required event.

    The output intentionally has no plate, appearance embedding, speed,
    cross-camera identity, risk score, or theft conclusion.
    """

    tenant_id = _canonical_uuid(observation.tenant_id, "tenant_id")
    site_id = _canonical_uuid(observation.site_id, "site_id")
    camera_id = _canonical_uuid(observation.camera_id, "camera_id")
    zone_id = _canonical_uuid(observation.zone_id, "zone_id")
    if (
        not isinstance(observation.track_id, int)
        or isinstance(observation.track_id, bool)
        or not 0 <= observation.track_id <= _MAX_TRACK_ID
    ):
        raise ValueError("track_id must be a non-negative signed 64-bit integer")

    first_seen_at = observation.first_seen_at
    if first_seen_at.tzinfo is None or first_seen_at.utcoffset() is None:
        raise ValueError("first_seen_at must be timezone-aware")
    occurred_at = first_seen_at.astimezone(timezone.utc)

    subject_class = observation.subject_class.strip().lower()
    if subject_class not in VEHICLE_CLASSES:
        raise ValueError("subject_class is not a canonical MVP vehicle class")
    if (
        isinstance(observation.confidence, bool)
        or not isinstance(observation.confidence, (int, float))
        or not isfinite(observation.confidence)
        or not 0 <= observation.confidence <= 1
    ):
        raise ValueError("confidence must be finite and between zero and one")

    model_version = observation.model_version.strip()
    if not model_version or len(model_version) > 128:
        raise ValueError("model_version must contain between 1 and 128 characters")
    evidence_refs = _evidence_refs(observation.evidence_refs)

    timestamp = occurred_at.isoformat(timespec="microseconds").replace("+00:00", "Z")
    source_key = f"{camera_id}:{observation.track_id}:{timestamp}"
    event_id = str(uuid5(_EVENT_NAMESPACE, f"{tenant_id}:{source_key}"))
    dedupe_key = f"vehicle_activity:{source_key}"

    return {
        "event_id": event_id,
        "tenant_id": tenant_id,
        "dedupe_key": dedupe_key,
        "occurred_at": timestamp,
        "site_id": site_id,
        "camera_id": camera_id,
        "zone_id": zone_id,
        "event_type": "vehicle_activity",
        "model_version": model_version,
        "confidence": observation.confidence,
        "evidence_refs": evidence_refs,
        "requires_human_review": True,
        "review_state": "pending",
        "observed_behavior": OBSERVED_BEHAVIOR,
        "subject_class": subject_class,
    }


def _canonical_uuid(value: str, field: str) -> str:
    try:
        return str(UUID(value.strip()))
    except (AttributeError, ValueError) as error:
        raise ValueError(f"{field} must be a UUID") from error


def _evidence_refs(values: tuple[str, ...]) -> list[str]:
    if len(values) > _MAX_EVIDENCE_REFS:
        raise ValueError("evidence_refs cannot contain more than 32 entries")
    result: list[str] = []
    for value in values:
        normalized = value.strip()
        if not normalized or len(normalized) > 1024:
            raise ValueError(
                "evidence_refs entries must contain between 1 and 1024 characters"
            )
        result.append(normalized)
    return result
