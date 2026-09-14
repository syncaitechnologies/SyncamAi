"""Synthetic-safe temporal confirmation for future fall metadata.

The boundary consumes already-associated camera-local posture metadata. It
does not load a model, inspect pixels, identify a person, make a medical or
safety conclusion, or trigger an autonomous response. Every emitted event
remains pending human review.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from math import isfinite
import re
from typing import Final
from uuid import UUID, uuid5


POSTURES: Final = frozenset(
    {"upright", "transitioning", "sitting", "lying", "unknown"}
)
MOTION_STATES: Final = frozenset(
    {"stable", "downward", "ambiguous", "unknown"}
)
MINIMUM_TRANSITION_DURATION: Final = timedelta(seconds=1.5)
MAX_OBSERVATION_GAP: Final = timedelta(seconds=1)
MAX_TRACKS_PER_FRAME: Final = 64
MAX_ACTIVE_TRACKS: Final = 256
MAX_EMITTED_RETENTION_FRAMES: Final = 30
_MAX_SEQUENCE: Final = (1 << 63) - 1
_MODEL_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
_EVENT_NAMESPACE: Final = UUID("fb3bfeb3-3ff6-4530-ad99-da4361f768b8")


@dataclass(frozen=True, slots=True)
class FallTrackObservation:
    """One identity-free posture/motion record from a future local source."""

    track_id: int
    posture: str
    motion_state: str
    confidence: float
    model_version: str


@dataclass(frozen=True, slots=True)
class FallTrackFrame:
    """One ordered camera/zone-local metadata batch."""

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    sequence: int
    observed_at: datetime
    tracks: tuple[FallTrackObservation, ...]


@dataclass(frozen=True, slots=True)
class FallReviewMetrics:
    """Payload-free local confirmation counters."""

    accepted_frames: int
    active_tracks: int
    confirmed_events: int
    reset_tracks: int


@dataclass(frozen=True, slots=True)
class _CanonicalTrack:
    track_id: int
    posture: str
    motion_state: str
    confidence: float
    model_version: str


@dataclass(frozen=True, slots=True)
class _TrackState:
    track_id: int
    phase: str
    model_version: str
    transition_started_at: datetime | None
    last_seen_at: datetime
    minimum_confidence: float
    missed_frames: int
    emitted: bool


class FallReviewConfirmation:
    """Confirm an ordered sustained posture transition before review emission."""

    def __init__(
        self,
        tenant_id: str,
        site_id: str,
        camera_id: str,
        zone_id: str,
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
        self._max_tracks = max_tracks
        self._emitted_retention_frames = emitted_retention_frames
        self._last_sequence = 0
        self._last_observed: datetime | None = None
        self._states: dict[int, _TrackState] = {}
        self._accepted_frames = 0
        self._confirmed_events = 0
        self._reset_tracks = 0

    @property
    def metrics(self) -> FallReviewMetrics:
        return FallReviewMetrics(
            self._accepted_frames,
            len(self._states),
            self._confirmed_events,
            self._reset_tracks,
        )

    def confirm(self, frame: FallTrackFrame) -> list[dict[str, object]]:
        """Atomically confirm one metadata frame and return new review events."""

        scope, sequence, observed_at, tracks = _validate_frame(frame)
        if scope != self._scope:
            raise ValueError("fall track frame scope cannot change")
        if sequence <= self._last_sequence:
            raise ValueError("fall track sequence must increase")
        if self._last_observed is not None and observed_at <= self._last_observed:
            raise ValueError("fall track timestamps must strictly increase")

        proposed: dict[int, _TrackState] = {}
        observed_track_ids = {track.track_id for track in tracks}
        resets = 0
        for track_id, state in self._states.items():
            if track_id in observed_track_ids:
                continue
            missed = state.missed_frames + 1
            age = observed_at - state.last_seen_at
            if (
                not state.emitted
                or missed > self._emitted_retention_frames
                or age
                > MAX_OBSERVATION_GAP * (self._emitted_retention_frames + 1)
            ):
                resets += 1
                continue
            proposed[track_id] = _TrackState(
                state.track_id,
                state.phase,
                state.model_version,
                state.transition_started_at,
                state.last_seen_at,
                state.minimum_confidence,
                missed,
                True,
            )

        new_events: list[dict[str, object]] = []
        for track in tracks:
            previous = self._states.get(track.track_id)
            state, event = self._advance(
                track,
                previous,
                observed_at,
            )
            if previous is not None and state is None:
                resets += 1
            elif (
                previous is not None
                and state is not None
                and state.phase == "armed"
                and (
                    previous.phase != "armed"
                    or previous.model_version != state.model_version
                    or observed_at - previous.last_seen_at > MAX_OBSERVATION_GAP
                )
            ):
                resets += 1
            if state is not None:
                proposed[track.track_id] = state
            if event is not None:
                new_events.append(event)

        if len(proposed) > self._max_tracks:
            raise ValueError("fall confirmation capacity is exhausted")
        self._last_sequence = sequence
        self._last_observed = observed_at
        self._states = proposed
        self._accepted_frames += 1
        self._confirmed_events += len(new_events)
        self._reset_tracks += resets
        return new_events

    def _advance(
        self,
        track: _CanonicalTrack,
        previous: _TrackState | None,
        observed_at: datetime,
    ) -> tuple[_TrackState | None, dict[str, object] | None]:
        continuous = (
            previous is not None
            and previous.model_version == track.model_version
            and observed_at - previous.last_seen_at <= MAX_OBSERVATION_GAP
        )

        if track.motion_state in {"ambiguous", "unknown"}:
            return None, None

        if track.posture == "upright" and track.motion_state == "stable":
            return (
                _TrackState(
                    track.track_id,
                    "armed",
                    track.model_version,
                    None,
                    observed_at,
                    track.confidence,
                    0,
                    False,
                ),
                None,
            )

        if (
            track.posture == "transitioning"
            and track.motion_state == "downward"
            and continuous
            and previous is not None
            and previous.phase in {"armed", "descending"}
            and not previous.emitted
        ):
            started_at = (
                observed_at
                if previous.phase == "armed"
                else previous.transition_started_at
            )
            return (
                _TrackState(
                    track.track_id,
                    "descending",
                    track.model_version,
                    started_at,
                    observed_at,
                    min(previous.minimum_confidence, track.confidence),
                    0,
                    False,
                ),
                None,
            )

        if (
            track.posture == "lying"
            and track.motion_state == "stable"
            and continuous
            and previous is not None
            and previous.phase in {"descending", "lying_candidate"}
            and not previous.emitted
            and previous.transition_started_at is not None
        ):
            minimum_confidence = min(
                previous.minimum_confidence,
                track.confidence,
            )
            duration = observed_at - previous.transition_started_at
            emitted = duration >= MINIMUM_TRANSITION_DURATION
            state = _TrackState(
                track.track_id,
                "emitted" if emitted else "lying_candidate",
                track.model_version,
                previous.transition_started_at,
                observed_at,
                minimum_confidence,
                0,
                emitted,
            )
            if emitted:
                return state, _event(
                    self._scope,
                    track.track_id,
                    previous.transition_started_at,
                    observed_at,
                    minimum_confidence,
                    track.model_version,
                )
            return state, None

        if (
            track.posture == "lying"
            and track.motion_state == "stable"
            and continuous
            and previous is not None
            and previous.emitted
        ):
            return (
                _TrackState(
                    track.track_id,
                    "emitted",
                    track.model_version,
                    previous.transition_started_at,
                    observed_at,
                    min(previous.minimum_confidence, track.confidence),
                    0,
                    True,
                ),
                None,
            )

        return None, None


def _validate_frame(
    frame: object,
) -> tuple[
    tuple[str, str, str, str],
    int,
    datetime,
    tuple[_CanonicalTrack, ...],
]:
    if not isinstance(frame, FallTrackFrame):
        raise ValueError("frame must be a FallTrackFrame")
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
    if not isinstance(frame.tracks, tuple) or len(frame.tracks) > MAX_TRACKS_PER_FRAME:
        raise ValueError("tracks must be a tuple within the frame bound")
    tracks = tuple(
        sorted(
            (_validate_track(value) for value in frame.tracks),
            key=lambda value: value.track_id,
        )
    )
    if len({track.track_id for track in tracks}) != len(tracks):
        raise ValueError("track identifiers must be unique per frame")
    return scope, frame.sequence, frame.observed_at.astimezone(timezone.utc), tracks


def _validate_track(value: object) -> _CanonicalTrack:
    if not isinstance(value, FallTrackObservation):
        raise ValueError("tracks must contain FallTrackObservation values")
    if (
        not isinstance(value.track_id, int)
        or isinstance(value.track_id, bool)
        or not 0 <= value.track_id <= _MAX_SEQUENCE
    ):
        raise ValueError("track_id must be a bounded integer")
    posture = value.posture.strip().lower() if isinstance(value.posture, str) else ""
    if posture not in POSTURES:
        raise ValueError("posture is not supported")
    motion_state = (
        value.motion_state.strip().lower()
        if isinstance(value.motion_state, str)
        else ""
    )
    if motion_state not in MOTION_STATES:
        raise ValueError("motion_state is not supported")
    if (
        isinstance(value.confidence, bool)
        or not isinstance(value.confidence, (int, float))
        or not isfinite(float(value.confidence))
        or not 0 <= float(value.confidence) <= 1
    ):
        raise ValueError("fall metadata confidence must be normalized")
    if (
        not isinstance(value.model_version, str)
        or _MODEL_VERSION.fullmatch(value.model_version) is None
    ):
        raise ValueError("model_version must be a bounded opaque identifier")
    return _CanonicalTrack(
        value.track_id,
        posture,
        motion_state,
        float(value.confidence),
        value.model_version,
    )


def _event(
    scope: tuple[str, str, str, str],
    track_id: int,
    transition_started_at: datetime,
    confirmed_at: datetime,
    confidence: float,
    model_version: str,
) -> dict[str, object]:
    source = f"{scope[2]}:{track_id}:{transition_started_at.isoformat()}"
    event_id = str(uuid5(_EVENT_NAMESPACE, f"{scope[0]}:{source}"))
    occurred_at = confirmed_at.isoformat(timespec="microseconds").replace(
        "+00:00", "Z"
    )
    return {
        "event_id": event_id,
        "tenant_id": scope[0],
        "dedupe_key": f"fall_review:{event_id}",
        "occurred_at": occurred_at,
        "site_id": scope[1],
        "camera_id": scope[2],
        "zone_id": scope[3],
        "event_type": "fall_review",
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
