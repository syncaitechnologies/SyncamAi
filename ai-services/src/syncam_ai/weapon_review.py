"""Synthetic-safe temporal boundary for future weapon detector metadata.

No detector, image, crop, identity, or autonomous action enters this module.
It confirms bounded knife/firearm candidates and emits review-required events.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from math import isfinite
import re
from typing import Final
from uuid import UUID, uuid5


WEAPON_CLASSES: Final = frozenset({"knife", "firearm"})
CONFIRMATION_FRAMES: Final = 3
MAX_CANDIDATES_PER_FRAME: Final = 64
MAX_ACTIVE_CANDIDATES: Final = 256
MAX_EMITTED_RETENTION_FRAMES: Final = 30
MAX_CONFIRMATION_GAP: Final = timedelta(seconds=1)
_MAX_SEQUENCE: Final = (1 << 63) - 1
_MODEL_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
_EVENT_NAMESPACE: Final = UUID("fa8b1378-ae25-4074-a80d-a17e767147f1")


@dataclass(frozen=True, slots=True)
class WeaponCandidate:
    """One normalized future-detector result without pixels or identity."""

    object_class: str
    confidence: float
    left: float
    top: float
    right: float
    bottom: float
    model_version: str


@dataclass(frozen=True, slots=True)
class WeaponCandidateFrame:
    """One ordered camera/zone-local metadata batch."""

    tenant_id: str
    site_id: str
    camera_id: str
    zone_id: str
    sequence: int
    observed_at: datetime
    candidates: tuple[WeaponCandidate, ...]


@dataclass(frozen=True, slots=True)
class WeaponReviewMetrics:
    """Payload-free confirmation counters."""

    accepted_frames: int
    active_candidates: int
    confirmed_events: int
    expired_candidates: int


@dataclass(frozen=True, slots=True)
class _CanonicalCandidate:
    object_class: str
    confidence: float
    box: tuple[float, float, float, float]
    model_version: str


@dataclass(frozen=True, slots=True)
class _CandidateState:
    candidate_id: int
    object_class: str
    box: tuple[float, float, float, float]
    model_version: str
    first_seen_at: datetime
    last_seen_at: datetime
    minimum_confidence: float
    consecutive_frames: int
    missed_frames: int
    emitted: bool


class WeaponReviewConfirmation:
    """Require three class-consistent local observations before review."""

    def __init__(
        self,
        tenant_id: str,
        site_id: str,
        camera_id: str,
        zone_id: str,
        *,
        minimum_iou: float = 0.2,
        max_candidates: int = 128,
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
            isinstance(minimum_iou, bool)
            or not isinstance(minimum_iou, (int, float))
            or not isfinite(float(minimum_iou))
            or not 0 < float(minimum_iou) <= 1
        ):
            raise ValueError("minimum_iou must be finite and in (0, 1]")
        if (
            not isinstance(max_candidates, int)
            or isinstance(max_candidates, bool)
            or not 1 <= max_candidates <= MAX_ACTIVE_CANDIDATES
        ):
            raise ValueError("max_candidates exceeds the confirmation bound")
        if (
            not isinstance(emitted_retention_frames, int)
            or isinstance(emitted_retention_frames, bool)
            or not 1
            <= emitted_retention_frames
            <= MAX_EMITTED_RETENTION_FRAMES
        ):
            raise ValueError("emitted retention exceeds the confirmation bound")
        self._minimum_iou = float(minimum_iou)
        self._max_candidates = max_candidates
        self._emitted_retention_frames = emitted_retention_frames
        self._last_sequence = 0
        self._last_observed: datetime | None = None
        self._next_candidate_id = 1
        self._states: dict[int, _CandidateState] = {}
        self._accepted_frames = 0
        self._confirmed_events = 0
        self._expired_candidates = 0

    @property
    def metrics(self) -> WeaponReviewMetrics:
        return WeaponReviewMetrics(
            self._accepted_frames,
            len(self._states),
            self._confirmed_events,
            self._expired_candidates,
        )

    def confirm(self, frame: WeaponCandidateFrame) -> list[dict[str, object]]:
        """Atomically confirm one frame and return only new review events."""

        scope, sequence, observed_at, candidates = _validate_frame(frame)
        if scope != self._scope:
            raise ValueError("weapon candidate frame scope cannot change")
        if sequence <= self._last_sequence:
            raise ValueError("weapon candidate sequence must increase")
        if self._last_observed is not None and observed_at <= self._last_observed:
            raise ValueError("weapon candidate timestamps must strictly increase")

        match_candidates: list[tuple[float, int, int]] = []
        for candidate_id, state in self._states.items():
            age = observed_at - state.last_seen_at
            eligible_age = (
                MAX_CONFIRMATION_GAP
                if not state.emitted
                else MAX_CONFIRMATION_GAP * (self._emitted_retention_frames + 1)
            )
            if age > eligible_age:
                continue
            for index, candidate in enumerate(candidates):
                if (
                    state.object_class != candidate.object_class
                    or state.model_version != candidate.model_version
                ):
                    continue
                overlap = _iou(state.box, candidate.box)
                if overlap >= self._minimum_iou:
                    match_candidates.append((-overlap, candidate_id, index))
        match_candidates.sort()

        matched_states: set[int] = set()
        matched_inputs: set[int] = set()
        associations: dict[int, int] = {}
        for _, candidate_id, index in match_candidates:
            if candidate_id in matched_states or index in matched_inputs:
                continue
            matched_states.add(candidate_id)
            matched_inputs.add(index)
            associations[index] = candidate_id

        proposed: dict[int, _CandidateState] = {}
        expired = 0
        for candidate_id, state in self._states.items():
            if candidate_id in matched_states:
                continue
            missed = state.missed_frames + 1
            age = observed_at - state.last_seen_at
            if (
                not state.emitted
                or missed > self._emitted_retention_frames
                or age > MAX_CONFIRMATION_GAP * (self._emitted_retention_frames + 1)
            ):
                expired += 1
                continue
            proposed[candidate_id] = _CandidateState(
                state.candidate_id,
                state.object_class,
                state.box,
                state.model_version,
                state.first_seen_at,
                state.last_seen_at,
                state.minimum_confidence,
                state.consecutive_frames,
                missed,
                True,
            )

        next_candidate_id = self._next_candidate_id
        new_events: list[dict[str, object]] = []
        for index, candidate in enumerate(candidates):
            candidate_id = associations.get(index)
            previous = self._states.get(candidate_id) if candidate_id is not None else None
            if previous is None:
                if next_candidate_id > _MAX_SEQUENCE:
                    raise ValueError("weapon candidate identifier space is exhausted")
                candidate_id = next_candidate_id
                next_candidate_id += 1
                first_seen = observed_at
                consecutive = 1
                confidence = candidate.confidence
                emitted = False
            else:
                first_seen = previous.first_seen_at
                consecutive = previous.consecutive_frames + 1
                confidence = min(previous.minimum_confidence, candidate.confidence)
                emitted = previous.emitted
            if consecutive >= CONFIRMATION_FRAMES and not emitted:
                new_events.append(
                    _event(
                        self._scope,
                        candidate_id,
                        first_seen,
                        observed_at,
                        confidence,
                        candidate.model_version,
                    )
                )
                emitted = True
            proposed[candidate_id] = _CandidateState(
                candidate_id,
                candidate.object_class,
                candidate.box,
                candidate.model_version,
                first_seen,
                observed_at,
                confidence,
                consecutive,
                0,
                emitted,
            )

        if len(proposed) > self._max_candidates:
            raise ValueError("weapon confirmation capacity is exhausted")
        self._last_sequence = sequence
        self._last_observed = observed_at
        self._next_candidate_id = next_candidate_id
        self._states = proposed
        self._accepted_frames += 1
        self._confirmed_events += len(new_events)
        self._expired_candidates += expired
        return new_events


def _validate_frame(
    frame: object,
) -> tuple[
    tuple[str, str, str, str],
    int,
    datetime,
    tuple[_CanonicalCandidate, ...],
]:
    if not isinstance(frame, WeaponCandidateFrame):
        raise ValueError("frame must be a WeaponCandidateFrame")
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
        not isinstance(frame.candidates, tuple)
        or len(frame.candidates) > MAX_CANDIDATES_PER_FRAME
    ):
        raise ValueError("candidates must be a tuple within the frame bound")
    candidates = tuple(
        sorted(
            (_validate_candidate(value) for value in frame.candidates),
            key=lambda value: (
                value.object_class,
                *value.box,
                -value.confidence,
                value.model_version,
            ),
        )
    )
    return scope, frame.sequence, frame.observed_at.astimezone(timezone.utc), candidates


def _validate_candidate(value: object) -> _CanonicalCandidate:
    if not isinstance(value, WeaponCandidate):
        raise ValueError("candidates must contain WeaponCandidate values")
    object_class = value.object_class.strip().lower() if isinstance(value.object_class, str) else ""
    if object_class not in WEAPON_CLASSES:
        raise ValueError("weapon candidate class is not permitted")
    if (
        isinstance(value.confidence, bool)
        or not isinstance(value.confidence, (int, float))
        or not isfinite(float(value.confidence))
        or not 0 <= float(value.confidence) <= 1
    ):
        raise ValueError("weapon candidate confidence must be normalized")
    coordinates = (value.left, value.top, value.right, value.bottom)
    if any(
        isinstance(item, bool)
        or not isinstance(item, (int, float))
        or not isfinite(float(item))
        for item in coordinates
    ):
        raise ValueError("weapon candidate box must contain finite numbers")
    box = tuple(float(item) for item in coordinates)
    if not all(0 <= item <= 1 for item in box) or box[0] >= box[2] or box[1] >= box[3]:
        raise ValueError("weapon candidate box must be positive and normalized")
    if not isinstance(value.model_version, str) or _MODEL_VERSION.fullmatch(value.model_version) is None:
        raise ValueError("model_version must be a bounded opaque identifier")
    return _CanonicalCandidate(object_class, float(value.confidence), box, value.model_version)


def _event(
    scope: tuple[str, str, str, str],
    candidate_id: int,
    first_seen_at: datetime,
    confirmed_at: datetime,
    confidence: float,
    model_version: str,
) -> dict[str, object]:
    source = f"{scope[2]}:{candidate_id}:{first_seen_at.isoformat()}"
    event_id = str(uuid5(_EVENT_NAMESPACE, f"{scope[0]}:{source}"))
    occurred_at = confirmed_at.isoformat(timespec="microseconds").replace("+00:00", "Z")
    return {
        "event_id": event_id,
        "tenant_id": scope[0],
        "dedupe_key": f"weapon_review:{event_id}",
        "occurred_at": occurred_at,
        "site_id": scope[1],
        "camera_id": scope[2],
        "zone_id": scope[3],
        "event_type": "weapon_review",
        "model_version": model_version,
        "confidence": confidence,
        "evidence_refs": [],
        "requires_human_review": True,
        "review_state": "pending",
    }


def _iou(
    left: tuple[float, float, float, float],
    right: tuple[float, float, float, float],
) -> float:
    width = max(0.0, min(left[2], right[2]) - max(left[0], right[0]))
    height = max(0.0, min(left[3], right[3]) - max(left[1], right[1]))
    intersection = width * height
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
