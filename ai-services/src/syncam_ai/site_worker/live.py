"""Near-live view for the dashboard's Camera Wall.

Video stays on the site box by default. Every couple of seconds the box asks
the API which of its cameras someone is watching right now (an authorised,
audited session in the dashboard). Only for those cameras it uploads small
JPEG frames, about two a second, and stops as soon as nobody is watching.
Frames are kept in the API's memory only and are never stored. Set
``"live_view": false`` in the site config to turn this off for the site.
"""

from __future__ import annotations

import logging
import threading
import time
from datetime import datetime
from typing import Any, Callable, Mapping

from .client import BackendError

_LOG = logging.getLogger(__name__)
MAX_WIDTH = 640
JPEG_QUALITY = 70


def encode_live_frame(frame: Any, captured_at: datetime | None = None) -> bytes:
    """A small JPEG of ``frame``, at most 640 px wide, with its capture time."""

    import cv2

    height, width = frame.shape[:2]
    if width > MAX_WIDTH:
        frame = cv2.resize(frame, (MAX_WIDTH, int(height * MAX_WIDTH / width)), interpolation=cv2.INTER_AREA)
    else:
        frame = frame.copy()
    if captured_at is not None:
        stamp = captured_at.astimezone().strftime("%Y-%m-%d %H:%M:%S")
        cv2.putText(frame, stamp, (10, frame.shape[0] - 12), cv2.FONT_HERSHEY_SIMPLEX, 0.55, (0, 0, 0), 3, cv2.LINE_AA)
        cv2.putText(frame, stamp, (10, frame.shape[0] - 12), cv2.FONT_HERSHEY_SIMPLEX, 0.55, (255, 255, 255), 1, cv2.LINE_AA)
    ok, encoded = cv2.imencode(".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, JPEG_QUALITY])
    if not ok:
        raise ValueError("could not encode live frame")
    return encoded.tobytes()


class LiveRelay:
    """Sends frames for watched cameras only."""

    def __init__(
        self,
        backend: Any,
        streams: Mapping[str, Any],
        *,
        poll_seconds: float = 2.0,
        fps: float = 2.0,
        encode: Callable[[Any, datetime | None], bytes] = encode_live_frame,
        clock: Callable[[], float] = time.monotonic,
        on_tasks: Callable[[list[Any]], None] | None = None,
        send_frames: bool = True,
    ) -> None:
        self._backend = backend
        self._streams = streams
        self._on_tasks = on_tasks
        self._send_frames = send_frames
        self._poll_seconds = poll_seconds
        self._interval = 1.0 / fps
        self._encode = encode
        self._clock = clock
        self._wanted: set[str] = set()
        self._last_poll = -1e9
        self._last_sent: dict[str, float] = {}
        self._last_sequence: dict[str, int] = {}
        self._stopped = threading.Event()

    @property
    def wanted(self) -> frozenset[str]:
        return frozenset(self._wanted)

    def _refresh(self, now: float) -> None:
        self._last_poll = now
        try:
            if hasattr(self._backend, "poll"):
                data = self._backend.poll()
            else:
                data = {"cameras": self._backend.live_requests(), "tasks": []}
        except BackendError as error:
            _LOG.debug("live view poll failed: %s", error)
            return
        if data.get("tasks") and self._on_tasks is not None:
            self._on_tasks(list(data["tasks"]))
        requested = set(data.get("cameras", [])) if self._send_frames else set()
        started = requested - self._wanted
        ended = self._wanted - requested
        for camera_id in sorted(started):
            if camera_id in self._streams:
                _LOG.info("live view started for camera %s", camera_id)
        for camera_id in sorted(ended):
            _LOG.info("live view ended for camera %s", camera_id)
        # Only cameras this box actually reads; anything else is ignored.
        self._wanted = {camera_id for camera_id in requested if camera_id in self._streams}

    def step(self) -> int:
        """One pass: refresh the wanted list when due, then send due frames."""

        now = self._clock()
        if now - self._last_poll >= self._poll_seconds:
            self._refresh(now)
        sent = 0
        for camera_id in sorted(self._wanted):
            if now - self._last_sent.get(camera_id, -1e9) < self._interval:
                continue
            latest = self._streams[camera_id].latest()
            if latest is None or latest[0] == self._last_sequence.get(camera_id):
                continue
            sequence, frame, captured_at = latest
            self._last_sent[camera_id] = now
            self._last_sequence[camera_id] = sequence
            try:
                still_wanted = self._backend.put_live_frame(camera_id, self._encode(frame, captured_at))
            except BackendError as error:
                _LOG.debug("live frame upload failed for %s: %s", camera_id, error)
                continue
            except ValueError as error:
                _LOG.warning("live frame for %s skipped: %s", camera_id, error)
                continue
            if still_wanted:
                sent += 1
            else:
                self._wanted.discard(camera_id)
                _LOG.info("live view ended for camera %s", camera_id)
        return sent

    def run(self, *, tick_seconds: float = 0.1) -> None:
        while not self._stopped.is_set():
            try:
                self.step()
            except Exception:  # never let live view take the site box down
                _LOG.exception("live view pass failed; continuing")
            self._stopped.wait(tick_seconds)

    def start(self) -> threading.Thread:
        thread = threading.Thread(target=self.run, name="live-view", daemon=True)
        thread.start()
        return thread

    def stop(self) -> None:
        self._stopped.set()
