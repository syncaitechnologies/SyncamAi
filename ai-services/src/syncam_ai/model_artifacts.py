"""Checksum and license verification for external model files.

Model weights never live in the repository. Every model the site worker loads
is pinned here by SHA-384 and license. Production use is limited to the
licenses in ``licenses/allowlist.json`` (currently Apache-2.0); a model marked
``dev_only`` loads only when the operator explicitly allows development models.
"""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha384
from pathlib import Path
from typing import Final, Mapping

APPROVED_MODEL_LICENSES: Final = frozenset({"Apache-2.0"})


@dataclass(frozen=True)
class ModelArtifact:
    """An external model and the immutable hashes of each of its files."""

    identifier: str
    license: str
    source: str
    files: tuple[tuple[str, str], ...]
    dev_only: bool = False

    @property
    def model_version(self) -> str:
        return self.identifier


YOLOX_S: Final = ModelArtifact(
    identifier="yolox-s-coco-0.1.1rc0",
    license="Apache-2.0",
    source="https://github.com/Megvii-BaseDetection/YOLOX/releases/download/0.1.1rc0/yolox_s.onnx",
    files=(
        (
            "onnx",
            "58e4d3a9701466eb6e3158c17dfe9a11dfe97c7dbaa033bc4f8fe01c6e8c637c"
            "3b4972ac764c8709fcae291b1fc72f19",
        ),
    ),
)


def verify_model_artifact(
    artifact: ModelArtifact,
    paths: Mapping[str, Path],
    *,
    allow_dev_only: bool = False,
) -> None:
    """Fail closed unless every file matches its pinned hash and license policy."""

    if artifact.dev_only:
        if not allow_dev_only:
            raise ValueError(
                f"{artifact.identifier} is a development-only model; "
                "enable development models explicitly to load it"
            )
    elif artifact.license not in APPROVED_MODEL_LICENSES:
        raise ValueError(f"{artifact.identifier} license {artifact.license} is not approved")
    expected = dict(artifact.files)
    if set(paths) != set(expected):
        raise ValueError(f"{artifact.identifier} needs exactly these files: {sorted(expected)}")
    for role, path in paths.items():
        if not path.is_file():
            raise ValueError(f"{artifact.identifier} {role} file is missing: {path}")
        digest = _sha384(path)
        if digest != expected[role]:
            raise ValueError(f"{artifact.identifier} {role} checksum does not match its pin")


def _sha384(path: Path) -> str:
    digest = sha384()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()
