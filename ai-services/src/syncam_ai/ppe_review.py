"""Synthetic-safe temporal confirmation for future PPE metadata.

The boundary consumes already-associated camera-local track metadata. It does
not load a detector, process pixels, identify a person, or make a safety or
compliance decision. Every emitted event remains pending human review.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from math import isfinite
import re
from typing import Final
from uuid import UUID, uuid5


PPE_ITEMS: Final = frozenset(
    {"helmet", "vest", "mask", "gloves", "glasses", "boots"}
)
PPE_STATES: Final = frozenset({"present", "absent", "unknown"})
CONFIRMATION_FRAMES: Final = 3
MAX_TRACKS_PER_FRAME: Final = 64
MAX_ACTIVE_TRACKS: Final = 256
MAX_EMITTED_RETENTION_FRAMES: Final = 30
MAX_CONFIRMATION_GAP: Final = timedelta(seconds=1)
_MAX_SEQUENCE: Final = (1 << 63) - 1
_MODEL_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
_EVENT_NAMESPACE: Final = UUID("c9af022f-f45b-45d0-99e4-c7dd60a13f52")


@dataclass(frozen=True, slots=True)
class PPEItemObservation:
    """One bounded PPE item state produced by a future local classifier."""

    item: str
    state: str
    confidence: float


@dataclass(frozen=True, slots=True)
class PPETrackObservation:
    """Already-associated PPE metadata for one camera-local track."""

    track_id: int
    model_version: str
    items: tuple[PPEItemObservation, ...]


@dataclass(frozen=True, slots=True)
class PPETrackFrame:
    """One ordered camera/zone-local metadata batch."""

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    sequence: int
    observed_at: datetime
    tracks: tuple[PPETrackObservation, ...]


@dataclass(frozen=True, slots=True)
class PPEReviewMetrics:
    """Payload-free local confirmation counters."""

    accepted_frames: int
    active_tracks: int
    confirmed_events: int
    expired_tracks: int


@dataclass(frozen=True, slots=True)
class _CanonicalTrack:
    track_id: int
    model_version: str
    items: tuple[tuple[str, str, float], ...]


@dataclass(frozen=True, slots=True)
class _TrackState:
    track_id: int
    missing_items: frozenset[str]
    model_version: str
    first_seen_at: datetime
    last_seen_at: datetime
    minimum_confidence: float
    consecutive_frames: int
    missed_frames: int
    emitted: bool


class PPEReviewConfirmation:
    """Confirm repeated absent-item metadata before review-only emission."""

    def __init__(
        self,
        tenant_id: str,
        site_id: str,
        camera_id: str,
        zone_id: str,
        required_items: frozenset[str],
        *,
        max_tracks: int = 128,
        emitted_retention_frames: int = 10,
    ) -> None:
        self._scope = tuple(
            _uuid(value, field)
            for value, field in (
                (tenant_id, "tenant_id"),
                (site_id, "site_id"),
                (camera_id, "camera_id"),
                (zone_id, "zone_id"),
            )
        )
        if not isinstance(required_items, frozenset) or not required_items:
            raise ValueError("required_items must be a non-empty frozenset")
        canonical_required = frozenset(
            item.strip().lower() if isinstance(item, str) else ""
            for item in required_items
        )
        if canonical_required != required_items or not canonical_required <= PPE_ITEMS:
            raise ValueError("required_items contains an unsupported PPE item")
        if (
            not isinstance(max_tracks, int)
            or isinstance(max_tracks, bool)
            or not 1 <= max_tracks <= MAX_ACTIVE_TRACKS
        ):
            raise ValueError("max_tracks exceeds the confirmation bound")
        if (
            not isinstance(emitted_retention_frames, int)
            or isinstance(emitted_retention_frames, bool)
            or not 1
            <= emitted_retention_frames
            <= MAX_EMITTED_RETENTION_FRAMES
        ):
            raise ValueError("emitted retention exceeds the confirmation bound")
        self._required_items = canonical_required
        self._max_tracks = max_tracks
        self._emitted_retention_frames = emitted_retention_frames
        self._last_sequence = 0
        self._last_observed: datetime | None = None
        self._states: dict[int, _TrackState] = {}
        self._accepted_frames = 0
        self._confirmed_events = 0
        self._expired_tracks = 0

    @property
    def metrics(self) -> PPEReviewMetrics:
        return PPEReviewMetrics(
            self._accepted_frames,
            len(self._states),
            self._confirmed_events,
            self._expired_tracks,
        )

    def confirm(self, frame: PPETrackFrame) -> list[dict[str, object]]:
        """Atomically confirm one metadata frame and return new review events."""

        scope, sequence, observed_at, tracks = _validate_frame(frame)
        if scope != self._scope:
            raise ValueError("PPE track frame scope cannot change")
        if sequence <= self._last_sequence:
            raise ValueError("PPE track sequence must increase")
        if self._last_observed is not None and observed_at <= self._last_observed:
            raise ValueError("PPE track timestamps must strictly increase")

        proposed: dict[int, _TrackState] = {}
        observed_track_ids = {track.track_id for track in tracks}
        expired = 0
        for track_id, state in self._states.items():
            if track_id in observed_track_ids:
                continue
            missed = state.missed_frames + 1
            age = observed_at - state.last_seen_at
            if (
                not state.emitted
                or missed > self._emitted_retention_frames
                or age
                > MAX_CONFIRMATION_GAP * (self._emitted_retention_frames + 1)
            ):
                expired += 1
                continue
            proposed[track_id] = _TrackState(
                state.track_id,
                state.missing_items,
                state.model_version,
                state.first_seen_at,
                state.last_seen_at,
                state.minimum_confidence,
                state.consecutive_frames,
                missed,
                True,
            )

        new_events: list[dict[str, object]] = []
        for track in tracks:
            assessment = _potential_absence(track, self._required_items)
            previous = self._states.get(track.track_id)
            if assessment is None:
                if previous is not None:
                    expired += 1
                continue
            missing_items, confidence = assessment
            if (
                previous is not None
                and previous.missing_items == missing_items
                and previous.model_version == track.model_version
                and observed_at - previous.last_seen_at <= MAX_CONFIRMATION_GAP
            ):
                first_seen_at = previous.first_seen_at
                minimum_confidence = min(previous.minimum_confidence, confidence)
                consecutive_frames = previous.consecutive_frames + 1
                emitted = previous.emitted
            else:
                if previous is not None:
                    expired += 1
                first_seen_at = observed_at
                minimum_confidence = confidence
                consecutive_frames = 1
                emitted = False
            if consecutive_frames >= CONFIRMATION_FRAMES and not emitted:
                new_events.append(
                    _event(
                        self._scope,
                        track.track_id,
                        missing_items,
                        first_seen_at,
                        observed_at,
                        minimum_confidence,
                        track.model_version,
                    )
                )
                emitted = True
            proposed[track.track_id] = _TrackState(
                track.track_id,
                missing_items,
                track.model_version,
                first_seen_at,
                observed_at,
                minimum_confidence,
                consecutive_frames,
                0,
                emitted,
            )

        if len(proposed) > self._max_tracks:
            raise ValueError("PPE confirmation capacity is exhausted")
        self._last_sequence = sequence
        self._last_observed = observed_at
        self._states = proposed
        self._accepted_frames += 1
        self._confirmed_events += len(new_events)
        self._expired_tracks += expired
        return new_events


def _validate_frame(
    frame: object,
) -> tuple[
    tuple[str, str, str, str],
    int,
    datetime,
    tuple[_CanonicalTrack, ...],
]:
    if not isinstance(frame, PPETrackFrame):
        raise ValueError("frame must be a PPETrackFrame")
    scope = tuple(
        _uuid(value, field)
        for value, field in (
            (frame.tenant_id, "tenant_id"),
            (frame.site_id, "site_id"),
            (frame.camera_id, "camera_id"),
            (frame.zone_id, "zone_id"),
        )
    )
    if (
        not isinstance(frame.sequence, int)
        or isinstance(frame.sequence, bool)
        or not 1 <= frame.sequence <= _MAX_SEQUENCE
    ):
        raise ValueError("sequence must be a positive bounded integer")
    if (
        not isinstance(frame.observed_at, datetime)
        or frame.observed_at.tzinfo is None
        or frame.observed_at.utcoffset() is None
    ):
        raise ValueError("observed_at must be timezone-aware")
    if (
        not isinstance(frame.tracks, tuple)
        or len(frame.tracks) > MAX_TRACKS_PER_FRAME
    ):
        raise ValueError("tracks must be a tuple within the frame bound")
    tracks = tuple(sorted((_validate_track(value) for value in frame.tracks), key=lambda value: value.track_id))
    if len({track.track_id for track in tracks}) != len(tracks):
        raise ValueError("track identifiers must be unique per frame")
    return scope, frame.sequence, frame.observed_at.astimezone(timezone.utc), tracks


def _validate_track(value: object) -> _CanonicalTrack:
    if not isinstance(value, PPETrackObservation):
        raise ValueError("tracks must contain PPETrackObservation values")
    if (
        not isinstance(value.track_id, int)
        or isinstance(value.track_id, bool)
        or not 0 <= value.track_id <= _MAX_SEQUENCE
    ):
        raise ValueError("track_id must be a bounded integer")
    if (
        not isinstance(value.model_version, str)
        or _MODEL_VERSION.fullmatch(value.model_version) is None
    ):
        raise ValueError("model_version must be a bounded opaque identifier")
    if not isinstance(value.items, tuple) or len(value.items) > len(PPE_ITEMS):
        raise ValueError("PPE items must be a bounded tuple")
    canonical_items: list[tuple[str, str, float]] = []
    seen_items: set[str] = set()
    for observation in value.items:
        if not isinstance(observation, PPEItemObservation):
            raise ValueError("items must contain PPEItemObservation values")
        item = observation.item.strip().lower() if isinstance(observation.item, str) else ""
        state = observation.state.strip().lower() if isinstance(observation.state, str) else ""
        if item not in PPE_ITEMS or item in seen_items:
            raise ValueError("PPE item must be unique and supported")
        if state not in PPE_STATES:
            raise ValueError("PPE item state is not supported")
        if (
            isinstance(observation.confidence, bool)
            or not isinstance(observation.confidence, (int, float))
            or not isfinite(float(observation.confidence))
            or not 0 <= float(observation.confidence) <= 1
        ):
            raise ValueError("PPE item confidence must be normalized")
        seen_items.add(item)
        canonical_items.append((item, state, float(observation.confidence)))
    canonical_items.sort()
    return _CanonicalTrack(value.track_id, value.model_version, tuple(canonical_items))


def _potential_absence(
    track: _CanonicalTrack,
    required_items: frozenset[str],
) -> tuple[frozenset[str], float] | None:
    observations = {item: (state, confidence) for item, state, confidence in track.items}
    if any(
        item not in observations or observations[item][0] == "unknown"
        for item in required_items
    ):
        return None
    missing_items = frozenset(
        item for item in required_items if observations[item][0] == "absent"
    )
    if not missing_items:
        return None
    return missing_items, min(observations[item][1] for item in missing_items)


def _event(
    scope: tuple[str, str, str, str],
    track_id: int,
    missing_items: frozenset[str],
    first_seen_at: datetime,
    confirmed_at: datetime,
    confidence: float,
    model_version: str,
) -> dict[str, object]:
    source = ":".join(
        (
            scope[2],
            str(track_id),
            first_seen_at.isoformat(),
            ",".join(sorted(missing_items)),
        )
    )
    event_id = str(uuid5(_EVENT_NAMESPACE, f"{scope[0]}:{source}"))
    occurred_at = confirmed_at.isoformat(timespec="microseconds").replace(
        "+00:00", "Z"
    )
    return {
        "event_id": event_id,
        "tenant_id": scope[0],
        "dedupe_key": f"ppe_review:{event_id}",
        "occurred_at": occurred_at,
        "site_id": scope[1],
        "camera_id": scope[2],
        "zone_id": scope[3],
        "event_type": "ppe_review",
        "model_version": model_version,
        "confidence": confidence,
        "evidence_refs": [],
        "requires_human_review": True,
        "review_state": "pending",
    }


def _uuid(value: object, field: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a UUID")
    try:
        return str(UUID(value))
    except ValueError as error:
        raise ValueError(f"{field} must be a UUID") from error
