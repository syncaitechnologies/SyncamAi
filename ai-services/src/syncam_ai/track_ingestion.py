"""Bounded, metadata-only sampling before camera-local zone evaluation.

This boundary deliberately accepts tracker points, not frames.  Detector
inference, pixels, outbound event delivery, tenant quotas, and network
backpressure remain separate delivery slices.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import Final
from uuid import UUID

from .zone_rules import TrackObservation
from .zone_runtime import ZoneRuntime


MIN_SAMPLE_FPS: Final = 5
MAX_SAMPLE_FPS: Final = 10
MAX_TRACKS_PER_FRAME: Final = 256


@dataclass(frozen=True, slots=True)
class TrackFrame:
    """One timestamped, camera-local batch of tracker metadata only."""

    tenant_id: str
    site_id: str
    camera_id: str
    observed_at: datetime
    tracks: tuple[TrackObservation, ...]


@dataclass(frozen=True, slots=True)
class TrackIngressMetrics:
    """Safe local counters; they intentionally contain no track or image data."""

    accepted_frames: int
    sampled_frames: int
    suppressed_frames: int
    emitted_events: int


class SampledTrackIngress:
    """Rate-limit timestamp-ordered tracker batches before zone evaluation.

    The caller owns detector and tracker execution.  This class is deliberately
    camera-local and deterministic: an accepted batch is either sampled in full
    or suppressed in full, never partially sampled.
    """

    def __init__(self, runtime: ZoneRuntime, sample_fps: int = MIN_SAMPLE_FPS) -> None:
        if not isinstance(runtime, ZoneRuntime):
            raise ValueError("runtime must be a ZoneRuntime")
        if not isinstance(sample_fps, int) or isinstance(sample_fps, bool):
            raise ValueError("sample_fps must be an integer")
        if not MIN_SAMPLE_FPS <= sample_fps <= MAX_SAMPLE_FPS:
            raise ValueError("sample_fps must be between 5 and 10")
        self._runtime = runtime
        self._sample_interval = timedelta(seconds=1 / sample_fps)
        self._last_seen: dict[tuple[str, str, str], datetime] = {}
        self._last_sampled: dict[tuple[str, str, str], datetime] = {}
        self._accepted_frames = 0
        self._sampled_frames = 0
        self._suppressed_frames = 0
        self._emitted_events = 0

    @property
    def metrics(self) -> TrackIngressMetrics:
        return TrackIngressMetrics(
            accepted_frames=self._accepted_frames,
            sampled_frames=self._sampled_frames,
            suppressed_frames=self._suppressed_frames,
            emitted_events=self._emitted_events,
        )

    def ingest(self, frame: TrackFrame) -> list[dict[str, object]]:
        """Validate and sample one full batch, returning only safe rule events."""

        key, observed_at = _validate_frame(frame)
        previous_seen = self._last_seen.get(key)
        if previous_seen is not None and observed_at < previous_seen:
            raise ValueError("track frames must be timestamp ordered per camera")
        self._last_seen[key] = observed_at
        self._accepted_frames += 1

        previous_sampled = self._last_sampled.get(key)
        if previous_sampled is not None and observed_at - previous_sampled < self._sample_interval:
            self._suppressed_frames += 1
            return []

        events: list[dict[str, object]] = []
        for track in frame.tracks:
            events.extend(self._runtime.observe(track))
        self._last_sampled[key] = observed_at
        self._sampled_frames += 1
        self._emitted_events += len(events)
        return events


def _validate_frame(frame: TrackFrame) -> tuple[tuple[str, str, str], datetime]:
    if not isinstance(frame, TrackFrame):
        raise ValueError("frame must be a TrackFrame")
    tenant_id = _uuid(frame.tenant_id, "tenant_id")
    site_id = _uuid(frame.site_id, "site_id")
    camera_id = _uuid(frame.camera_id, "camera_id")
    observed_at = _timestamp(frame.observed_at)
    if not isinstance(frame.tracks, tuple) or len(frame.tracks) > MAX_TRACKS_PER_FRAME:
        raise ValueError("tracks must be a tuple within the configured frame bound")

    track_ids: set[int] = set()
    for track in frame.tracks:
        if not isinstance(track, TrackObservation):
            raise ValueError("tracks must contain TrackObservation values")
        if (track.tenant_id, track.site_id, track.camera_id) != (tenant_id, site_id, camera_id):
            raise ValueError("all tracks must match the frame tenant, site, and camera")
        if _timestamp(track.observed_at) != observed_at:
            raise ValueError("all tracks must have the frame timestamp")
        if track.track_id in track_ids:
            raise ValueError("track IDs must be unique within a frame")
        track_ids.add(track.track_id)
    return (tenant_id, site_id, camera_id), observed_at


def _uuid(value: object, field: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a UUID")
    try:
        return str(UUID(value))
    except ValueError as error:
        raise ValueError(f"{field} must be a UUID") from error


def _timestamp(value: object) -> datetime:
    if not isinstance(value, datetime) or value.tzinfo is None or value.utcoffset() is None:
        raise ValueError("observed_at must be timezone-aware")
    return value.astimezone(timezone.utc)
