"""YOLOX (Apache-2.0) person and vehicle detection on OpenVINO.

One shared detector finds people and vehicles. Output boxes are normalized to
0..1 so they plug straight into ``detection_tracking.Detection`` and the
existing zone rules. NumPy, OpenCV and OpenVINO are imported lazily so the
rest of the package (and its tests) work without the site-worker extras.
"""

from __future__ import annotations

from dataclasses import dataclass
from math import exp
from pathlib import Path
from typing import Any, Final, Sequence

from ..model_artifacts import YOLOX_S, ModelArtifact, verify_model_artifact

# COCO class index -> canonical SyncCam subject class.
COCO_SUBJECTS: Final = {0: "person", 1: "bicycle", 2: "car", 3: "motorcycle", 5: "bus", 7: "truck"}
COCO_KNIFE: Final = 43
_STRIDES: Final = (8, 16, 32)
_VEHICLE_GROUP: Final = frozenset({"car", "truck", "bus", "van"})


@dataclass(frozen=True, slots=True)
class ObjectDetection:
    """A normalized box with a canonical class; no pixels or identities."""

    subject_class: str
    confidence: float
    left: float
    top: float
    right: float
    bottom: float

    @property
    def center(self) -> tuple[float, float]:
        return ((self.left + self.right) / 2, (self.top + self.bottom) / 2)


