"""Bounded camera-local association from detector metadata to local tracks.

This module does not execute a detector or accept pixels. It gives a future
approved detector a deterministic, identity-free path into the existing
track-ingress and zone-rule runtime.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from math import isfinite
import re
from typing import Final
from uuid import UUID

from .track_ingestion import TrackFrame
from .zone_rules import SUPPORTED_SUBJECT_CLASSES, TrackObservation


MAX_DETECTIONS_PER_FRAME: Final = 256
MAX_ACTIVE_TRACKS: Final = 2_048
MAX_MISSED_FRAMES: Final = 30
_MAX_TRACK_ID: Final = (1 << 63) - 1
_MODEL_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")


@dataclass(frozen=True, slots=True)
class Detection:
    """One normalized non-identity detector result without pixels or crops."""

    subject_class: str
    confidence: float
    left: float
    top: float
    right: float
    bottom: float
    model_version: str


@dataclass(frozen=True, slots=True)
class DetectionFrame:
    """One ordered camera-local batch from a future approved detector."""

    tenant_id: str
    site_id: str
    camera_id: str
    device_id: str
    sequence: int
    observed_at: datetime
    detections: tuple[Detection, ...]


@dataclass(frozen=True, slots=True)
class DetectionTrackingMetrics:
    """Payload-free local association counters."""

    accepted_frames: int
    active_tracks: int
    created_tracks: int
    matched_detections: int
    expired_tracks: int


@dataclass(frozen=True, slots=True)
class _CanonicalDetection:
    subject_class: str
    confidence: float
    box: tuple[float, float, float, float]
    model_version: str


@dataclass(frozen=True, slots=True)
class _TrackState:
    track_id: int
    subject_class: str
    box: tuple[float, float, float, float]
    missed_frames: int


class CameraLocalDetectionTracker:
    """Greedy IoU association with strict scope, ordering, and state bounds."""

    def __init__(
        self,
        *,
        minimum_iou: float = 0.3,
        max_missed_frames: int = 10,
        max_active_tracks: int = 512,
    ) -> None:
        if (
            isinstance(minimum_iou, bool)
            or not isinstance(minimum_iou, (int, float))
            or not isfinite(float(minimum_iou))
            or not 0 < float(minimum_iou) <= 1
        ):
            raise ValueError("minimum_iou must be finite and in (0, 1]")
        if (
            not isinstance(max_missed_frames, int)
            or isinstance(max_missed_frames, bool)
            or not 0 <= max_missed_frames <= MAX_MISSED_FRAMES
        ):
            raise ValueError("max_missed_frames exceeds the local bound")
        if (
            not isinstance(max_active_tracks, int)
            or isinstance(max_active_tracks, bool)
            or not 1 <= max_active_tracks <= MAX_ACTIVE_TRACKS
        ):
            raise ValueError("max_active_tracks exceeds the local bound")
        self._minimum_iou = float(minimum_iou)
        self._max_missed_frames = max_missed_frames
        self._max_active_tracks = max_active_tracks
        self._scope: tuple[str, str, str, str] | None = None
        self._last_sequence = 0
        self._last_observed: datetime | None = None
        self._next_track_id = 1
        self._tracks: dict[int, _TrackState] = {}
        self._accepted_frames = 0
        self._created_tracks = 0
        self._matched_detections = 0
        self._expired_tracks = 0

    @property
    def metrics(self) -> DetectionTrackingMetrics:
        return DetectionTrackingMetrics(
            accepted_frames=self._accepted_frames,
            active_tracks=len(self._tracks),
            created_tracks=self._created_tracks,
            matched_detections=self._matched_detections,
            expired_tracks=self._expired_tracks,
        )

    def track(self, frame: DetectionFrame) -> TrackFrame:
        """Associate a complete frame atomically and emit metadata-only tracks."""

        scope, sequence, observed_at, detections = _validate_frame(frame)
        if self._scope is not None and scope != self._scope:
            raise ValueError("detection frame scope cannot change")
        if sequence <= self._last_sequence:
            raise ValueError("detection frame sequence must increase")
        if self._last_observed is not None and observed_at < self._last_observed:
            raise ValueError("detection frames must be timestamp ordered")

        candidates: list[tuple[float, int, int]] = []
        for track_id, state in self._tracks.items():
            for detection_index, detection in enumerate(detections):
                if state.subject_class != detection.subject_class:
                    continue
                overlap = _intersection_over_union(state.box, detection.box)
                if overlap >= self._minimum_iou:
                    candidates.append((-overlap, track_id, detection_index))
        candidates.sort()

        matched_tracks: set[int] = set()
        matched_detections: set[int] = set()
        associations: dict[int, int] = {}
        for _, track_id, detection_index in candidates:
            if track_id in matched_tracks or detection_index in matched_detections:
                continue
            matched_tracks.add(track_id)
            matched_detections.add(detection_index)
            associations[detection_index] = track_id

        proposed: dict[int, _TrackState] = {}
        expired = 0
        for track_id, state in self._tracks.items():
            if track_id in matched_tracks:
                continue
            missed = state.missed_frames + 1
            if missed > self._max_missed_frames:
                expired += 1
                continue
            proposed[track_id] = _TrackState(
                track_id, state.subject_class, state.box, missed
            )

        next_track_id = self._next_track_id
        created = 0
        for detection_index, detection in enumerate(detections):
            track_id = associations.get(detection_index)
            if track_id is None:
                if next_track_id > _MAX_TRACK_ID:
                    raise ValueError("camera-local track identifier space is exhausted")
                track_id = next_track_id
                next_track_id += 1
                created += 1
                associations[detection_index] = track_id
            proposed[track_id] = _TrackState(
                track_id, detection.subject_class, detection.box, 0
            )

        if len(proposed) > self._max_active_tracks:
            raise ValueError("active camera-local track capacity is exhausted")

        observations = tuple(
            TrackObservation(
                tenant_id=scope[0],
                site_id=scope[1],
                camera_id=scope[2],
                track_id=associations[index],
                observed_at=observed_at,
                center_x=(detection.box[0] + detection.box[2]) / 2,
                center_y=(detection.box[1] + detection.box[3]) / 2,
                subject_class=detection.subject_class,
                confidence=detection.confidence,
                model_version=detection.model_version,
            )
            for index, detection in enumerate(detections)
        )

        self._scope = scope
        self._last_sequence = sequence
        self._last_observed = observed_at
        self._next_track_id = next_track_id
        self._tracks = proposed
        self._accepted_frames += 1
        self._created_tracks += created
        self._matched_detections += len(matched_detections)
        self._expired_tracks += expired
        return TrackFrame(*scope, observed_at, observations)


def _validate_frame(
    frame: object,
) -> tuple[
    tuple[str, str, str, str], int, datetime, tuple[_CanonicalDetection, ...]
]:
    if not isinstance(frame, DetectionFrame):
        raise ValueError("frame must be a DetectionFrame")
    scope = (
        _uuid(frame.tenant_id, "tenant_id"),
        _uuid(frame.site_id, "site_id"),
        _uuid(frame.camera_id, "camera_id"),
        _uuid(frame.device_id, "device_id"),
    )
    if (
        not isinstance(frame.sequence, int)
        or isinstance(frame.sequence, bool)
        or not 1 <= frame.sequence <= _MAX_TRACK_ID
    ):
        raise ValueError("sequence must be a positive bounded integer")
    if (
        not isinstance(frame.observed_at, datetime)
        or frame.observed_at.tzinfo is None
        or frame.observed_at.utcoffset() is None
    ):
        raise ValueError("observed_at must be timezone-aware")
    observed_at = frame.observed_at.astimezone(timezone.utc)
    if (
        not isinstance(frame.detections, tuple)
        or len(frame.detections) > MAX_DETECTIONS_PER_FRAME
    ):
        raise ValueError("detections must be a tuple within the frame bound")
    detections = tuple(
        sorted(
            (_validate_detection(value) for value in frame.detections),
            key=_detection_key,
        )
    )
    return scope, frame.sequence, observed_at, detections


def _validate_detection(value: object) -> _CanonicalDetection:
    if not isinstance(value, Detection):
        raise ValueError("detections must contain Detection values")
    subject_class = value.subject_class.strip().lower() if isinstance(value.subject_class, str) else ""
    if subject_class not in SUPPORTED_SUBJECT_CLASSES:
        raise ValueError("subject_class is not permitted for local tracking")
    if (
        isinstance(value.confidence, bool)
        or not isinstance(value.confidence, (int, float))
        or not isfinite(float(value.confidence))
        or not 0 <= float(value.confidence) <= 1
    ):
        raise ValueError("confidence must be finite and normalized")
    coordinates = (value.left, value.top, value.right, value.bottom)
    if any(
        isinstance(item, bool)
        or not isinstance(item, (int, float))
        or not isfinite(float(item))
        for item in coordinates
    ):
        raise ValueError("detection box must contain finite numbers")
    box = tuple(float(item) for item in coordinates)
    if not all(0 <= item <= 1 for item in box) or box[0] >= box[2] or box[1] >= box[3]:
        raise ValueError("detection box must be positive and normalized")
    if not isinstance(value.model_version, str) or _MODEL_VERSION.fullmatch(value.model_version) is None:
        raise ValueError("model_version must be a bounded opaque identifier")
    return _CanonicalDetection(subject_class, float(value.confidence), box, value.model_version)


def _detection_key(value: _CanonicalDetection) -> tuple[object, ...]:
    return (value.subject_class, *value.box, -value.confidence, value.model_version)


def _intersection_over_union(
    left: tuple[float, float, float, float],
    right: tuple[float, float, float, float],
) -> float:
    intersection_width = max(0.0, min(left[2], right[2]) - max(left[0], right[0]))
    intersection_height = max(0.0, min(left[3], right[3]) - max(left[1], right[1]))
    intersection = intersection_width * intersection_height
    left_area = (left[2] - left[0]) * (left[3] - left[1])
    right_area = (right[2] - right[0]) * (right[3] - right[1])
    union = left_area + right_area - intersection
    return 0.0 if union <= 0 else intersection / union


def _uuid(value: object, field: str) -> str:
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a UUID")
    try:
        return str(UUID(value))
    except ValueError as error:
        raise ValueError(f"{field} must be a UUID") from error
