"""The site box's local configuration file.

Only what must stay on the premises lives here: the backend URL, which
camera ID maps to which RTSP address (with its password), model paths and
tuning. The site key is read from an environment variable by default so it is
not written into a file. Zones, schedules and camera names come from the
dashboard through the backend.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping
from uuid import UUID


@dataclass(frozen=True)
class LocalConfig:
    backend_url: str
    site_key: str
    cameras: Mapping[str, str]
    detector_model: Path
    device: str = "AUTO"
    fps: float = 4.0
    min_confidence: float = 0.45
    spool_dir: Path = Path("syncam-spool")
    weapon_model: Path | None = None
    weapon_model_dev_only: bool = False
    face_models_dir: Path | None = None
    entrance_cameras: tuple[str, ...] = ()
    face_gallery: Path = Path("syncam-faces.gallery")
    face_match_threshold: float = 0.55


def _path(value: object, field_name: str) -> Path:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{field_name} must be a file path")
    return Path(os.path.expandvars(value.strip()))


def parse_local_config(raw: Mapping[str, Any], *, environ: Mapping[str, str] | None = None) -> LocalConfig:
    environment = os.environ if environ is None else environ
    backend_url = raw.get("backend_url")
    if not isinstance(backend_url, str) or not backend_url.strip():
        raise ValueError("backend_url is required")
    key_env = str(raw.get("site_key_env", "SYNCAM_SITE_KEY"))
    site_key = environment.get(key_env) or raw.get("site_key")
    if not isinstance(site_key, str) or not site_key.strip():
        raise ValueError(f"set the site key in the {key_env} environment variable")
    cameras = raw.get("cameras")
    if not isinstance(cameras, Mapping) or not cameras:
        raise ValueError("cameras must map camera IDs from the dashboard to RTSP addresses")
    parsed_cameras: dict[str, str] = {}
    for camera_id, source in cameras.items():
        try:
            canonical = str(UUID(str(camera_id)))
        except ValueError as error:
            raise ValueError(f"camera ID {camera_id!r} must be the UUID shown in the dashboard") from error
        if not isinstance(source, str) or not source.strip():
            raise ValueError(f"camera {canonical} needs an RTSP address, file or webcam index")
        parsed_cameras[canonical] = os.path.expandvars(source.strip())
    models = raw.get("models")
    if not isinstance(models, Mapping):
        raise ValueError("models.detector must point to the YOLOX model file")
    fps = float(raw.get("fps", 4.0))
    min_confidence = float(raw.get("min_confidence", 0.45))
    if not 0.5 <= fps <= 15:
        raise ValueError("fps must be between 0.5 and 15")
    if not 0.1 <= min_confidence <= 0.95:
        raise ValueError("min_confidence must be between 0.1 and 0.95")
    weapon = models.get("weapon")
    faces = models.get("faces")
    entrance = raw.get("entrance_cameras", [])
    if not isinstance(entrance, list) or any(str(item) not in parsed_cameras for item in entrance):
        raise ValueError("entrance_cameras must list configured camera IDs")
    threshold = float(raw.get("face_match_threshold", 0.55))
    if not 0.3 <= threshold <= 0.95:
        raise ValueError("face_match_threshold must be between 0.3 and 0.95")
    spool_dir = _path(raw.get("spool_dir", "syncam-spool"), "spool_dir")
    gallery = raw.get("face_gallery")
    return LocalConfig(
        backend_url=backend_url.strip(),
        site_key=site_key.strip(),
        cameras=parsed_cameras,
        detector_model=_path(models.get("detector"), "models.detector"),
        device=str(raw.get("device", "AUTO")),
        fps=fps,
        min_confidence=min_confidence,
        spool_dir=spool_dir,
        weapon_model=_path(weapon, "models.weapon") if weapon else None,
        weapon_model_dev_only=bool(raw.get("allow_dev_models", False)),
        face_models_dir=_path(faces, "models.faces") if faces else None,
        entrance_cameras=tuple(str(item) for item in entrance),
        face_gallery=_path(gallery, "face_gallery") if gallery else spool_dir.parent / "syncam-faces.gallery",
        face_match_threshold=threshold,
    )


def load_local_config(path: Path) -> LocalConfig:
    return parse_local_config(json.loads(path.read_text(encoding="utf-8")))
