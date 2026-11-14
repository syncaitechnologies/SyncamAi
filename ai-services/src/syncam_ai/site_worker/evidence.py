"""Annotated JPEG snapshots for alerts and zone-drawing reference frames."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Sequence

from .detector import ObjectDetection

MAX_SNAPSHOT_BYTES = 500 * 1024
_MAX_WIDTH = 1280
_ALERT = (40, 40, 230)  # BGR red
_ZONE = (0, 200, 255)  # BGR amber
_OTHER = (200, 200, 200)


def _resized(frame: Any) -> Any:
    import cv2

    height, width = frame.shape[:2]
    if width <= _MAX_WIDTH:
        return frame.copy()
    scale = _MAX_WIDTH / width
    return cv2.resize(frame, (_MAX_WIDTH, int(height * scale)), interpolation=cv2.INTER_AREA)


def encode_jpeg(frame: Any, *, limit: int = MAX_SNAPSHOT_BYTES) -> bytes:
    """Encode as JPEG, stepping quality down until it fits the upload limit."""

    import cv2

    image = _resized(frame)
    for quality in (85, 75, 65, 55, 45, 35):
        ok, buffer = cv2.imencode(".jpg", image, [int(cv2.IMWRITE_JPEG_QUALITY), quality])
        if ok and len(buffer) <= limit:
            return bytes(buffer)
        image = cv2.resize(image, (int(image.shape[1] * 0.85), int(image.shape[0] * 0.85)), interpolation=cv2.INTER_AREA)
    raise ValueError("snapshot could not be compressed under the upload limit")


def annotate(
    frame: Any,
    *,
    detections: Sequence[ObjectDetection],
    zones: Sequence[tuple[str, Sequence[tuple[float, float]], bool]],
    title: str,
    observed_at: datetime,
) -> Any:
    """Draw zone outlines, boxes and a caption bar onto a copy of the frame.

    ``zones`` holds (name, normalized points, highlighted) tuples.
    """

    import cv2
    import numpy

    image = _resized(frame)
    height, width = image.shape[:2]
    for name, points, highlighted in zones:
        if len(points) < 2:
            continue
        polygon = numpy.array([[int(x * width), int(y * height)] for x, y in points], dtype=numpy.int32)
        color = _ALERT if highlighted else _ZONE
        cv2.polylines(image, [polygon], isClosed=len(points) > 2, color=color, thickness=2)
        anchor = polygon.min(axis=0)
        cv2.putText(image, name[:40], (int(anchor[0]) + 4, max(16, int(anchor[1]) + 16)), cv2.FONT_HERSHEY_SIMPLEX, 0.5, color, 1, cv2.LINE_AA)
    for detection in detections:
        color = _ALERT if detection.subject_class == "person" or detection.subject_class in {"knife", "firearm"} else _OTHER
        top_left = (int(detection.left * width), int(detection.top * height))
        bottom_right = (int(detection.right * width), int(detection.bottom * height))
        cv2.rectangle(image, top_left, bottom_right, color, 2)
        label = f"{detection.subject_class} {detection.confidence:.0%}"
        cv2.putText(image, label, (top_left[0], max(14, top_left[1] - 4)), cv2.FONT_HERSHEY_SIMPLEX, 0.45, color, 1, cv2.LINE_AA)
    banner = f"SyncCam AI | {title} | {observed_at.strftime('%Y-%m-%d %H:%M:%S %Z')}"
    cv2.rectangle(image, (0, height - 26), (width, height), (0, 0, 0), thickness=-1)
    cv2.putText(image, banner[:120], (8, height - 8), cv2.FONT_HERSHEY_SIMPLEX, 0.5, (255, 255, 255), 1, cv2.LINE_AA)
    return image
