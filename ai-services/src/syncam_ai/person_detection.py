"""Local-only Apache-compatible person-detection adapter.

This module deliberately has no HTTP client, persistence, event emission or
camera capture. It accepts an already-decoded BGR frame and an external model
pair whose published checksums have been verified. Model files never belong in
the repository.
"""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha384
from math import isfinite
from pathlib import Path
from typing import Any

import numpy as np
from numpy.typing import NDArray

_MAX_FRAME_PIXELS = 33_554_432
_MODEL_INPUT_SIZE = 256
_PERSON_LABEL = 0


@dataclass(frozen=True)
class PersonDetectorArtifact:
    """The source and immutable hashes for one external IR artifact pair."""

    identifier: str
    license: str
    source_manifest: str
    xml_sha384: str
    bin_sha384: str


INTEL_PERSON_DETECTION_0200 = PersonDetectorArtifact(
    identifier="intel-person-detection-0200-fp32",
    license="Apache-2.0",
    source_manifest=(
        "https://raw.githubusercontent.com/openvinotoolkit/open_model_zoo/"
        "master/models/intel/person-detection-0200/model.yml"
    ),
    xml_sha384=(
        "c615231e6fc865bd875e95c283d115e9ee9742ef02910b8752fe4b60724b1b5e"
        "198ed564ade0372109cd7abe9eed43ae"
    ),
    bin_sha384=(
        "fdf355324c0603bd6e917067fe07581792fa48aeb5f47a031f3ed2773c541051"
        "712a3e2bf03cc681baa9ad6900d9f147"
    ),
)


@dataclass(frozen=True)
class PersonDetection:
    """A bounded, pixel-coordinate detection from one local frame."""

    confidence: float
    left: int
    top: int
    right: int
    bottom: int


def verify_person_detector_artifact(
    xml_path: Path,
    bin_path: Path,
    artifact: PersonDetectorArtifact = INTEL_PERSON_DETECTION_0200,
) -> None:
    """Fail closed unless the external model pair exactly matches its manifest."""

    if artifact.license != "Apache-2.0":
        raise ValueError("person detector artifact must use the Apache-2.0 allowlist")
    _verify_file(xml_path, artifact.xml_sha384, "XML")
    _verify_file(bin_path, artifact.bin_sha384, "BIN")


def parse_person_detections(
    output: NDArray[np.floating[Any]],
    *,
    frame_width: int,
    frame_height: int,
    minimum_confidence: float,
) -> tuple[PersonDetection, ...]:
    """Parse the documented SSD output without inferring or storing identities."""

    _validate_frame_dimensions(frame_width, frame_height)
    _validate_confidence(minimum_confidence)
    values = np.asarray(output)
    if values.ndim != 4 or values.shape[0:2] != (1, 1) or values.shape[-1] != 7:
        raise ValueError("person detector output must use the documented 1x1xNx7 contract")
    if not np.isfinite(values).all():
        raise ValueError("person detector output must be finite")

    detections: list[PersonDetection] = []
    for row in values[0, 0]:
        image_id, label, confidence, left, top, right, bottom = (float(value) for value in row)
        if image_id < 0:
            break
        if int(label) != _PERSON_LABEL:
            raise ValueError("person detector emitted an unexpected label")
        if confidence < minimum_confidence:
            continue
        normalized = tuple(min(1.0, max(0.0, value)) for value in (left, top, right, bottom))
        if normalized[0] >= normalized[2] or normalized[1] >= normalized[3]:
            continue
        detections.append(
            PersonDetection(
                confidence=confidence,
                left=int(normalized[0] * frame_width),
                top=int(normalized[1] * frame_height),
                right=int(normalized[2] * frame_width),
                bottom=int(normalized[3] * frame_height),
            )
        )
    return tuple(detections)


class OpenVinoPersonDetector:
    """A developer-machine adapter for the verified Intel person detector."""

    def __init__(
        self,
        *,
        xml_path: Path,
        bin_path: Path,
        device: str = "CPU",
        artifact: PersonDetectorArtifact = INTEL_PERSON_DETECTION_0200,
    ) -> None:
        verify_person_detector_artifact(xml_path, bin_path, artifact)
        try:
            from openvino import Core
        except ImportError as error:
            raise RuntimeError(
                "install the person-detector extra before loading an external model"
            ) from error

        core = Core()
        model = core.read_model(model=str(xml_path), weights=str(bin_path))
        self._compiled_model = core.compile_model(model, device)
        self._input_name = self._compiled_model.input(0).any_name

    def detect(
        self,
        frame: NDArray[np.uint8[Any]],
        *,
        minimum_confidence: float = 0.5,
    ) -> tuple[PersonDetection, ...]:
        """Run local inference over one BGR frame and return person boxes only."""

        _validate_frame(frame)
        _validate_confidence(minimum_confidence)
        height, width, _ = frame.shape
        resized = _resize_nearest_bgr(frame, _MODEL_INPUT_SIZE, _MODEL_INPUT_SIZE)
        tensor = np.transpose(resized, (2, 0, 1))[np.newaxis, ...]
        outputs = self._compiled_model({self._input_name: tensor})
        if len(outputs) != 1:
            raise RuntimeError("person detector must return exactly one output")
        output = np.asarray(next(iter(outputs.values())))
        return parse_person_detections(
            output, frame_width=width, frame_height=height, minimum_confidence=minimum_confidence
        )


def _verify_file(path: Path, expected_sha384: str, label: str) -> None:
    if not path.is_file():
        raise ValueError(f"person detector {label} artifact is missing")
    digest = sha384(path.read_bytes()).hexdigest()
    if digest != expected_sha384:
        raise ValueError(f"person detector {label} checksum does not match its manifest")


def _validate_confidence(value: float) -> None:
    if isinstance(value, bool) or not isinstance(value, (float, int)):
        raise ValueError("minimum confidence must be numeric")
    if not isfinite(float(value)) or not 0.0 <= float(value) <= 1.0:
        raise ValueError("minimum confidence must be finite and within [0, 1]")


def _validate_frame_dimensions(width: int, height: int) -> None:
    if not all(isinstance(value, int) and not isinstance(value, bool) for value in (width, height)):
        raise ValueError("frame dimensions must be integers")
    if not 1 <= width <= 8192 or not 1 <= height <= 8192 or width * height > _MAX_FRAME_PIXELS:
        raise ValueError("frame dimensions exceed the local safety bound")


def _validate_frame(frame: NDArray[np.uint8[Any]]) -> None:
    if not isinstance(frame, np.ndarray) or frame.dtype != np.uint8:
        raise ValueError("person detector frame must be a uint8 ndarray")
    if frame.ndim != 3 or frame.shape[2] != 3:
        raise ValueError("person detector frame must be a BGR image with three channels")
    _validate_frame_dimensions(int(frame.shape[1]), int(frame.shape[0]))


def _resize_nearest_bgr(
    frame: NDArray[np.uint8[Any]], target_height: int, target_width: int
) -> NDArray[np.uint8[Any]]:
    row_indices = np.arange(target_height) * frame.shape[0] // target_height
    column_indices = np.arange(target_width) * frame.shape[1] // target_width
    return frame[row_indices][:, column_indices]

