"""Build bounded FR-103a vehicle-activity events from single-camera tracks."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from math import isfinite
from typing import Final
from uuid import UUID, uuid5

from .track_ingestion import TrackFrame
from .zone_rules import TrackObservation

OBSERVED_BEHAVIOR: Final = "detected"
VEHICLE_CLASSES: Final = frozenset(
    {"bicycle", "bus", "car", "motorcycle", "truck", "van"}
)
_EVENT_NAMESPACE: Final = UUID("9ab73957-2db1-4ed8-a022-23c2ccb76cb8")
_MAX_TRACK_ID: Final = (1 << 63) - 1
_MAX_EVIDENCE_REFS: Final = 32
MIN_CONFIRMATION_FRAMES: Final = 2
MAX_CONFIRMATION_FRAMES: Final = 10
MAX_CONFIRMATION_TRACKS: Final = 2_048
MAX_CONFIRMATION_MISSED_FRAMES: Final = 30


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


@dataclass(frozen=True, slots=True)
class VehicleActivityMetrics:
    """Payload-free counters for local temporal confirmation."""

    accepted_frames: int
    active_tracks: int
    confirmed_events: int
    ignored_person_observations: int
    expired_tracks: int


@dataclass(frozen=True, slots=True)
class _ConfirmationState:
    first_seen_at: datetime
    subject_class: str
    model_version: str
    minimum_confidence: float
    consecutive_frames: int
    missed_frames: int
    emitted: bool


class VehicleActivityConfirmation:
    """Confirm camera-local vehicle tracks before review-required emission."""

    def __init__(
        self,
        tenant_id: str,
        site_id: str,
        camera_id: str,
        zone_id: str,
        *,
        confirmation_frames: int = 3,
        max_missed_frames: int = 10,
        max_tracks: int = 512,
    ) -> None:
        self._scope = (
            _canonical_uuid(tenant_id, "tenant_id"),
            _canonical_uuid(site_id, "site_id"),
            _canonical_uuid(camera_id, "camera_id"),
        )
        self._zone_id = _canonical_uuid(zone_id, "zone_id")
        if (
            not isinstance(confirmation_frames, int)
            or isinstance(confirmation_frames, bool)
            or not MIN_CONFIRMATION_FRAMES
            <= confirmation_frames
            <= MAX_CONFIRMATION_FRAMES
        ):
            raise ValueError("confirmation_frames must be between 2 and 10")
        if (
            not isinstance(max_missed_frames, int)
            or isinstance(max_missed_frames, bool)
            or not 0
            <= max_missed_frames
            <= MAX_CONFIRMATION_MISSED_FRAMES
        ):
            raise ValueError("max_missed_frames exceeds the confirmation bound")
        if (
            not isinstance(max_tracks, int)
            or isinstance(max_tracks, bool)
            or not 1 <= max_tracks <= MAX_CONFIRMATION_TRACKS
        ):
            raise ValueError("max_tracks exceeds the confirmation bound")
        self._confirmation_frames = confirmation_frames
        self._max_missed_frames = max_missed_frames
        self._max_tracks = max_tracks
        self._last_observed: datetime | None = None
        self._states: dict[int, _ConfirmationState] = {}
        self._accepted_frames = 0
        self._confirmed_events = 0
        self._ignored_person_observations = 0
        self._expired_tracks = 0

    @property
    def metrics(self) -> VehicleActivityMetrics:
        return VehicleActivityMetrics(
            accepted_frames=self._accepted_frames,
            active_tracks=len(self._states),
            confirmed_events=self._confirmed_events,
            ignored_person_observations=self._ignored_person_observations,
            expired_tracks=self._expired_tracks,
        )

    def ingest(self, frame: TrackFrame) -> list[dict[str, object]]:
        """Atomically confirm a sampled metadata frame and emit new events."""

        observed_at, tracks = self._validate_frame(frame)
        if self._last_observed is not None and observed_at <= self._last_observed:
            raise ValueError("vehicle confirmation frames must strictly increase")

        observed_vehicle_ids = {
            track.track_id
            for track in tracks
            if track.subject_class.strip().lower() in VEHICLE_CLASSES
        }
        proposed: dict[int, _ConfirmationState] = {}
        expired = 0
        for track_id, state in self._states.items():
            if track_id in observed_vehicle_ids:
                continue
            missed = state.missed_frames + 1
            if missed > self._max_missed_frames:
                expired += 1
                continue
            proposed[track_id] = _ConfirmationState(
                state.first_seen_at,
                state.subject_class,
                state.model_version,
                state.minimum_confidence,
                0 if not state.emitted else state.consecutive_frames,
                missed,
                state.emitted,
            )

        ignored_people = 0
        event_inputs: list[VehicleTrackObservation] = []
        for track in tracks:
            subject_class = track.subject_class.strip().lower()
            if subject_class == "person":
                ignored_people += 1
                continue
            if subject_class not in VEHICLE_CLASSES:
                raise ValueError("track class is not permitted for vehicle confirmation")
            model_version = track.model_version.strip()
            confidence_value = float(track.confidence)
            current = VehicleTrackObservation(
                tenant_id=self._scope[0],
                site_id=self._scope[1],
                camera_id=self._scope[2],
                zone_id=self._zone_id,
                track_id=track.track_id,
                first_seen_at=observed_at,
                subject_class=subject_class,
                confidence=confidence_value,
                model_version=model_version,
            )
            build_vehicle_activity_event(current)
            previous = self._states.get(track.track_id)
            if previous is not None and (
                previous.subject_class != subject_class
                or previous.model_version != model_version
            ):
                raise ValueError("confirmed track provenance cannot change")
            continuing = previous is not None and previous.missed_frames == 0
            first_seen = previous.first_seen_at if continuing else observed_at
            consecutive = previous.consecutive_frames + 1 if continuing else 1
            confidence = (
                min(previous.minimum_confidence, confidence_value)
                if continuing
                else confidence_value
            )
            emitted = previous.emitted if previous is not None else False
            if consecutive >= self._confirmation_frames and not emitted:
                event_inputs.append(
                    VehicleTrackObservation(
                        tenant_id=self._scope[0],
                        site_id=self._scope[1],
                        camera_id=self._scope[2],
                        zone_id=self._zone_id,
                        track_id=track.track_id,
                        first_seen_at=first_seen,
                        subject_class=subject_class,
                        confidence=confidence,
                        model_version=model_version,
                    )
                )
                emitted = True
            proposed[track.track_id] = _ConfirmationState(
                first_seen,
                subject_class,
                model_version,
                confidence,
                consecutive,
                0,
                emitted,
            )

        if len(proposed) > self._max_tracks:
            raise ValueError("vehicle confirmation track capacity is exhausted")
        events = [build_vehicle_activity_event(value) for value in event_inputs]
        self._last_observed = observed_at
        self._states = proposed
        self._accepted_frames += 1
        self._confirmed_events += len(events)
        self._ignored_person_observations += ignored_people
        self._expired_tracks += expired
        return events

    def _validate_frame(
        self, frame: object
    ) -> tuple[datetime, tuple[TrackObservation, ...]]:
        if not isinstance(frame, TrackFrame):
            raise ValueError("frame must be a TrackFrame")
        scope = (
            _canonical_uuid(frame.tenant_id, "tenant_id"),
            _canonical_uuid(frame.site_id, "site_id"),
            _canonical_uuid(frame.camera_id, "camera_id"),
        )
        _canonical_uuid(frame.device_id, "device_id")
        if scope != self._scope:
            raise ValueError("track frame does not match vehicle confirmation scope")
        if (
            not isinstance(frame.observed_at, datetime)
            or frame.observed_at.tzinfo is None
            or frame.observed_at.utcoffset() is None
        ):
            raise ValueError("observed_at must be timezone-aware")
        if not isinstance(frame.tracks, tuple) or len(frame.tracks) > 256:
            raise ValueError("tracks must be a bounded tuple")
        observed_at = frame.observed_at.astimezone(timezone.utc)
        track_ids: set[int] = set()
        for track in frame.tracks:
            if not isinstance(track, TrackObservation):
                raise ValueError("frame must contain TrackObservation values")
            if (track.tenant_id, track.site_id, track.camera_id) != self._scope:
                raise ValueError("track scope does not match vehicle confirmation")
            if (
                not isinstance(track.track_id, int)
                or isinstance(track.track_id, bool)
                or not 0 <= track.track_id <= _MAX_TRACK_ID
            ):
                raise ValueError("track_id must be a bounded integer")
            if (
                not isinstance(track.observed_at, datetime)
                or track.observed_at.tzinfo is None
                or track.observed_at.utcoffset() is None
            ):
                raise ValueError("track observed_at must be timezone-aware")
            if track.observed_at.astimezone(timezone.utc) != observed_at:
                raise ValueError("track timestamp does not match frame")
            if not isinstance(track.subject_class, str) or track.subject_class.strip().lower() not in VEHICLE_CLASSES | {"person"}:
                raise ValueError("track class is not permitted for vehicle confirmation")
            if (
                isinstance(track.confidence, bool)
                or not isinstance(track.confidence, (int, float))
                or not isfinite(float(track.confidence))
                or not 0 <= float(track.confidence) <= 1
            ):
                raise ValueError("track confidence must be finite and normalized")
            if not isinstance(track.model_version, str) or not 1 <= len(track.model_version.strip()) <= 128:
                raise ValueError("track model_version must be bounded")
            if any(
                isinstance(value, bool)
                or not isinstance(value, (int, float))
                or not isfinite(float(value))
                or not 0 <= float(value) <= 1
                for value in (track.center_x, track.center_y)
            ):
                raise ValueError("track coordinates must be finite and normalized")
            if track.track_id in track_ids:
                raise ValueError("track identifiers must be unique per frame")
            track_ids.add(track.track_id)
        return observed_at, frame.tracks


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
