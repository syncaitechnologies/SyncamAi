"""Camera-local abandoned-object temporal logic and deterministic evaluation."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from math import hypot, isfinite
from typing import Final
from uuid import UUID, uuid5

ABANDONED_OBJECT_CLASSES: Final = frozenset({"bag", "box", "suitcase"})
_EVENT_NAMESPACE: Final = UUID("64ac5f48-369d-437a-a680-6edb53ffae63")
_MAX_TRACK_ID: Final = (1 << 63) - 1
_MAX_EVIDENCE_REFS: Final = 32


@dataclass(frozen=True, slots=True)
class PersonTrack:
    """A camera-local person track used only for owner association.

    The track identifier never leaves this module or the emitted event. It is
    not a biometric or cross-camera identity.
    """

    track_id: int
    center_x: float
    center_y: float


@dataclass(frozen=True, slots=True)
class AbandonedObjectObservation:
    """One static-object observation and the visible camera-local people."""

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    object_track_id: int
    object_class: str
    observed_at: datetime
    center_x: float
    center_y: float
    confidence: float
    model_version: str
    evidence_refs: tuple[str, ...] = ()
    person_tracks: tuple[PersonTrack, ...] = ()


@dataclass(frozen=True, slots=True)
class AbandonedObjectThresholds:
    """Per-zone temporal settings with conservative development defaults.

    ``static_dwell_seconds`` is T1 from the approved 30--60 second range.
    ``alert_dwell_seconds`` is T2 and must not be shorter than T1. Production
    values remain subject to per-zone evaluation and human approval.
    """

    static_dwell_seconds: int = 30
    alert_dwell_seconds: int = 60
    movement_tolerance: float = 0.02
    owner_association_radius: float = 0.10

    def __post_init__(self) -> None:
        if not 30 <= self.static_dwell_seconds <= 60:
            raise ValueError("static_dwell_seconds must be between 30 and 60")
        if not self.static_dwell_seconds <= self.alert_dwell_seconds <= 86_400:
            raise ValueError("alert_dwell_seconds must be between T1 and one day")
        for name, value in (
            ("movement_tolerance", self.movement_tolerance),
            ("owner_association_radius", self.owner_association_radius),
        ):
            if (
                not isinstance(value, (int, float))
                or isinstance(value, bool)
                or not isfinite(value)
            ):
                raise ValueError(f"{name} must be finite")
        if not 0 <= self.movement_tolerance <= 1:
            raise ValueError("movement_tolerance must be between zero and one")
        if not 0 < self.owner_association_radius <= 1.5:
            raise ValueError("owner_association_radius must be between zero and 1.5")


@dataclass(slots=True)
class _ObjectState:
    static_since: datetime
    last_seen_at: datetime
    anchor_x: float
    anchor_y: float
    owner_track_id: int | None
    emitted: bool = False


@dataclass(frozen=True, slots=True)
class EvaluationCase:
    """A deterministic, synthetic temporal regression case."""

    name: str
    observations: tuple[AbandonedObjectObservation, ...]
    expects_alert: bool


@dataclass(frozen=True, slots=True)
class EvaluationSummary:
    cases: int
    true_positives: int
    true_negatives: int
    false_positives: int
    false_negatives: int

    @property
    def detection_rate(self) -> float:
        positives = self.true_positives + self.false_negatives
        return 1.0 if positives == 0 else self.true_positives / positives


class AbandonedObjectEngine:
    """Emit one review-required alert after static dwell and owner separation.

    Inputs must arrive in timestamp order for each camera-local object track.
    No raw frames, person identifiers, object coordinates, or owner-track IDs
    are persisted in the resulting event.
    """

    def __init__(self, thresholds: AbandonedObjectThresholds | None = None) -> None:
        self._thresholds = thresholds or AbandonedObjectThresholds()
        self._states: dict[tuple[str, str, str, str, int], _ObjectState] = {}

    def observe(self, observation: AbandonedObjectObservation) -> dict[str, object] | None:
        """Process an observation and return an alert exactly once, if confirmed."""

        canonical = _validate_observation(observation)
        key = (
            canonical.tenant_id,
            canonical.site_id,
            canonical.camera_id,
            canonical.zone_id,
            canonical.object_track_id,
        )
        state = self._states.get(key)
        if state is None:
            state = _ObjectState(
                static_since=canonical.observed_at,
                last_seen_at=canonical.observed_at,
                anchor_x=canonical.center_x,
                anchor_y=canonical.center_y,
                owner_track_id=_nearest_owner(canonical, self._thresholds.owner_association_radius),
            )
            self._states[key] = state
            return None

        if canonical.observed_at < state.last_seen_at:
            raise ValueError("observations must be timestamp ordered per object track")
        if _distance(
            canonical.center_x, canonical.center_y, state.anchor_x, state.anchor_y
        ) > self._thresholds.movement_tolerance:
            self._states[key] = _ObjectState(
                static_since=canonical.observed_at,
                last_seen_at=canonical.observed_at,
                anchor_x=canonical.center_x,
                anchor_y=canonical.center_y,
                owner_track_id=_nearest_owner(canonical, self._thresholds.owner_association_radius),
            )
            return None
        state.last_seen_at = canonical.observed_at

        static_seconds = (canonical.observed_at - state.static_since).total_seconds()
        if state.owner_track_id is None and static_seconds < self._thresholds.static_dwell_seconds:
            state.owner_track_id = _nearest_owner(canonical, self._thresholds.owner_association_radius)
        if state.owner_track_id is None:
            return None

        owner_visible = any(track.track_id == state.owner_track_id for track in canonical.person_tracks)
        if owner_visible:
            return None

        if state.emitted or static_seconds < self._thresholds.alert_dwell_seconds:
            return None
        state.emitted = True
        return _build_event(canonical, state.static_since)


def evaluate_cases(
    cases: tuple[EvaluationCase, ...], thresholds: AbandonedObjectThresholds | None = None
) -> EvaluationSummary:
    """Evaluate synthetic temporal cases without asserting production accuracy."""

    true_positives = true_negatives = false_positives = false_negatives = 0
    for case in cases:
        engine = AbandonedObjectEngine(thresholds)
        emitted = any(engine.observe(observation) is not None for observation in case.observations)
        if emitted and case.expects_alert:
            true_positives += 1
        elif emitted:
            false_positives += 1
        elif case.expects_alert:
            false_negatives += 1
        else:
            true_negatives += 1
    return EvaluationSummary(
        cases=len(cases),
        true_positives=true_positives,
        true_negatives=true_negatives,
        false_positives=false_positives,
        false_negatives=false_negatives,
    )


def _validate_observation(observation: AbandonedObjectObservation) -> AbandonedObjectObservation:
    tenant_id = _canonical_uuid(observation.tenant_id, "tenant_id")
    site_id = _canonical_uuid(observation.site_id, "site_id")
    camera_id = _canonical_uuid(observation.camera_id, "camera_id")
    zone_id = _canonical_uuid(observation.zone_id, "zone_id")
    _track_id(observation.object_track_id, "object_track_id")
    if not isinstance(observation.object_class, str):
        raise ValueError("object_class must be a string")
    object_class = observation.object_class.strip().lower()
    if object_class not in ABANDONED_OBJECT_CLASSES:
        raise ValueError("object_class is not a canonical abandoned-object class")
    observed_at = observation.observed_at
    if observed_at.tzinfo is None or observed_at.utcoffset() is None:
        raise ValueError("observed_at must be timezone-aware")
    center_x = _normalized_coordinate(observation.center_x, "center_x")
    center_y = _normalized_coordinate(observation.center_y, "center_y")
    confidence = _confidence(observation.confidence)
    if not isinstance(observation.model_version, str):
        raise ValueError("model_version must be a string")
    model_version = observation.model_version.strip()
    if not model_version or len(model_version) > 128:
        raise ValueError("model_version must contain between 1 and 128 characters")
    evidence_refs = _evidence_refs(observation.evidence_refs)
    person_tracks = tuple(observation.person_tracks)
    seen_person_tracks: set[int] = set()
    for track in person_tracks:
        if not isinstance(track, PersonTrack):
            raise ValueError("person_tracks entries must be PersonTrack values")
        _track_id(track.track_id, "person track_id")
        if track.track_id in seen_person_tracks:
            raise ValueError("person track IDs must be unique per observation")
        seen_person_tracks.add(track.track_id)
        _normalized_coordinate(track.center_x, "person center_x")
        _normalized_coordinate(track.center_y, "person center_y")
    return AbandonedObjectObservation(
        tenant_id=tenant_id,
        site_id=site_id,
        camera_id=camera_id,
        zone_id=zone_id,
        object_track_id=observation.object_track_id,
        object_class=object_class,
        observed_at=observed_at.astimezone(timezone.utc),
        center_x=center_x,
        center_y=center_y,
        confidence=confidence,
        model_version=model_version,
        evidence_refs=tuple(evidence_refs),
        person_tracks=person_tracks,
    )


def _build_event(observation: AbandonedObjectObservation, static_since: datetime) -> dict[str, object]:
    static_timestamp = static_since.isoformat(timespec="microseconds").replace("+00:00", "Z")
    occurred_at = observation.observed_at.isoformat(timespec="microseconds").replace("+00:00", "Z")
    source_key = f"{observation.camera_id}:{observation.object_track_id}:{static_timestamp}"
    return {
        "event_id": str(uuid5(_EVENT_NAMESPACE, f"{observation.tenant_id}:{source_key}")),
        "tenant_id": observation.tenant_id,
        "dedupe_key": f"abandoned_object:{source_key}",
        "occurred_at": occurred_at,
        "site_id": observation.site_id,
        "camera_id": observation.camera_id,
        "zone_id": observation.zone_id,
        "event_type": "abandoned_object_review",
        "model_version": observation.model_version,
        "confidence": observation.confidence,
        "evidence_refs": list(observation.evidence_refs),
        "requires_human_review": True,
        "review_state": "pending",
    }


def _nearest_owner(observation: AbandonedObjectObservation, radius: float) -> int | None:
    nearby = [
        (
            _distance(
                observation.center_x,
                observation.center_y,
                track.center_x,
                track.center_y,
            ),
            track.track_id,
        )
        for track in observation.person_tracks
    ]
    if not nearby:
        return None
    distance, track_id = min(nearby)
    return track_id if distance <= radius else None


def _canonical_uuid(value: str, field: str) -> str:
    try:
        return str(UUID(value.strip()))
    except (AttributeError, ValueError) as error:
        raise ValueError(f"{field} must be a UUID") from error


def _track_id(value: int, field: str) -> None:
    if not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= _MAX_TRACK_ID:
        raise ValueError(f"{field} must be a non-negative signed 64-bit integer")


def _normalized_coordinate(value: float, field: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool) or not isfinite(value):
        raise ValueError(f"{field} must be finite")
    if not 0 <= value <= 1:
        raise ValueError(f"{field} must be between zero and one")
    return float(value)


def _confidence(value: float) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool) or not isfinite(value):
        raise ValueError("confidence must be finite and between zero and one")
    if not 0 <= value <= 1:
        raise ValueError("confidence must be finite and between zero and one")
    return float(value)


def _evidence_refs(values: tuple[str, ...]) -> list[str]:
    if len(values) > _MAX_EVIDENCE_REFS:
        raise ValueError("evidence_refs cannot contain more than 32 entries")
    result: list[str] = []
    for value in values:
        if not isinstance(value, str):
            raise ValueError("evidence_refs entries must be strings")
        normalized = value.strip()
        if not normalized or len(normalized) > 1024:
            raise ValueError("evidence_refs entries must contain between 1 and 1024 characters")
        result.append(normalized)
    return result


def _distance(left_x: float, left_y: float, right_x: float, right_y: float) -> float:
    return hypot(left_x - right_x, left_y - right_y)