def _iou(a: tuple[float, float, float, float], b: tuple[float, float, float, float]) -> float:
    left, top = max(a[0], b[0]), max(a[1], b[1])
    right, bottom = min(a[2], b[2]), min(a[3], b[3])
    intersection = max(0.0, right - left) * max(0.0, bottom - top)
    union = (a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - intersection
    return 0.0 if union <= 0 else intersection / union


def non_max_suppression(
    candidates: Sequence[tuple[str, float, tuple[float, float, float, float]]],
    *,
    iou_threshold: float = 0.45,
) -> list[tuple[str, float, tuple[float, float, float, float]]]:
    """Greedy NMS over (class, score, box) candidates.

    Vehicle classes suppress each other, so one pickup is never reported as
    both a "car" and a "truck"; people and vehicles never suppress each other.
    """

    def group(label: str) -> str:
        return "vehicle" if label in _VEHICLE_GROUP else label

    kept: list[tuple[str, float, tuple[float, float, float, float]]] = []
    for candidate in sorted(candidates, key=lambda item: item[1], reverse=True):
        if all(
            group(candidate[0]) != group(other[0]) or _iou(candidate[2], other[2]) < iou_threshold
            for other in kept
        ):
            kept.append(candidate)
    return kept


def decode_yolox(
    rows: Any,
    *,
    input_size: int,
    ratio: float,
    frame_width: int,
    frame_height: int,
    minimum_confidence: float,
    class_map: dict[int, str] = COCO_SUBJECTS,
    iou_threshold: float = 0.45,
    max_detections: int = 256,
) -> list[ObjectDetection]:
    """Decode raw YOLOX head output (N x (5 + classes)) into normalized boxes.

    ``rows`` are ordered by stride 8, 16, 32 grids, as exported by YOLOX with
    ``decode_in_inference=False``. Coordinates are mapped back through the
    letterbox ``ratio`` into the original frame.
    """

    grids: list[tuple[int, int, int]] = []
    for stride in _STRIDES:
        size = input_size // stride
        grids.extend((x, y, stride) for y in range(size) for x in range(size))
    if len(rows) != len(grids):
        raise ValueError("YOLOX output does not match the input grid")
    candidates: list[tuple[str, float, tuple[float, float, float, float]]] = []
    for row, (grid_x, grid_y, stride) in zip(rows, grids):
        objectness = float(row[4])
        if objectness < minimum_confidence:
            continue
        best_index, best_score = -1, 0.0
        for index in class_map:
            score = objectness * float(row[5 + index])
            if score > best_score:
                best_index, best_score = index, score
        if best_index < 0 or best_score < minimum_confidence:
            continue
        center_x = (float(row[0]) + grid_x) * stride / ratio
        center_y = (float(row[1]) + grid_y) * stride / ratio
        width = exp(min(float(row[2]), 10.0)) * stride / ratio
        height = exp(min(float(row[3]), 10.0)) * stride / ratio
        box = (
            min(1.0, max(0.0, (center_x - width / 2) / frame_width)),
            min(1.0, max(0.0, (center_y - height / 2) / frame_height)),
            min(1.0, max(0.0, (center_x + width / 2) / frame_width)),
            min(1.0, max(0.0, (center_y + height / 2) / frame_height)),
        )
        if box[0] >= box[2] or box[1] >= box[3]:
            continue
        candidates.append((class_map[best_index], best_score, box))
    kept = non_max_suppression(candidates, iou_threshold=iou_threshold)[:max_detections]
    return [
        ObjectDetection(subject_class=label, confidence=round(score, 4), left=box[0], top=box[1], right=box[2], bottom=box[3])
        for label, score, box in kept
    ]


def letterbox(frame: Any, input_size: int) -> tuple[Any, float]:
    """Resize keeping aspect ratio and pad bottom/right with YOLOX grey (114)."""

    import cv2
    import numpy

    height, width = frame.shape[:2]
    ratio = min(input_size / height, input_size / width)
    resized = cv2.resize(frame, (max(1, int(width * ratio)), max(1, int(height * ratio))), interpolation=cv2.INTER_LINEAR)
    padded = numpy.full((input_size, input_size, 3), 114, dtype=numpy.uint8)
    padded[: resized.shape[0], : resized.shape[1]] = resized
    return padded, ratio


class YoloxDetector:
    """Verified YOLOX ONNX model compiled with OpenVINO (GPU when available)."""

    def __init__(
        self,
        model_path: Path,
        *,
        device: str = "AUTO",
        artifact: ModelArtifact = YOLOX_S,
        input_size: int = 640,
        class_map: dict[int, str] = COCO_SUBJECTS,
    ) -> None:
        verify_model_artifact(artifact, {"onnx": model_path})
        try:
            import openvino
        except ImportError as error:
            raise RuntimeError("install the site-worker extra to run detection") from error
        core = openvino.Core()
        self._model = core.compile_model(core.read_model(str(model_path)), device)
        self._input_size = input_size
        self._class_map = class_map
        self.model_version = artifact.model_version

    def detect(self, frame: Any, *, minimum_confidence: float = 0.45) -> list[ObjectDetection]:
        import numpy

        if not isinstance(frame, numpy.ndarray) or frame.dtype != numpy.uint8 or frame.ndim != 3 or frame.shape[2] != 3:
            raise ValueError("detector frame must be a BGR uint8 image")
        height, width = frame.shape[:2]
        padded, ratio = letterbox(frame, self._input_size)
        tensor = padded.transpose(2, 0, 1)[numpy.newaxis].astype(numpy.float32)
        output = next(iter(self._model(tensor).values()))[0]
        # Pre-filter on objectness in NumPy before the per-row Python decode.
        return decode_yolox_fast(
            output, input_size=self._input_size, ratio=ratio, frame_width=width,
            frame_height=height, minimum_confidence=minimum_confidence, class_map=self._class_map,
        )


def decode_yolox_fast(
    output: Any,
    *,
    input_size: int,
    ratio: float,
    frame_width: int,
    frame_height: int,
    minimum_confidence: float,
    class_map: dict[int, str] = COCO_SUBJECTS,
) -> list[ObjectDetection]:
    """Vectorised equivalent of :func:`decode_yolox` for real-time use."""

    import numpy

    grids, strides = [], []
    for stride in _STRIDES:
        size = input_size // stride
        ys, xs = numpy.meshgrid(numpy.arange(size), numpy.arange(size), indexing="ij")
        grids.append(numpy.stack((xs, ys), axis=2).reshape(-1, 2))
        strides.append(numpy.full((size * size, 1), stride))
    grid = numpy.concatenate(grids)
    stride_column = numpy.concatenate(strides)
    if output.shape[0] != grid.shape[0]:
        raise ValueError("YOLOX output does not match the input grid")
    indices = numpy.array(sorted(class_map))
    scores = output[:, 4:5] * output[:, 5 + indices]
    best = scores.argmax(axis=1)
    best_scores = scores[numpy.arange(len(scores)), best]
    keep = best_scores >= minimum_confidence
    if not keep.any():
        return []
    rows = output[keep]
    centers = (rows[:, :2] + grid[keep]) * stride_column[keep] / ratio
    sizes = numpy.exp(numpy.minimum(rows[:, 2:4], 10.0)) * stride_column[keep] / ratio
    boxes = numpy.concatenate((centers - sizes / 2, centers + sizes / 2), axis=1)
    boxes /= numpy.array([frame_width, frame_height, frame_width, frame_height], dtype=boxes.dtype)
    boxes = numpy.clip(boxes, 0.0, 1.0)
    labels = indices[best[keep]]
    candidates = [
        (class_map[int(label)], float(score), (float(box[0]), float(box[1]), float(box[2]), float(box[3])))
        for label, score, box in zip(labels, best_scores[keep], boxes)
        if box[0] < box[2] and box[1] < box[3]
    ]
    kept = non_max_suppression(candidates)[:256]
    return [
        ObjectDetection(subject_class=label, confidence=round(score, 4), left=box[0], top=box[1], right=box[2], bottom=box[3])
        for label, score, box in kept
    ]
