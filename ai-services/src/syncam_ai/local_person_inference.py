"""Development-only HTTPS handler for transient, authorized person detections.

This module has no persistence, recording, model download, event emission, face
processing, weapon detection or alert creation. It accepts one bounded RGBA
frame in memory, converts it to BGR for the verified local detector, and
returns person boxes only. It must never be exposed as a production service.
"""

from __future__ import annotations

from dataclasses import dataclass
from math import isfinite
from typing import Any, Protocol
from urllib.parse import parse_qs, urlsplit

_PATH = "/local-dev/v1/person-detections"
_MAX_FRAME_PIXELS = 640 * 480
_MAX_FRAME_BYTES = _MAX_FRAME_PIXELS * 4


@dataclass(frozen=True)
class LocalFrame:
    """A short-lived browser RGBA frame with checked dimensions."""

    width: int
    height: int
    rgba: bytes


@dataclass(frozen=True)
class LocalPersonBox:
    """A bounded detection response for one transient frame."""

    confidence: float
    left: int
    top: int
    right: int
    bottom: int


class LocalPersonDetector(Protocol):
    def detect(self, frame: Any, *, minimum_confidence: float) -> tuple[Any, ...]:
        """Return local person detections for a BGR frame."""


def decode_local_frame(path: str, content_type: str | None, body: bytes) -> LocalFrame:
    """Strictly decode exactly one transient RGBA frame from the dev-only route."""

    parsed = urlsplit(path)
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    if parsed.path != _PATH or set(query) != {"width", "height"}:
        raise ValueError("unsupported local inference request")
    width = _single_dimension(query, "width")
    height = _single_dimension(query, "height")
    if width * height > _MAX_FRAME_PIXELS:
        raise ValueError("local inference dimensions exceed the development bound")
    if (content_type or "").split(";", 1)[0].strip().lower() != "application/octet-stream":
        raise ValueError("local inference requires an RGBA binary frame")
    expected_bytes = width * height * 4
    if len(body) != expected_bytes:
        raise ValueError("local inference frame length does not match its dimensions")
    return LocalFrame(width=width, height=height, rgba=body)


def validate_local_inference_origin(origin: str) -> str:
    """Allow one explicit HTTPS browser origin; wildcard and credentials are forbidden."""

    try:
        parsed = urlsplit(origin)
    except ValueError as error:
        raise ValueError("local inference origin must be a valid HTTPS origin") from error
    if (
        parsed.scheme != "https"
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("local inference origin must be one HTTPS origin without credentials or a path")
    return f"https://{parsed.netloc}"


def rgba_to_bgr(frame: LocalFrame) -> Any:
    """Convert one in-memory RGBA frame only after the optional runtime is present."""

    try:
        import numpy
    except ImportError as error:
        raise RuntimeError("install the person-detector extra before running local inference") from error
    rgba = numpy.frombuffer(frame.rgba, dtype=numpy.uint8).reshape((frame.height, frame.width, 4))
    return rgba[:, :, [2, 1, 0]].copy()


def detect_local_people(
    detector: LocalPersonDetector,
    frame: LocalFrame,
    *,
    minimum_confidence: float = 0.6,
) -> tuple[LocalPersonBox, ...]:
    """Run one local frame and defensively serialize only bounded person boxes."""

    if not isinstance(minimum_confidence, (float, int)) or isinstance(minimum_confidence, bool):
        raise ValueError("minimum confidence must be numeric")
    if not isfinite(float(minimum_confidence)) or not 0.0 <= float(minimum_confidence) <= 1.0:
        raise ValueError("minimum confidence must be finite and within [0, 1]")
    detections = detector.detect(rgba_to_bgr(frame), minimum_confidence=float(minimum_confidence))
    boxes: list[LocalPersonBox] = []
    for detection in detections:
        values = (
            getattr(detection, "confidence", None),
            getattr(detection, "left", None),
            getattr(detection, "top", None),
            getattr(detection, "right", None),
            getattr(detection, "bottom", None),
        )
        if not _valid_box(values, frame.width, frame.height):
            raise RuntimeError("local detector returned an invalid person box")
        confidence, left, top, right, bottom = values
        boxes.append(
            LocalPersonBox(
                confidence=float(confidence),
                left=int(left),
                top=int(top),
                right=int(right),
                bottom=int(bottom),
            )
        )
    return tuple(boxes)


def local_person_response(boxes: tuple[LocalPersonBox, ...]) -> dict[str, object]:
    """Return a deliberately narrow response without pixels, identities or model internals."""

    return {
        "detections": [
            {
                "confidence": box.confidence,
                "left": box.left,
                "top": box.top,
                "right": box.right,
                "bottom": box.bottom,
            }
            for box in boxes
        ]
    }


def _single_dimension(query: dict[str, list[str]], name: str) -> int:
    values = query.get(name, [])
    if len(values) != 1 or not values[0].isdigit():
        raise ValueError("local inference dimensions are invalid")
    value = int(values[0])
    if not 1 <= value <= 640:
        raise ValueError("local inference dimensions exceed the development bound")
    return value


def _valid_box(values: tuple[object, object, object, object, object], width: int, height: int) -> bool:
    confidence, left, top, right, bottom = values
    if not all(isinstance(value, (float, int)) and not isinstance(value, bool) for value in values):
        return False
    if not isfinite(float(confidence)) or not 0.0 <= float(confidence) <= 1.0:
        return False
    return 0 <= int(left) < int(right) <= width and 0 <= int(top) < int(bottom) <= height
