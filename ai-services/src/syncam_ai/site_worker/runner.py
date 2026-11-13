"""The site worker loop: frames -> detections -> tracks -> rules -> alerts.

Everything with side effects (cameras, model, network, JPEG encoding, clock)
is injected so the orchestration can be tested without hardware.
"""

from __future__ import annotations

import logging
import threading
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable, Mapping, Protocol, Sequence

from ..detection_tracking import CameraLocalDetectionTracker, Detection, DetectionFrame
from .client import BackendError
from .detector import ObjectDetection
from .rules import SiteRules
from .spool import Spool

_LOG = logging.getLogger(__name__)
_DEVICE_NAMESPACE = uuid.UUID("0d5f8d4e-4b5c-4f0e-9c4f-2b8f6c1a7e31")
_HEALTH_NAMESPACE = uuid.UUID("6a1f1c9e-3c3e-4b55-8a4c-8f1e2d7b9a10")
CAMERA_OFFLINE_SECONDS = 60.0
CONFIG_REFRESH_SECONDS = 60.0
HEARTBEAT_SECONDS = 120.0
REFERENCE_SECONDS = 1800.0

TITLES = {
    "intrusion": "Intrusion",
    "restricted_zone": "Restricted zone entry",
    "loitering": "Loitering",
    "tripwire": "Line crossed",
    "weapon_review": "Possible weapon",
    "camera_health": "Camera offline",
}


class Detector(Protocol):
    model_version: str

    def detect(self, frame: Any, *, minimum_confidence: float = ...) -> list[ObjectDetection]: ...


class Stream(Protocol):
    camera_id: str

    def latest(self) -> tuple[int, Any, datetime] | None: ...

    def health(self) -> Any: ...


class Backend(Protocol):
    def config(self) -> dict[str, Any]: ...

    def heartbeat(self, cameras: Mapping[str, Mapping[str, object]]) -> None: ...

    def put_reference(self, camera_id: str, jpeg: bytes) -> None: ...

    def upload_evidence(self, jpeg: bytes) -> str: ...

    def post_event(self, event: dict[str, object]) -> None: ...


@dataclass(frozen=True)
class AlertCandidate:
    """An event ready to send, with what to draw on its snapshot."""

    event: dict[str, object]
    title: str
    detections: tuple[ObjectDetection, ...] = ()


class FrameProcessor(Protocol):
    """Extra analytics (weapons, faces) that run on the same frame."""

    def process(
        self, camera_id: str, frame: Any, detections: Sequence[ObjectDetection], observed_at: datetime
    ) -> list[AlertCandidate]: ...


SnapshotFactory = Callable[[Any, Sequence[ObjectDetection], Sequence[tuple[str, Sequence[tuple[float, float]], bool]], str, datetime], bytes]


def default_snapshot(frame: Any, detections: Sequence[ObjectDetection], zones: Sequence[tuple[str, Sequence[tuple[float, float]], bool]], title: str, observed_at: datetime) -> bytes:
    from .evidence import annotate, encode_jpeg

    return encode_jpeg(annotate(frame, detections=detections, zones=zones, title=title, observed_at=observed_at))


def reference_snapshot(frame: Any) -> bytes:
    from .evidence import encode_jpeg

    return encode_jpeg(frame)


@dataclass
class _CameraState:
    tracker: CameraLocalDetectionTracker = field(default_factory=lambda: CameraLocalDetectionTracker(minimum_iou=0.25, max_missed_frames=8))
    last_sequence: int = -1
    processed_frames: int = 0
    last_processed: float = -1e9
    last_reference: float = -1e9
    offline_since: float | None = None
    offline_reported: bool = False


