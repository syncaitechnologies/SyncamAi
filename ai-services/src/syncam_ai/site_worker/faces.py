"""Staff face attendance that keeps biometric templates on the site box.

Models (Intel Open Model Zoo, Apache-2.0, run with OpenVINO):
face-detection-retail-0004 finds faces, landmarks-regression-retail-0009
places five landmarks used to align each face, and
face-reidentification-retail-0095 turns the aligned face into a 256-value
signature compared by cosine similarity.

Privacy boundary: a staff member must first be added in the dashboard, which
records who confirmed their written consent and when. Enrollment then runs on
the site box from that person's photos; the resulting signatures are stored
only in a gallery file encrypted with a key held on the box
(``SYNCAM_FACE_KEY``). Only "staff X was seen at camera Y at time Z" is sent to
the cloud. Unknown faces are ignored: they never raise alerts and nothing about
them is stored. Liveness (anti-spoofing) is not implemented yet, so a photo of
a staff member could be accepted; this must be added before production.
"""

from __future__ import annotations

import json
import logging
import os
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any, Final, Mapping, Sequence

from ..model_artifacts import ModelArtifact, verify_model_artifact
from .client import BackendError
from .config import LocalConfig
from .detector import ObjectDetection
from .runner import AlertCandidate

_LOG = logging.getLogger(__name__)
_OMZ = "https://storage.openvinotoolkit.org/repositories/open_model_zoo/2023.0/models_bin/1"


def _intel(name: str, xml: str, bin_: str) -> ModelArtifact:
    return ModelArtifact(identifier=f"intel-{name}-fp16", license="Apache-2.0", source=f"{_OMZ}/{name}/FP16/", files=(("xml", xml), ("bin", bin_)))


FACE_DETECTOR: Final = _intel(
    "face-detection-retail-0004",
    "a7f8d1d41998503c4f3cdd8c12275f04f1736e5142127edcb4c76c3e17188499390574095a5b2a9dd78d3d0f77d02034",
    "394185d3e42c34d7f9d43229ec8f5755c07e19fd6469d23883e71707fdd8eb66d90cbd62248db90ff3ba1c94adac599b",
)
FACE_LANDMARKS: Final = _intel(
    "landmarks-regression-retail-0009",
    "393eda465df2a886f7edd932e2f1cd6930ae91f85708f91b4cd60e411acfb86e9de9e083907901208e9e8af639846572",
    "0e6e830387f56783de313e0f7022b7808fd2e9e0b28b411a65608e2c1df0bcf939c65fb699fc51868df30232d6308515",
)
FACE_REID: Final = _intel(
    "face-reidentification-retail-0095",
    "689fb39e94bbd0d22cfeeb9292d0b91ac753424a7ee2c0555070e4038a00bfcaad473b47303012b9a393027dbad0dbce",
    "6d51703e854509dafdab9dfc6b932136d301d95be6d52c9c23b5a4f5de1ffec80e8d38b87e0e9556ad36f0dded1588ed",
)
# Five-point alignment target for face-reidentification-retail-0095 (OMZ).
_REFERENCE_LANDMARKS: Final = (
    (30.2946 / 96, 51.6963 / 112), (65.5318 / 96, 51.5014 / 112), (48.0252 / 96, 71.7366 / 112),
    (33.5493 / 96, 92.3655 / 112), (62.7299 / 96, 92.2041 / 112),
)
MIN_FACE_PIXELS: Final = 36
CONFIRMATIONS: Final = 2
CONFIRMATION_WINDOW_SECONDS: Final = 10.0
REPEAT_SECONDS: Final = 600.0
_SIGHTING_NAMESPACE: Final = uuid.UUID("0b0f3d55-3c74-4f5c-9b8f-91c0c5d2a1e4")


# ---------------------------------------------------------------- matching

def cosine(left: Sequence[float], right: Sequence[float]) -> float:
    dot = sum(a * b for a, b in zip(left, right))
    norm = (sum(a * a for a in left) ** 0.5) * (sum(b * b for b in right) ** 0.5)
    return 0.0 if norm == 0 else dot / norm


@dataclass(frozen=True)
class Match:
    staff_id: str
    similarity: float


def best_match(
    signature: Sequence[float],
    gallery: Mapping[str, Sequence[Sequence[float]]],
    *,
    threshold: float,
    margin: float = 0.05,
) -> Match | None:
    """The most similar enrolled person, if clearly above the threshold.

    The best score must also beat the next person by ``margin`` so that two
    look-alike staff members are never confused; ambiguous faces are ignored.
    """

    scores = sorted(
        ((max(cosine(signature, sample) for sample in samples), staff_id) for staff_id, samples in gallery.items() if samples),
        reverse=True,
    )
    if not scores or scores[0][0] < threshold:
        return None
    if len(scores) > 1 and scores[0][0] - scores[1][0] < margin:
        return None
    return Match(staff_id=scores[0][1], similarity=round(scores[0][0], 4))


