"""Where an installed site box keeps its files.

``SyncCamSetup.exe`` installs the program under Program Files and keeps
everything that changes in one folder, ``%ProgramData%\\SyncCam`` on Windows:

    syncam-site.json    backend URL, model paths and tuning (no secrets)
    syncam-box.vault    site key, box private key, recorder passwords (DPAPI)
    models/             detection model, downloaded and checked by SHA-384
    spool/              alerts waiting to be delivered
    logs/               rotating logs
    updates/            verified installers waiting to run

The installer restricts the folder to SYSTEM and Administrators, because a
machine-scope DPAPI vault can be opened by any account on the computer.
``SYNCAM_HOME`` overrides the location (used by tests and development).
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any, Mapping

DEFAULT_BACKEND = "https://syncam-api.onrender.com"
CONFIG_NAME = "syncam-site.json"
VAULT_NAME = "syncam-box.vault"
STATUS_PORT = 8790


def box_home(environ: Mapping[str, str] | None = None) -> Path:
    environment = os.environ if environ is None else environ
    override = environment.get("SYNCAM_HOME", "").strip()
    if override:
        return Path(override)
    if os.name == "nt":
        return Path(environment.get("PROGRAMDATA") or r"C:\ProgramData") / "SyncCam"
    return Path("/var/lib/syncam")


def default_config_path(environ: Mapping[str, str] | None = None) -> Path:
    return box_home(environ) / CONFIG_NAME


def default_config(home: Path, backend_url: str = DEFAULT_BACKEND) -> dict[str, Any]:
    """A config for a box whose cameras come from "Find cameras" in the dashboard."""

    return {
        "backend_url": backend_url,
        "models": {"detector": (home / "models" / "yolox_s.onnx").as_posix()},
        "spool_dir": (home / "spool").as_posix(),
        "vault": (home / VAULT_NAME).as_posix(),
        "device": "AUTO",
        "fps": 4,
        "min_confidence": 0.45,
        "status_port": STATUS_PORT,
        "auto_update": True,
    }


def ensure_home(home: Path, backend_url: str = DEFAULT_BACKEND) -> Path:
    """Create the box folder and its config (an existing config is kept as is)."""

    if not backend_url.startswith("https://") and not backend_url.startswith("http://127.0.0.1"):
        raise ValueError("the backend URL must use https")
    for folder in (home, home / "models", home / "spool", home / "logs", home / "updates"):
        folder.mkdir(parents=True, exist_ok=True)
    config = home / CONFIG_NAME
    if not config.exists():
        temporary = config.with_suffix(".tmp")
        temporary.write_text(json.dumps(default_config(home, backend_url), indent=2) + "\n", encoding="utf-8")
        temporary.replace(config)
    return config
