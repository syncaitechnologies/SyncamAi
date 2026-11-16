"""Download the detection model on first run and check it against its pin.

The installer does not ship model weights. The box downloads YOLOX-s from its
upstream release and keeps it only if the SHA-384 matches the pin in
``syncam_ai.model_artifacts``; anything else is deleted.
"""

from __future__ import annotations

import urllib.request
from hashlib import sha384
from pathlib import Path
from typing import Callable

from ..model_artifacts import YOLOX_S, ModelArtifact

MAX_MODEL_BYTES = 300 * 1024 * 1024
Downloader = Callable[[str, Path], None]


def _sha384(path: Path) -> str:
    digest = sha384()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def download_file(url: str, target: Path, *, timeout: float = 60.0, limit: int = MAX_MODEL_BYTES) -> None:
    if not url.startswith("https://"):
        raise ValueError("models are downloaded over https only")
    request = urllib.request.Request(url, headers={"User-Agent": "SyncCam-Box"})
    written = 0
    with urllib.request.urlopen(request, timeout=timeout) as response, target.open("wb") as handle:  # noqa: S310 - https only
        for chunk in iter(lambda: response.read(1 << 20), b""):
            written += len(chunk)
            if written > limit:
                raise ValueError(f"download from {url} is larger than expected")
            handle.write(chunk)


def fetch_model(target: Path, artifact: ModelArtifact = YOLOX_S, *, download: Downloader = download_file) -> bool:
    """Make sure ``target`` holds ``artifact``. Returns True when it was downloaded now."""

    expected = dict(artifact.files)["onnx"]
    if target.is_file() and _sha384(target) == expected:
        return False
    target.parent.mkdir(parents=True, exist_ok=True)
    partial = target.with_name(target.name + ".part")
    try:
        download(artifact.source, partial)
        if _sha384(partial) != expected:
            raise ValueError(f"{artifact.identifier} download does not match its pinned checksum; it was deleted")
        partial.replace(target)
    finally:
        partial.unlink(missing_ok=True)
    return True