# ---------------------------------------------------------------- gallery

class FaceGallery:
    """Encrypted on-box store of enrolled signatures (Fernet, AES-128 + HMAC)."""

    def __init__(self, path: Path, key: str) -> None:
        from cryptography.fernet import Fernet

        self._path = path
        self._fernet = Fernet(key.encode())

    @staticmethod
    def new_key() -> str:
        from cryptography.fernet import Fernet

        return Fernet.generate_key().decode()

    @classmethod
    def from_environment(cls, path: Path) -> "FaceGallery":
        key = os.environ.get("SYNCAM_FACE_KEY", "")
        if not key:
            raise ValueError("set SYNCAM_FACE_KEY (create one with: python -m syncam_ai.site_worker face-key)")
        return cls(path, key)

    def load(self) -> dict[str, dict[str, Any]]:
        if not self._path.exists():
            return {}
        data = json.loads(self._fernet.decrypt(self._path.read_bytes()))
        staff = data.get("staff", {})
        return staff if isinstance(staff, dict) else {}

    def save(self, staff: Mapping[str, Mapping[str, Any]]) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        payload = self._fernet.encrypt(json.dumps({"version": 1, "staff": staff}).encode())
        temporary = self._path.with_name(self._path.name + ".tmp")
        temporary.write_bytes(payload)
        os.replace(temporary, self._path)

    def enroll(self, staff_id: str, name: str, signatures: Sequence[Sequence[float]]) -> None:
        staff = self.load()
        staff[staff_id] = {"name": name, "signatures": [list(map(float, item)) for item in signatures]}
        self.save(staff)

    def forget(self, staff_id: str) -> bool:
        staff = self.load()
        removed = staff.pop(staff_id, None) is not None
        self.save(staff)
        return removed

    def signatures(self) -> dict[str, list[list[float]]]:
        return {staff_id: entry.get("signatures", []) for staff_id, entry in self.load().items()}


# ---------------------------------------------------------------- models

class FaceEngine:
    """Detects, aligns and signs faces with three verified OpenVINO models."""

    def __init__(self, models_dir: Path, *, device: str = "AUTO") -> None:
        import openvino

        core = openvino.Core()
        compiled = []
        for artifact, name in ((FACE_DETECTOR, "face-detection-retail-0004"), (FACE_LANDMARKS, "landmarks-regression-retail-0009"), (FACE_REID, "face-reidentification-retail-0095")):
            xml, bin_ = models_dir / f"{name}.xml", models_dir / f"{name}.bin"
            verify_model_artifact(artifact, {"xml": xml, "bin": bin_})
            compiled.append(core.compile_model(core.read_model(str(xml), str(bin_)), device))
        self._detector, self._landmarks, self._reid = compiled

    @staticmethod
    def _tensor(image: Any, size: tuple[int, int]) -> Any:
        import cv2
        import numpy

        return cv2.resize(image, size).transpose(2, 0, 1)[numpy.newaxis].astype(numpy.float32)

    def faces(self, image: Any, *, minimum_confidence: float = 0.6) -> list[tuple[int, int, int, int, float]]:
        """Face boxes in pixel coordinates of ``image``."""

        height, width = image.shape[:2]
        output = next(iter(self._detector(self._tensor(image, (300, 300))).values()))[0][0]
        boxes = []
        for _, label, confidence, left, top, right, bottom in output:
            if confidence < minimum_confidence or int(label) != 1:
                continue
            box = (int(max(0.0, left) * width), int(max(0.0, top) * height), int(min(1.0, right) * width), int(min(1.0, bottom) * height))
            if box[2] - box[0] >= MIN_FACE_PIXELS and box[3] - box[1] >= MIN_FACE_PIXELS:
                boxes.append((*box, float(confidence)))
        return boxes

    def signature(self, image: Any, box: tuple[int, int, int, int]) -> list[float]:
        import cv2
        import numpy

        left, top, right, bottom = box
        face = image[top:bottom, left:right]
        points = next(iter(self._landmarks(self._tensor(face, (48, 48))).values())).reshape(-1)
        source = numpy.array([[points[2 * i] * face.shape[1], points[2 * i + 1] * face.shape[0]] for i in range(5)], dtype=numpy.float32)
        target = numpy.array([[x * 128, y * 128] for x, y in _REFERENCE_LANDMARKS], dtype=numpy.float32)
        transform, _ = cv2.estimateAffinePartial2D(source, target)
        aligned = cv2.warpAffine(face, transform, (128, 128)) if transform is not None else cv2.resize(face, (128, 128))
        vector = next(iter(self._reid(self._tensor(aligned, (128, 128))).values())).reshape(-1)
        length = float(numpy.linalg.norm(vector)) or 1.0
        return [float(value) / length for value in vector]

    def largest_face_signature(self, image: Any) -> list[float] | None:
        faces = self.faces(image)
        if not faces:
            return None
        largest = max(faces, key=lambda face: (face[2] - face[0]) * (face[3] - face[1]))
        return self.signature(image, largest[:4])


