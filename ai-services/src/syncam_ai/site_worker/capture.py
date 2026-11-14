"""Background camera readers that always hold only the newest frame.

RTSP streams from NVRs and IP cameras are read over TCP through OpenCV's
FFmpeg backend. A reader reconnects with backoff, never buffers old frames,
and reports health so the dashboard can show offline cameras.
"""

from __future__ import annotations

import logging
import os
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

_LOG = logging.getLogger(__name__)
os.environ.setdefault("OPENCV_FFMPEG_CAPTURE_OPTIONS", "rtsp_transport;tcp|stimeout;10000000")


def redact(source: str) -> str:
    """Hide credentials in an RTSP URL before it reaches a log line."""

    if "@" in source and "://" in source:
        scheme, rest = source.split("://", 1)
        return f"{scheme}://***@{rest.split('@', 1)[1]}"
    return source


@dataclass(frozen=True)
class CameraHealth:
    online: bool
    last_frame_at: datetime | None
    error: str


class CameraStream(threading.Thread):
    """Reads one camera continuously in a daemon thread."""

    def __init__(self, camera_id: str, source: str, *, loop_files: bool = True) -> None:
        super().__init__(name=f"camera-{camera_id[:8]}", daemon=True)
        self.camera_id = camera_id
        self._source = source
        self._loop_files = loop_files
        self._lock = threading.Lock()
        self._frame: Any = None
        self._frame_at: datetime | None = None
        self._sequence = 0
        self._error = "starting"
        self._stopped = threading.Event()

    def stop(self) -> None:
        self._stopped.set()

    def latest(self) -> tuple[int, Any, datetime] | None:
        with self._lock:
            if self._frame is None or self._frame_at is None:
                return None
            return self._sequence, self._frame, self._frame_at

    def health(self, *, stale_after: float = 15.0) -> CameraHealth:
        with self._lock:
            fresh = self._frame_at is not None and (datetime.now(timezone.utc) - self._frame_at).total_seconds() < stale_after
            return CameraHealth(online=fresh, last_frame_at=self._frame_at, error="" if fresh else self._error)

    def _open(self) -> Any:
        import cv2

        source: Any = int(self._source) if self._source.isdigit() else self._source
        if isinstance(source, str) and source.lower().startswith(("rtsp://", "rtsps://", "http://", "https://")):
            return cv2.VideoCapture(source, cv2.CAP_FFMPEG)
        return cv2.VideoCapture(source)

    def run(self) -> None:
        import cv2

        backoff = 2.0
        is_file = Path(self._source).is_file()
        while not self._stopped.is_set():
            capture = self._open()
            if not capture.isOpened():
                self._set_error("cannot connect to camera")
                _LOG.warning("camera %s unreachable at %s; retrying in %.0fs", self.camera_id, redact(self._source), backoff)
                capture.release()
                self._stopped.wait(backoff)
                backoff = min(backoff * 2, 60.0)
                continue
            backoff = 2.0
            frame_delay = 0.0
            if is_file:
                fps = capture.get(cv2.CAP_PROP_FPS) or 15.0
                frame_delay = 1.0 / max(1.0, min(fps, 60.0))
            failures = 0
            while not self._stopped.is_set():
                ok, frame = capture.read()
                if not ok or frame is None:
                    failures += 1
                    if is_file and self._loop_files:
                        capture.set(cv2.CAP_PROP_POS_FRAMES, 0)
                        continue
                    if failures >= 25:
                        self._set_error("camera stream stopped")
                        break
                    time.sleep(0.05)
                    continue
                failures = 0
                with self._lock:
                    self._frame = frame
                    self._frame_at = datetime.now(timezone.utc)
                    self._sequence += 1
                if frame_delay:
                    time.sleep(frame_delay)
            capture.release()
            if not self._stopped.is_set():
                _LOG.warning("camera %s disconnected; reconnecting", self.camera_id)
                self._stopped.wait(backoff)

    def _set_error(self, message: str) -> None:
        with self._lock:
            self._error = message
