"""Synthetic-safe temporal confirmation for future fight metadata.

The boundary consumes already-associated camera-local motion-cluster metadata.
It does not load a model, inspect pixels, identify a person, make a safety
conclusion, or trigger an autonomous response. Every emitted event remains
pending human review.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from math import isfinite
import re
from typing import Final
from uuid import UUID, uuid5


MOTION_STATES: Final = frozenset(
    {"aggressive", "ordinary", "hugging", "jostling", "unknown"}
)
MINIMUM_CLUSTER_DURATION: Final = timedelta(seconds=1)
MAX_OBSERVATION_GAP: Final = timedelta(seconds=1)
MAX_TRACKS_PER_FRAME: Final = 64
MAX_ACTIVE_CLUSTERS: Final = 256
MAX_EMITTED_RETENTION_FRAMES: Final = 30
_MAX_SEQUENCE: Final = (1 << 63) - 1
_MODEL_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
_EVENT_NAMESPACE: Final = UUID("a64053a6-e346-4338-b7be-f4cfb2610faf")


@dataclass(frozen=True, slots=True)
class FightTrackObservation:
    """One identity-free motion record from a future camera-local source."""

    track_id: int
    cluster_id: int
    motion_state: str
    confidence: float
    model_version: str


@dataclass(frozen=True, slots=True)
class FightTrackFrame:
    """One ordered camera/zone-local metadata batch."""

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    sequence: int
    observed_at: datetime
    tracks: tuple[FightTrackObservation, ...]


@dataclass(frozen=True, slots=True)
class FightReviewMetrics:
    """Payload-free local confirmation counters."""

    accepted_frames: int
    active_clusters: int
    confirmed_events: int
    reset_clusters: int


@dataclass(frozen=True, slots=True)
class _CanonicalTrack:
    track_id: int
    cluster_id: int
    motion_state: str
    confidence: float
    model_version: str


@dataclass(frozen=True, slots=True)
class _CanonicalCluster:
    cluster_id: int
    track_ids: tuple[int, ...]
    minimum_confidence: float
    model_version: str
    qualifies: bool


@dataclass(frozen=True, slots=True)
class _ClusterState:
    cluster_id: int
    track_ids: tuple[int, ...]
    model_version: str
    started_at: datetime
    last_seen_at: datetime
    minimum_confidence: float
    missed_frames: int
    emitted: bool


class FightReviewConfirmation:
    """Require a sustained unambiguous multi-track cluster before review."""

    def __init__(
        self,
        tenant_id: str,
        site_id: str,
        camera_id: str,
        zone_id: str,
        *,
        max_clusters: int = 128,
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
            not isinstance(max_clusters, int)
            or isinstance(max_clusters, bool)
            or not 1 <= max_clusters <= MAX_ACTIVE_CLUSTERS
        ):
            raise ValueError("max_clusters exceeds the confirmation bound")
        if (
            not isinstance(emitted_retention_frames, int)
            or isinstance(emitted_retention_frames, bool)
            or not 1
            <= emitted_retention_frames
            <= MAX_EMITTED_RETENTION_FRAMES
        ):
            raise ValueError("emitted retention exceeds the confirmation bound")
        self._max_clusters = max_clusters
        self._emitted_retention_frames = emitted_retention_frames
        self._last_sequence = 0
        self._last_observed: datetime | None = None
        self._states: dict[int, _ClusterState] = {}
        self._accepted_frames = 0
        self._confirmed_events = 0
        self._reset_clusters = 0

    @property
    def metrics(self) -> FightReviewMetrics:
        return FightReviewMetrics(
            self._accepted_frames,
            len(self._states),
            self._confirmed_events,
            self._reset_clusters,
        )

    def confirm(self, frame: FightTrackFrame) -> list[dict[str, object]]:
        """Atomically confirm one metadata frame and return new review events."""

        scope, sequence, observed_at, clusters = _validate_frame(frame)
        if scope != self._scope:
            raise ValueError("fight track frame scope cannot change")
        if sequence <= self._last_sequence:
            raise ValueError("fight track sequence must increase")
        if self._last_observed is not None and observed_at <= self._last_observed:
            raise ValueError("fight track timestamps must strictly increase")

        observed_cluster_ids = {cluster.cluster_id for cluster in clusters}
        proposed: dict[int, _ClusterState] = {}
        resets = 0
        for cluster_id, state in self._states.items():
            if cluster_id in observed_cluster_ids:
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
            proposed[cluster_id] = _ClusterState(
                state.cluster_id,
                state.track_ids,
                state.model_version,
                state.started_at,
                state.last_seen_at,
                state.minimum_confidence,
                missed,
                True,
            )

        events: list[dict[str, object]] = []
        for cluster in clusters:
            previous = self._states.get(cluster.cluster_id)
            if not cluster.qualifies:
                if previous is not None:
                    resets += 1
                continue

            continuous = (
                previous is not None
                and previous.track_ids == cluster.track_ids
                and previous.model_version == cluster.model_version
                and observed_at - previous.last_seen_at <= MAX_OBSERVATION_GAP
            )
            if not continuous:
                if previous is not None:
                    resets += 1
                state = _ClusterState(
                    cluster.cluster_id,
                    cluster.track_ids,
                    cluster.model_version,
                    observed_at,
                    observed_at,
                    cluster.minimum_confidence,
                    0,
                    False,
                )
            else:
                assert previous is not None
                minimum_confidence = min(
                    previous.minimum_confidence,
                    cluster.minimum_confidence,
                )
                emitted = previous.emitted
                if (
                    not emitted
                    and observed_at - previous.started_at >= MINIMUM_CLUSTER_DURATION
                ):
                    events.append(
                        _event(
                            self._scope,
                            cluster.cluster_id,
                            previous.started_at,
                            observed_at,
                            minimum_confidence,
                            cluster.model_version,
                        )
                    )
                    emitted = True
                state = _ClusterState(
                    cluster.cluster_id,
                    cluster.track_ids,
                    cluster.model_version,
                    previous.started_at,
                    observed_at,
                    minimum_confidence,
                    0,
                    emitted,
                )
            proposed[cluster.cluster_id] = state

        if len(proposed) > self._max_clusters:
            raise ValueError("fight confirmation capacity is exhausted")
        self._last_sequence = sequence
        self._last_observed = observed_at
        self._states = proposed
        self._accepted_frames += 1
        self._confirmed_events += len(events)
        self._reset_clusters += resets
        return events


def _validate_frame(
    frame: object,
) -> tuple[
    tuple[str, str, str, str],
    int,
    datetime,
    tuple[_CanonicalCluster, ...],
]:
    if not isinstance(frame, FightTrackFrame):
        raise ValueError("frame must be a FightTrackFrame")
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
            key=lambda value: (value.cluster_id, value.track_id),
        )
    )
    if len({track.track_id for track in tracks}) != len(tracks):
        raise ValueError("track identifiers must be unique per frame")
    return (
        scope,
        frame.sequence,
        frame.observed_at.astimezone(timezone.utc),
        _clusters(tracks),
    )


def _validate_track(value: object) -> _CanonicalTrack:
    if not isinstance(value, FightTrackObservation):
        raise ValueError("tracks must contain FightTrackObservation values")
    if (
        not isinstance(value.track_id, int)
        or isinstance(value.track_id, bool)
        or not 0 <= value.track_id <= _MAX_SEQUENCE
    ):
        raise ValueError("track_id must be a bounded integer")
    if (
        not isinstance(value.cluster_id, int)
        or isinstance(value.cluster_id, bool)
        or not 0 <= value.cluster_id <= _MAX_SEQUENCE
    ):
        raise ValueError("cluster_id must be a bounded integer")
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
        raise ValueError("fight metadata confidence must be normalized")
    if (
        not isinstance(value.model_version, str)
        or _MODEL_VERSION.fullmatch(value.model_version) is None
    ):
        raise ValueError("model_version must be a bounded opaque identifier")
    return _CanonicalTrack(
        value.track_id,
        value.cluster_id,
        motion_state,
        float(value.confidence),
        value.model_version,
    )


def _clusters(tracks: tuple[_CanonicalTrack, ...]) -> tuple[_CanonicalCluster, ...]:
    grouped: dict[int, list[_CanonicalTrack]] = {}
    for track in tracks:
        grouped.setdefault(track.cluster_id, []).append(track)

    clusters: list[_CanonicalCluster] = []
    for cluster_id, members in sorted(grouped.items()):
        versions = {member.model_version for member in members}
        qualifies = (
            len(members) >= 2
            and all(member.motion_state == "aggressive" for member in members)
            and len(versions) == 1
        )
        clusters.append(
            _CanonicalCluster(
                cluster_id,
                tuple(member.track_id for member in members),
                min(member.confidence for member in members),
                next(iter(versions)) if len(versions) == 1 else "",
                qualifies,
            )
        )
    return tuple(clusters)


def _event(
    scope: tuple[str, str, str, str],
    cluster_id: int,
    started_at: datetime,
    confirmed_at: datetime,
    confidence: float,
    model_version: str,
) -> dict[str, object]:
    source = f"{':'.join(scope)}:{cluster_id}:{started_at.isoformat()}"
    event_id = str(uuid5(_EVENT_NAMESPACE, source))
    occurred_at = confirmed_at.isoformat(timespec="microseconds").replace(
        "+00:00", "Z"
    )
    return {
        "event_id": event_id,
        "tenant_id": scope[0],
        "dedupe_key": f"fight_review:{event_id}",
        "occurred_at": occurred_at,
        "site_id": scope[1],
        "camera_id": scope[2],
        "zone_id": scope[3],
        "event_type": "fight_review",
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