# ---------------------------------------------------------------- runtime

@dataclass
class AttendanceProcessor:
    """Marks attendance when an enrolled staff member is seen at an entrance."""

    engine: Any
    gallery: Any
    backend: Any
    entrance_cameras: frozenset[str]
    threshold: float = 0.55
    clock: Any = time.monotonic
    _signatures: dict[str, list[list[float]]] = field(default_factory=dict)
    _active_staff: set[str] = field(default_factory=set)
    _loaded_at: float = -1e9
    _recent: dict[str, list[float]] = field(default_factory=dict)
    _last_marked: dict[str, float] = field(default_factory=dict)
    _pending: list[dict[str, object]] = field(default_factory=list)

    @classmethod
    def from_config(cls, config: LocalConfig, backend: Any) -> "AttendanceProcessor":
        if config.face_models_dir is None:
            raise ValueError("models.faces must point to the face model folder")
        return cls(
            engine=FaceEngine(config.face_models_dir, device=config.device),
            gallery=FaceGallery.from_environment(config.face_gallery),
            backend=backend,
            entrance_cameras=frozenset(config.entrance_cameras),
            threshold=config.face_match_threshold,
        )

    def _refresh(self) -> None:
        if self.clock() - self._loaded_at < 300:
            return
        self._loaded_at = self.clock()
        try:
            self._active_staff = {str(item["id"]) for item in self.backend.config().get("staff", [])}
        except (BackendError, KeyError, TypeError) as error:
            _LOG.warning("staff list refresh failed: %s", error)
        # Only staff who are still active (consent not withdrawn) are matched.
        self._signatures = {staff_id: samples for staff_id, samples in self.gallery.signatures().items() if staff_id in self._active_staff}

    def observe(self, camera_id: str, signatures: Sequence[Sequence[float]], observed_at: datetime) -> list[dict[str, object]]:
        """Turn face signatures from one frame into attendance sightings."""

        sightings: list[dict[str, object]] = []
        now = self.clock()
        for signature in signatures:
            match = best_match(signature, self._signatures, threshold=self.threshold)
            if match is None:
                continue
            hits = [moment for moment in self._recent.get(match.staff_id, []) if now - moment <= CONFIRMATION_WINDOW_SECONDS] + [now]
            self._recent[match.staff_id] = hits
            if len(hits) < CONFIRMATIONS or now - self._last_marked.get(match.staff_id, -1e12) < REPEAT_SECONDS:
                continue
            self._last_marked[match.staff_id] = now
            stamp = observed_at.isoformat().replace("+00:00", "Z")
            sightings.append({
                "id": str(uuid.uuid5(_SIGHTING_NAMESPACE, f"{match.staff_id}:{camera_id}:{stamp}")),
                "staff_id": match.staff_id, "camera_id": camera_id, "occurred_at": stamp,
                "confidence": max(0.0, min(1.0, match.similarity)),
            })
        return sightings

    def flush(self) -> None:
        remaining = []
        for sighting in self._pending:
            try:
                self.backend.post_attendance(sighting)
            except BackendError as error:
                if error.retryable:
                    remaining.append(sighting)
                else:
                    _LOG.error("attendance sighting rejected: %s", error)
        self._pending = remaining[-200:]

    def process(
        self,
        scope: tuple[str, str],
        camera_id: str,
        frame: Any,
        detections: Sequence[ObjectDetection],
        observed_at: datetime,
    ) -> list[AlertCandidate]:
        if camera_id not in self.entrance_cameras:
            return []
        people = [item for item in detections if item.subject_class == "person"]
        if not people:
            return []
        self._refresh()
        if not self._signatures:
            return []
        height, width = frame.shape[:2]
        signatures = []
        for person in sorted(people, key=lambda item: item.bottom - item.top, reverse=True)[:3]:
            # Faces are in the upper part of a person box; crop it for detail.
            left, right = int(person.left * width), int(person.right * width)
            top = int(person.top * height)
            bottom = int((person.top + (person.bottom - person.top) * 0.5) * height)
            region = frame[max(0, top):max(top + 1, bottom), max(0, left):max(left + 1, right)]
            if region.size == 0:
                continue
            for face in self.engine.faces(region):
                signatures.append(self.engine.signature(region, face[:4]))
        self._pending.extend(self.observe(camera_id, signatures, observed_at))
        if self._pending:
            self.flush()
        return []