class SiteWorker:
    """Coordinates every camera at one site."""

    def __init__(
        self,
        *,
        backend: Backend,
        detector: Detector,
        streams: Mapping[str, Stream],
        spool: Spool,
        fps: float = 4.0,
        min_confidence: float = 0.45,
        processors: Sequence[FrameProcessor] = (),
        snapshot: SnapshotFactory = default_snapshot,
        reference: Callable[[Any], bytes] = reference_snapshot,
        clock: Callable[[], float] = time.monotonic,
        now: Callable[[], datetime] = lambda: datetime.now(timezone.utc),
    ) -> None:
        self._backend = backend
        self._detector = detector
        self._streams = dict(streams)
        self._spool = spool
        self._interval = 1.0 / fps
        self._min_confidence = min_confidence
        self._processors = list(processors)
        self._snapshot = snapshot
        self._reference = reference
        self._clock = clock
        self._now = now
        self._rules: SiteRules | None = None
        self._site_name = ""
        self._known_cameras: dict[str, str] = {}
        self._cameras: dict[str, _CameraState] = {camera_id: _CameraState() for camera_id in self._streams}
        self._last_config = -1e9
        self._last_heartbeat = -1e9
        self._delivery_wakeup = threading.Event()
        self._stopped = threading.Event()
        self.alerts_queued = 0

    @property
    def rules(self) -> SiteRules | None:
        return self._rules

    def refresh_config(self) -> bool:
        """Pull cameras, zones and schedules. Returns True when zones changed."""

        data = self._backend.config()
        tenant_id, site_id = str(data["tenant_id"]), str(data["site_id"])
        if self._rules is None or (self._rules.tenant_id, self._rules.site_id) != (tenant_id, site_id):
            self._rules = SiteRules(tenant_id=tenant_id, site_id=site_id)
        self._site_name = str((data.get("site") or {}).get("name", ""))
        self._known_cameras = {str(camera["id"]): str(camera.get("name", "")) for camera in data.get("cameras", [])}
        for camera_id in self._streams:
            if camera_id not in self._known_cameras:
                _LOG.warning("camera %s is in the local config but not registered for this site in the dashboard", camera_id)
        changed = self._rules.apply_zones(list(data.get("zones", [])))
        if changed:
            _LOG.info("activated %d zone rule(s) for %s", len(self._rules.zones), self._site_name or site_id)
        return changed

    def tick(self) -> int:
        """Run one pass over all cameras. Returns the number of alerts queued."""

        moment = self._clock()
        if moment - self._last_config >= CONFIG_REFRESH_SECONDS:
            try:
                self.refresh_config()
            except (BackendError, KeyError, ValueError) as error:
                _LOG.warning("configuration refresh failed: %s", error)
            self._last_config = moment
        if self._rules is None:
            return 0
        queued = 0
        for camera_id, stream in self._streams.items():
            queued += self._check_health(camera_id, stream, moment)
            state = self._cameras[camera_id]
            latest = stream.latest()
            if latest is None or moment - state.last_processed < self._interval:
                continue
            sequence, frame, observed_at = latest
            if sequence == state.last_sequence:
                continue
            state.last_sequence = sequence
            state.last_processed = moment
            if moment - state.last_reference >= REFERENCE_SECONDS:
                state.last_reference = moment
                self._upload_reference(camera_id, frame)
            queued += self.process_frame(camera_id, frame, observed_at)
        if moment - self._last_heartbeat >= HEARTBEAT_SECONDS:
            self._last_heartbeat = moment
            self._send_heartbeat()
        if queued:
            self._delivery_wakeup.set()
        return queued

    def process_frame(self, camera_id: str, frame: Any, observed_at: datetime) -> int:
        rules = self._rules
        if rules is None:
            return 0
        state = self._cameras.setdefault(camera_id, _CameraState())
        detections = self._detector.detect(frame, minimum_confidence=self._min_confidence)
        state.processed_frames += 1
        track_frame = state.tracker.track(
            DetectionFrame(
                tenant_id=rules.tenant_id,
                site_id=rules.site_id,
                camera_id=camera_id,
                device_id=str(uuid.uuid5(_DEVICE_NAMESPACE, rules.site_id)),
                sequence=state.processed_frames,
                observed_at=observed_at,
                detections=tuple(
                    Detection(
                        subject_class=item.subject_class, confidence=item.confidence, left=item.left,
                        top=item.top, right=item.right, bottom=item.bottom, model_version=self._detector.model_version,
                    )
                    for item in detections
                ),
            )
        )
        candidates = [
            AlertCandidate(event=event, title=self._title(event), detections=tuple(detections))
            for event in rules.evaluate(track_frame)
        ]
        for processor in self._processors:
            candidates.extend(processor.process(camera_id, frame, detections, observed_at))
        for candidate in candidates:
            self._queue(camera_id, frame, candidate, observed_at)
        return len(candidates)

    def _title(self, event: Mapping[str, object]) -> str:
        title = TITLES.get(str(event.get("event_type")), str(event.get("event_type")))
        zone = self._rules.zones.get(str(event.get("zone_id"))) if self._rules else None
        if zone is not None and zone.near_vehicles:
            title = "Person loitering near vehicles"
        if zone is not None and zone.name:
            title = f"{title} - {zone.name}"
        return title

    def _queue(self, camera_id: str, frame: Any, candidate: AlertCandidate, observed_at: datetime) -> None:
        zone_id = str(candidate.event.get("zone_id"))
        zones = []
        if self._rules is not None:
            for zone in self._rules.zones_for_camera(camera_id):
                if zone.compiled is not None:
                    zones.append((zone.name, zone.compiled.points, zone.id == zone_id))
        snapshot: bytes | None = None
        if frame is not None:
            try:
                snapshot = self._snapshot(frame, candidate.detections, zones, candidate.title, observed_at)
            except Exception as error:  # a snapshot failure must never drop the alert
                _LOG.error("snapshot failed for %s: %s", candidate.event.get("event_id"), error)
        self._spool.enqueue(dict(candidate.event), snapshot)
        self.alerts_queued += 1
        _LOG.info("ALERT %s on camera %s", candidate.title, self._known_cameras.get(camera_id, camera_id))

    def _check_health(self, camera_id: str, stream: Stream, moment: float) -> int:
        state = self._cameras[camera_id]
        health = stream.health()
        if health.online:
            if state.offline_reported:
                _LOG.info("camera %s is back online", camera_id)
            state.offline_since = None
            state.offline_reported = False
            return 0
        if state.offline_since is None:
            state.offline_since = moment
            return 0
        if state.offline_reported or moment - state.offline_since < CAMERA_OFFLINE_SECONDS or self._rules is None:
            return 0
        state.offline_reported = True
        event = camera_health_event(self._rules.tenant_id, self._rules.site_id, camera_id, self._now())
        self._queue(camera_id, None, AlertCandidate(event=event, title=TITLES["camera_health"]), self._now())
        return 1

    def _upload_reference(self, camera_id: str, frame: Any) -> None:
        try:
            self._backend.put_reference(camera_id, self._reference(frame))
        except Exception as error:
            _LOG.warning("reference image upload failed for %s: %s", camera_id, error)

    def _send_heartbeat(self) -> None:
        cameras: dict[str, dict[str, object]] = {}
        for camera_id, stream in self._streams.items():
            health = stream.health()
            status: dict[str, object] = {"online": bool(health.online)}
            if health.last_frame_at is not None:
                status["last_frame_at"] = health.last_frame_at.isoformat().replace("+00:00", "Z")
            if health.error:
                status["error"] = str(health.error)[:200]
            cameras[camera_id] = status
        try:
            self._backend.heartbeat(cameras)
        except BackendError as error:
            _LOG.warning("heartbeat failed: %s", error)

    def deliver_pending(self) -> None:
        result = self._spool.deliver(self._backend)
        if result.delivered or result.dropped:
            _LOG.info("alerts delivered=%d dropped=%d pending=%d", result.delivered, result.dropped, result.pending)

    def run(self, *, poll_seconds: float = 0.05) -> None:
        delivery = threading.Thread(target=self._delivery_loop, name="alert-delivery", daemon=True)
        delivery.start()
        while not self._stopped.is_set():
            try:
                self.tick()
            except Exception:
                _LOG.exception("site worker pass failed; continuing")
            self._stopped.wait(poll_seconds)

    def stop(self) -> None:
        self._stopped.set()
        self._delivery_wakeup.set()

    def _delivery_loop(self) -> None:
        while not self._stopped.is_set():
            self._delivery_wakeup.wait(15)
            self._delivery_wakeup.clear()
            try:
                self.deliver_pending()
            except Exception:
                _LOG.exception("alert delivery failed; will retry")


def camera_health_event(tenant_id: str, site_id: str, camera_id: str, at: datetime) -> dict[str, object]:
    """A canonical camera-offline event. The camera ID stands in for the zone."""

    stamp = at.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    event_id = str(uuid.uuid5(_HEALTH_NAMESPACE, f"{tenant_id}:{camera_id}:{stamp}"))
    return {
        "event_id": event_id,
        "tenant_id": tenant_id,
        "dedupe_key": f"camera_health:{event_id}",
        "occurred_at": stamp,
        "site_id": site_id,
        "camera_id": camera_id,
        "zone_id": camera_id,
        "event_type": "camera_health",
        "model_version": "site-worker-health-1",
        "confidence": 1.0,
        "evidence_refs": [],
        "requires_human_review": True,
        "review_state": "pending",
    }
