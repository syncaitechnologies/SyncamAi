"""Evaluate camera-local non-mask zone rules from bounded tracker metadata.

This module intentionally has no pixel, image, embedding, plate, identity, or
cross-camera inputs. It is the deterministic geometry/state boundary that sits
after tracking and before canonical human-review event ingestion.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from math import isfinite
from typing import Final, Mapping, Sequence
from uuid import UUID, uuid5


SUPPORTED_KINDS: Final = frozenset(
    {"intrusion", "restricted_zone", "loitering", "tripwire"}
)
SUPPORTED_SUBJECT_CLASSES: Final = frozenset({"person", "bicycle", "bus", "car", "motorcycle", "truck", "van"})
_EVENT_NAMESPACE: Final = UUID("93a83fc9-2f58-4e69-99ea-5c70b112bdf8")
_MAX_TRACK_ID: Final = (1 << 63) - 1
_MAX_EVIDENCE_REFS: Final = 32
_MAX_COORDINATE: Final = 1_000_000.0
_MAX_ZONES: Final = 1_000
DEFAULT_LOITER_SECONDS: Final = 30
MIN_LOITER_SECONDS: Final = 30
MAX_LOITER_SECONDS: Final = 600


@dataclass(frozen=True, slots=True)
class ZoneRule:
    """One enabled camera-bound rule from a delivered zone configuration."""

    id: str
    tenant_id: str
    site_id: str
    camera_id: str
    kind: str
    geometry: Mapping[str, object]
    enabled: bool = True
    loiter_seconds: int = DEFAULT_LOITER_SECONDS
    subject_classes: frozenset[str] = frozenset()


@dataclass(frozen=True, slots=True)
class TrackObservation:
    """A single-camera tracker point in the configured geometry coordinate space."""

    tenant_id: str
    site_id: str
    camera_id: str
    track_id: int
    observed_at: datetime
    center_x: float
    center_y: float
    subject_class: str
    confidence: float
    model_version: str
    evidence_refs: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class _CompiledRule:
    id: str
    tenant_id: str
    site_id: str
    camera_id: str
    kind: str
    points: tuple[tuple[float, float], ...]
    loiter_seconds: int
    subject_classes: frozenset[str]


@dataclass(slots=True)
class _TrackState:
    observed_at: datetime
    inside: bool | None = None
    entered_at: datetime | None = None
    emitted: bool = False
    line_side: int | None = None


class ZoneRuleEngine:
    """Stateful, bounded, deterministic evaluation for one edge runtime.

    Track state is in-memory only. Event payloads deliberately exclude tracker
    IDs and coordinates, which are solely local matching inputs.
    """

    def __init__(self, rules: Sequence[ZoneRule]) -> None:
        if len(rules) > _MAX_ZONES:
            raise ValueError("zone rule count exceeds the runtime bound")
        compiled = tuple(_compile_rule(rule) for rule in rules if rule.enabled)
        identifiers = [rule.id for rule in compiled]
        if len(identifiers) != len(set(identifiers)):
            raise ValueError("zone rule IDs must be unique")
        self._rules = compiled
        self._states: dict[tuple[str, int], _TrackState] = {}

    def observe(self, observation: TrackObservation) -> list[dict[str, object]]:
        """Return new canonical events for this local track point only."""

        canonical = _canonical_observation(observation)
        events: list[dict[str, object]] = []
        for rule in self._rules:
            if (
                rule.tenant_id != canonical.tenant_id
                or rule.site_id != canonical.site_id
                or rule.camera_id != canonical.camera_id
            ):
                continue
            if rule.subject_classes and canonical.subject_class not in rule.subject_classes:
                continue
            key = (rule.id, canonical.track_id)
            state = self._states.get(key)
            if rule.kind == "tripwire":
                event, state = _observe_tripwire(rule, canonical, state)
            else:
                event, state = _observe_polygon(rule, canonical, state)
            self._states[key] = state
            if event is not None:
                events.append(event)
        return events


def load_zone_rules(payload: Mapping[str, object]) -> list[ZoneRule]:
    """Compile applicable camera-bound rules from a delivered config payload.

    Privacy-mask zones are rejected rather than ignored. A caller must not
    accidentally run a generic analytics path on a configuration that includes
    an unapproved mask kind.
    """

    raw_zones = payload.get("zones")
    if not isinstance(raw_zones, list) or len(raw_zones) > _MAX_ZONES:
        raise ValueError("configuration zones must be a bounded list")
    result: list[ZoneRule] = []
    for raw in raw_zones:
        if not isinstance(raw, Mapping):
            raise ValueError("configuration zone must be an object")
        kind = _text(raw.get("kind"), "kind").lower()
        if kind == "mask":
            raise ValueError("privacy mask zones require the separate approved runtime")
        if kind not in SUPPORTED_KINDS:
            continue
        if raw.get("enabled") is not True:
            continue
        camera_id = _text(raw.get("camera_id"), "camera_id", allow_empty=True)
        if not camera_id:
            # Site-map zones cannot be evaluated against camera pixel tracks.
            continue
        geometry = raw.get("geometry")
        if not isinstance(geometry, Mapping):
            raise ValueError("zone geometry must be an object")
        loiter_seconds = DEFAULT_LOITER_SECONDS
        if kind == "loitering":
            raw_loiter_seconds = raw.get("loiter_seconds", DEFAULT_LOITER_SECONDS)
            if not isinstance(raw_loiter_seconds, int) or isinstance(raw_loiter_seconds, bool):
                raise ValueError("loiter_seconds must be an integer")
            if not MIN_LOITER_SECONDS <= raw_loiter_seconds <= MAX_LOITER_SECONDS:
                raise ValueError("loiter_seconds must be between 30 and 600")
            loiter_seconds = raw_loiter_seconds
        elif "loiter_seconds" in raw:
            raise ValueError("loiter_seconds is valid only for loitering zones")
        subject_classes = _subject_classes(raw.get("subject_classes", []))
        result.append(
            ZoneRule(
                id=_text(raw.get("id"), "id"),
                tenant_id=_text(raw.get("tenant_id"), "tenant_id"),
                site_id=_text(raw.get("site_id"), "site_id"),
                camera_id=camera_id,
                kind=kind,
                geometry=geometry,
                loiter_seconds=loiter_seconds,
                subject_classes=subject_classes,
            )
        )
    return result


def _compile_rule(rule: ZoneRule) -> _CompiledRule:
    if rule.kind not in SUPPORTED_KINDS:
        raise ValueError("zone kind is not supported by the runtime")
    expected_type = "LineString" if rule.kind == "tripwire" else "Polygon"
    geometry_type = _text(rule.geometry.get("type"), "geometry type")
    if geometry_type != expected_type:
        raise ValueError("zone geometry type does not match the rule kind")
    raw_coordinates = rule.geometry.get("coordinates")
    if rule.kind == "tripwire":
        points = _points(raw_coordinates, 2)
    else:
        if not isinstance(raw_coordinates, Sequence) or isinstance(raw_coordinates, (str, bytes)) or len(raw_coordinates) != 1:
            raise ValueError("polygon zones must contain exactly one ring")
        points = _points(raw_coordinates[0], 4)
        if points[0] != points[-1] or _polygon_area(points) == 0:
            raise ValueError("polygon zones must be closed and have area")
    if rule.kind == "loitering" and not MIN_LOITER_SECONDS <= rule.loiter_seconds <= MAX_LOITER_SECONDS:
        raise ValueError("loiter_seconds must be between 30 and 600")
    return _CompiledRule(
        id=_uuid(rule.id, "zone id"),
        tenant_id=_uuid(rule.tenant_id, "tenant_id"),
        site_id=_uuid(rule.site_id, "site_id"),
        camera_id=_uuid(rule.camera_id, "camera_id"),
        kind=rule.kind,
        points=points,
        loiter_seconds=rule.loiter_seconds,
        subject_classes=rule.subject_classes,
    )


def _observe_polygon(
    rule: _CompiledRule, observation: TrackObservation, previous: _TrackState | None
) -> tuple[dict[str, object] | None, _TrackState]:
    inside = _point_in_polygon(observation.center_x, observation.center_y, rule.points)
    state = previous or _TrackState(observed_at=observation.observed_at)
    _ordered(state, observation.observed_at)
    entered = state.inside is False and inside
    if inside and state.inside is not True:
        state.entered_at = observation.observed_at
        state.emitted = False
    if not inside:
        state.entered_at = None
        state.emitted = False
    state.inside = inside
    state.observed_at = observation.observed_at
    if rule.kind in {"intrusion", "restricted_zone"} and entered:
        state.emitted = True
        return _event(rule, observation, "entered", observation.observed_at), state
    if rule.kind == "loitering" and inside and state.entered_at is not None and not state.emitted:
        if (observation.observed_at - state.entered_at).total_seconds() >= rule.loiter_seconds:
            state.emitted = True
            return _event(rule, observation, "dwell_exceeded", state.entered_at), state
    return None, state


def _observe_tripwire(
    rule: _CompiledRule, observation: TrackObservation, previous: _TrackState | None
) -> tuple[dict[str, object] | None, _TrackState]:
    first, last = rule.points[0], rule.points[-1]
    side = _side(first, last, observation.center_x, observation.center_y)
    state = previous or _TrackState(observed_at=observation.observed_at)
    _ordered(state, observation.observed_at)
    prior_side = state.line_side
    if side != 0:
        state.line_side = side
    state.observed_at = observation.observed_at
    if prior_side is not None and side != 0 and prior_side != side:
        return _event(rule, observation, "crossed", observation.observed_at), state
    return None, state


def _event(rule: _CompiledRule, observation: TrackObservation, behavior: str, started_at: datetime) -> dict[str, object]:
    start = _timestamp(started_at)
    occurred = _timestamp(observation.observed_at)
    source_key = f"{observation.camera_id}:{rule.id}:{observation.track_id}:{behavior}:{start}"
    return {
        "event_id": str(uuid5(_EVENT_NAMESPACE, f"{observation.tenant_id}:{source_key}")),
        "tenant_id": observation.tenant_id,
        "dedupe_key": f"{rule.kind}:{source_key}",
        "occurred_at": occurred,
        "site_id": observation.site_id,
        "camera_id": observation.camera_id,
        "zone_id": rule.id,
        "event_type": rule.kind,
        "model_version": observation.model_version,
        "confidence": observation.confidence,
        "evidence_refs": list(observation.evidence_refs),
        "requires_human_review": True,
        "review_state": "pending",
        "observed_behavior": behavior,
        "subject_class": observation.subject_class,
    }


def _canonical_observation(value: TrackObservation) -> TrackObservation:
    if not isinstance(value.track_id, int) or isinstance(value.track_id, bool) or not 0 <= value.track_id <= _MAX_TRACK_ID:
        raise ValueError("track_id must be a non-negative signed 64-bit integer")
    observed = value.observed_at
    if observed.tzinfo is None or observed.utcoffset() is None:
        raise ValueError("observed_at must be timezone-aware")
    if not isinstance(value.confidence, (int, float)) or isinstance(value.confidence, bool) or not isfinite(value.confidence) or not 0 <= value.confidence <= 1:
        raise ValueError("confidence must be finite and between zero and one")
    references = _evidence_refs(value.evidence_refs)
    subject_class = _text(value.subject_class, "subject_class").lower()
    if len(subject_class) > 64:
        raise ValueError("subject_class must contain at most 64 characters")
    model_version = _text(value.model_version, "model_version")
    if len(model_version) > 128:
        raise ValueError("model_version must contain at most 128 characters")
    return TrackObservation(
        tenant_id=_uuid(value.tenant_id, "tenant_id"), site_id=_uuid(value.site_id, "site_id"),
        camera_id=_uuid(value.camera_id, "camera_id"), track_id=value.track_id,
        observed_at=observed.astimezone(timezone.utc), center_x=_coordinate(value.center_x, "center_x"),
        center_y=_coordinate(value.center_y, "center_y"), subject_class=subject_class,
        confidence=float(value.confidence), model_version=model_version, evidence_refs=tuple(references),
    )


def _points(value: object, minimum: int) -> tuple[tuple[float, float], ...]:
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes)) or len(value) < minimum:
        raise ValueError("geometry must contain enough coordinate pairs")
    points: list[tuple[float, float]] = []
    for raw in value:
        if not isinstance(raw, Sequence) or isinstance(raw, (str, bytes)) or len(raw) != 2:
            raise ValueError("geometry coordinate must be a pair")
        points.append((_coordinate(raw[0], "geometry x"), _coordinate(raw[1], "geometry y")))
    return tuple(points)


def _point_in_polygon(x: float, y: float, points: tuple[tuple[float, float], ...]) -> bool:
    inside = False
    for index in range(len(points) - 1):
        left, right = points[index], points[index + 1]
        if _on_segment(left, right, x, y):
            return True
        if (left[1] > y) != (right[1] > y):
            crossing = (right[0] - left[0]) * (y - left[1]) / (right[1] - left[1]) + left[0]
            if x < crossing:
                inside = not inside
    return inside


def _on_segment(left: tuple[float, float], right: tuple[float, float], x: float, y: float) -> bool:
    return _side(left, right, x, y) == 0 and min(left[0], right[0]) <= x <= max(left[0], right[0]) and min(left[1], right[1]) <= y <= max(left[1], right[1])


def _side(left: tuple[float, float], right: tuple[float, float], x: float, y: float) -> int:
    value = (right[0] - left[0]) * (y - left[1]) - (right[1] - left[1]) * (x - left[0])
    return 1 if value > 0 else -1 if value < 0 else 0


def _polygon_area(points: tuple[tuple[float, float], ...]) -> float:
    return sum(points[index][0] * points[index + 1][1] - points[index + 1][0] * points[index][1] for index in range(len(points) - 1)) / 2


def _ordered(state: _TrackState, observed_at: datetime) -> None:
    if observed_at < state.observed_at:
        raise ValueError("observations must be timestamp ordered per zone track")


def _coordinate(value: object, field: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool) or not isfinite(value) or abs(value) > _MAX_COORDINATE:
        raise ValueError(f"{field} must be finite and within the configured coordinate bounds")
    return float(value)


def _uuid(value: object, field: str) -> str:
    try:
        return str(UUID(_text(value, field)))
    except ValueError as error:
        raise ValueError(f"{field} must be a UUID") from error


def _text(value: object, field: str, allow_empty: bool = False) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a string")
    result = value.strip()
    if (not allow_empty and not result) or len(result) > 1024:
        raise ValueError(f"{field} must contain between 1 and 1024 characters")
    return result


def _evidence_refs(values: tuple[str, ...]) -> list[str]:
    if len(values) > _MAX_EVIDENCE_REFS:
        raise ValueError("evidence_refs cannot contain more than 32 entries")
    result: list[str] = []
    for value in values:
        result.append(_text(value, "evidence ref"))
    return result


def _subject_classes(value: object) -> frozenset[str]:
    if not isinstance(value, list) or len(value) > len(SUPPORTED_SUBJECT_CLASSES):
        raise ValueError("subject_classes must be a bounded list")
    result: set[str] = set()
    for raw in value:
        subject_class = _text(raw, "subject_class").lower()
        if subject_class not in SUPPORTED_SUBJECT_CLASSES or subject_class in result:
            raise ValueError("subject_classes must be unique canonical classes")
        result.add(subject_class)
    return frozenset(result)


def _timestamp(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")
