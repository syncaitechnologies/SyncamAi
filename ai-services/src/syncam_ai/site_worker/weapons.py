"""Possible-weapon alerts: a detector plus the existing 3-frame confirmation.

The detector only runs on frames where a person is visible, and also looks
inside enlarged crops around the largest people so small handguns are not lost
when the frame is shrunk to 640 px. Candidates go through
``WeaponReviewConfirmation`` (weapon_review.py), which needs the same object in
three consecutive frames within one second before it emits a ``weapon_review``
event. Every alert is a request for urgent human review, never a conclusion.

The development model pinned below has no stated licence and is therefore
marked ``dev_only``: it loads only with ``allow_dev_models`` and must be
replaced by a licence-clean model before production.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Final, Sequence

from ..model_artifacts import ModelArtifact, verify_model_artifact
from ..weapon_review import WeaponCandidate, WeaponCandidateFrame, WeaponReviewConfirmation
from .config import LocalConfig
from .detector import ObjectDetection, letterbox, non_max_suppression
from .runner import AlertCandidate

WEAPON_DEV_YOLOV8: Final = ModelArtifact(
    identifier="weapon-dev-yolov8n-hadi959",
    license="unspecified-community",
    source="https://huggingface.co/Hadi959/weapon-detection-yolov8/resolve/main/best.onnx",
    files=(
        (
            "onnx",
            "86f286064e66ee2f857ec35eb3e9cf9094e1e7e2594536ddcdd35176be6fed91"
            "0dfe3683d7ee7254f4e8ea6c5594cde7",
        ),
    ),
    dev_only=True,
)
# Detector class index -> canonical weapon class used by weapon_review.py.
WEAPON_CLASSES: Final = {0: "firearm", 1: "knife"}
REPEAT_SUPPRESSION_SECONDS: Final = 60.0
_MAX_CROPS: Final = 3


def decode_yolov8(
    output: Any,
    *,
    ratio: float,
    offset: tuple[float, float],
    region: tuple[float, float, float, float],
    frame_width: int,
    frame_height: int,
    minimum_confidence: float,
    class_map: dict[int, str] = WEAPON_CLASSES,
) -> list[ObjectDetection]:
    """Decode a YOLOv8 head (4 + classes) x N into full-frame normalized boxes.

    ``region`` is the crop (left, top, right, bottom) in frame pixels that was
    letterboxed; ``offset`` is unused padding kept for API symmetry.
    """

    import numpy

    rows = numpy.asarray(output)
    if rows.ndim != 2 or rows.shape[0] != 4 + len(class_map):
        raise ValueError("weapon detector output does not match its class map")
    scores = rows[4:]
    best = scores.argmax(axis=0)
    best_scores = scores[best, numpy.arange(scores.shape[1])]
    keep = best_scores >= minimum_confidence
    detections: list[ObjectDetection] = []
    for index in numpy.flatnonzero(keep):
        center_x, center_y, width, height = (float(value) for value in rows[:4, index])
        left = region[0] + (center_x - width / 2 - offset[0]) / ratio
        top = region[1] + (center_y - height / 2 - offset[1]) / ratio
        right = region[0] + (center_x + width / 2 - offset[0]) / ratio
        bottom = region[1] + (center_y + height / 2 - offset[1]) / ratio
        box = (
            min(1.0, max(0.0, left / frame_width)),
            min(1.0, max(0.0, top / frame_height)),
            min(1.0, max(0.0, right / frame_width)),
            min(1.0, max(0.0, bottom / frame_height)),
        )
        if box[0] < box[2] and box[1] < box[3]:
            detections.append(ObjectDetection(class_map[int(best[index])], round(float(best_scores[index]), 4), *box))
    return detections


def person_crops(
    people: Sequence[ObjectDetection], frame_width: int, frame_height: int, *, limit: int = _MAX_CROPS
) -> list[tuple[int, int, int, int]]:
    """Square-ish pixel regions around the largest people, enlarged 1.6x."""

    largest = sorted(people, key=lambda item: (item.right - item.left) * (item.bottom - item.top), reverse=True)[:limit]
    regions = []
    for person in largest:
        center_x = (person.left + person.right) / 2 * frame_width
        center_y = (person.top + person.bottom) / 2 * frame_height
        half = max((person.right - person.left) * frame_width, (person.bottom - person.top) * frame_height) * 0.8
        left, top = int(max(0, center_x - half)), int(max(0, center_y - half))
        right, bottom = int(min(frame_width, center_x + half)), int(min(frame_height, center_y + half))
        if right - left >= 32 and bottom - top >= 32:
            regions.append((left, top, right, bottom))
    return regions


@dataclass
class WeaponProcessor:
    """Runs the weapon model and confirms candidates per camera."""

    model: Any
    model_version: str
    minimum_confidence: float = 0.5
    input_size: int = 640
    _confirmations: dict[str, WeaponReviewConfirmation] = field(default_factory=dict)
    _sequences: dict[str, int] = field(default_factory=dict)
    _last_alert: dict[str, float] = field(default_factory=dict)

    @classmethod
    def from_config(cls, config: LocalConfig) -> "WeaponProcessor":
        if config.weapon_model is None:
            raise ValueError("models.weapon is not configured")
        verify_model_artifact(WEAPON_DEV_YOLOV8, {"onnx": config.weapon_model}, allow_dev_only=config.weapon_model_dev_only)
        import openvino

        core = openvino.Core()
        compiled = core.compile_model(core.read_model(str(config.weapon_model)), config.device)
        return cls(model=compiled, model_version=WEAPON_DEV_YOLOV8.model_version)

    def detect(self, frame: Any, people: Sequence[ObjectDetection]) -> list[ObjectDetection]:
        import numpy

        height, width = frame.shape[:2]
        regions = [(0, 0, width, height), *person_crops(people, width, height)]
        found: list[ObjectDetection] = []
        for left, top, right, bottom in regions:
            crop = frame[top:bottom, left:right]
            padded, ratio = letterbox(crop, self.input_size)
            # YOLOv8 expects RGB scaled to 0..1.
            tensor = padded[:, :, ::-1].transpose(2, 0, 1)[numpy.newaxis].astype(numpy.float32) / 255.0
            output = next(iter(self.model(tensor).values()))[0]
            found.extend(decode_yolov8(
                output, ratio=ratio, offset=(0.0, 0.0), region=(left, top, right, bottom),
                frame_width=width, frame_height=height, minimum_confidence=self.minimum_confidence,
            ))
        kept = non_max_suppression([(item.subject_class, item.confidence, (item.left, item.top, item.right, item.bottom)) for item in found])
        return [ObjectDetection(label, score, *box) for label, score, box in kept]

    def process(
        self,
        scope: tuple[str, str],
        camera_id: str,
        frame: Any,
        detections: Sequence[ObjectDetection],
        observed_at: datetime,
    ) -> list[AlertCandidate]:
        people = [item for item in detections if item.subject_class == "person"]
        if not people:
            return []
        weapons = self.detect(frame, people)
        return self.confirm(scope, camera_id, weapons, people, observed_at)

    def confirm(
        self,
        scope: tuple[str, str],
        camera_id: str,
        weapons: Sequence[ObjectDetection],
        people: Sequence[ObjectDetection],
        observed_at: datetime,
    ) -> list[AlertCandidate]:
        tenant_id, site_id = scope
        confirmation = self._confirmations.get(camera_id)
        if confirmation is None:
            confirmation = WeaponReviewConfirmation(tenant_id, site_id, camera_id, camera_id)
            self._confirmations[camera_id] = confirmation
        sequence = self._sequences.get(camera_id, 0) + 1
        self._sequences[camera_id] = sequence
        events = confirmation.confirm(WeaponCandidateFrame(
            tenant_id=tenant_id, site_id=site_id, camera_id=camera_id, zone_id=camera_id,
            sequence=sequence, observed_at=observed_at,
            candidates=tuple(
                WeaponCandidate(object_class=item.subject_class, confidence=item.confidence, left=item.left,
                                top=item.top, right=item.right, bottom=item.bottom, model_version=self.model_version)
                for item in weapons[:64]
            ),
        ))
        alerts: list[AlertCandidate] = []
        for event in events:
            moment = observed_at.timestamp()
            if moment - self._last_alert.get(camera_id, -1e12) < REPEAT_SUPPRESSION_SECONDS:
                continue
            self._last_alert[camera_id] = moment
            alerts.append(AlertCandidate(event=event, title="Possible weapon - review now", detections=tuple(people) + tuple(weapons)))
        return alerts


__all__ = ["WEAPON_DEV_YOLOV8", "WeaponProcessor", "decode_yolov8", "person_crops"]
