"""Signed automatic updates for installed site boxes.

Once a day an installed box reads ``syncam-box-manifest.json`` and its
Ed25519 signature from the latest GitHub release. It acts only when

* the signature verifies against a public key built into this program
  (``release_keys.py``; with no key built in, updates are off),
* the manifest names a strictly newer version (no downgrades), and
* the installer URL is a release asset of the SyncCam repository.

It then downloads the installer into the box folder (SYSTEM and
Administrators only), checks its SHA-256 from the signed manifest and hands
it to Windows Task Scheduler as a one-off SYSTEM task, so the installer runs
outside this process and can stop and restart the box.
"""

from __future__ import annotations

import base64
import json
import logging
import re
import subprocess
import threading
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from hashlib import sha256
from pathlib import Path
from typing import Any, Callable

from .release_keys import RELEASE_PUBLIC_KEYS

_LOG = logging.getLogger(__name__)

REPOSITORY = "https://github.com/syncaitechnologies/SyncamAi"
MANIFEST_URL = f"{REPOSITORY}/releases/latest/download/syncam-box-manifest.json"
DOWNLOAD_PREFIX = f"{REPOSITORY}/releases/download/"
CHECK_INTERVAL_SECONDS = 24 * 3600
FIRST_CHECK_DELAY_SECONDS = 300
MAX_MANIFEST_BYTES = 64 * 1024
MAX_INSTALLER_BYTES = 1024 * 1024 * 1024
UPDATE_TASK = "SyncCam Box Update"
_VERSION = re.compile(r"^(0|[1-9]\d{0,3})\.(0|[1-9]\d{0,3})\.(0|[1-9]\d{0,3})$")
_INSTALLER_NAME = re.compile(r"^SyncCamSetup-[0-9.]+\.exe$")

Fetch = Callable[[str, int], bytes]


class UpdatesDisabled(Exception):
    pass


@dataclass(frozen=True)
class UpdateInfo:
    version: str
    url: str
    sha256: str
    notes: str = ""


def parse_version(text: str) -> tuple[int, int, int]:
    match = _VERSION.match(text.strip())
    if not match:
        raise ValueError(f"not a release version: {text!r}")
    return int(match[1]), int(match[2]), int(match[3])


def fetch_url(url: str, limit: int) -> bytes:
    if not url.startswith("https://"):
        raise ValueError("updates are fetched over https only")
    request = urllib.request.Request(url, headers={"User-Agent": "SyncCam-Box"})
    with urllib.request.urlopen(request, timeout=30) as response:  # noqa: S310 - https only
        body = response.read(limit + 1)
    if len(body) > limit:
        raise ValueError(f"{url} is larger than expected")
    return body


def verify_manifest(manifest: bytes, signature: bytes, public_keys: tuple[str, ...]) -> UpdateInfo:
    """The update the manifest describes, if one of ``public_keys`` signed it."""

    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    from cryptography.hazmat.primitives.serialization import load_pem_public_key

    try:
        raw_signature = base64.b64decode(signature.strip(), validate=True)
    except ValueError as error:
        raise ValueError("the update signature is not valid base64") from error
    for pem in public_keys:
        key = load_pem_public_key(pem.encode())
        if not isinstance(key, Ed25519PublicKey):
            raise ValueError("release keys must be Ed25519")
        try:
            key.verify(raw_signature, manifest)
            break
        except InvalidSignature:
            continue
    else:
        raise ValueError("the update manifest is not signed by a SyncCam release key")
    data: dict[str, Any] = json.loads(manifest)
    version, url, digest = str(data.get("version", "")), str(data.get("url", "")), str(data.get("sha256", "")).lower()
    parse_version(version)
    name = url.rsplit("/", 1)[-1]
    if not url.startswith(DOWNLOAD_PREFIX) or not _INSTALLER_NAME.match(name) or ".." in url:
        raise ValueError("the update URL is not a SyncCam release asset")
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("the update manifest has no valid SHA-256")
    return UpdateInfo(version=version, url=url, sha256=digest, notes=str(data.get("notes", ""))[:500])


def check_for_update(
    current: str,
    *,
    fetch: Fetch = fetch_url,
    public_keys: tuple[str, ...] = RELEASE_PUBLIC_KEYS,
) -> UpdateInfo | None:
    if not public_keys:
        raise UpdatesDisabled("this build has no release key, so automatic updates are off")
    manifest = fetch(MANIFEST_URL, MAX_MANIFEST_BYTES)
    signature = fetch(MANIFEST_URL + ".sig", 1024)
    info = verify_manifest(manifest, signature, public_keys)
    return info if parse_version(info.version) > parse_version(current) else None


def download_installer(info: UpdateInfo, directory: Path, *, fetch: Fetch = fetch_url) -> Path:
    directory.mkdir(parents=True, exist_ok=True)
    target = directory / info.url.rsplit("/", 1)[-1]
    body = fetch(info.url, MAX_INSTALLER_BYTES)
    if sha256(body).hexdigest() != info.sha256:
        raise ValueError("the downloaded installer does not match the signed SHA-256; it was not kept")
    partial = target.with_name(target.name + ".part")
    partial.write_bytes(body)
    partial.replace(target)
    return target


def update_task_xml(installer: Path, log_file: Path) -> str:
    """A Task Scheduler definition with no trigger: it runs only when started.

    Defined in XML rather than with ``/SC ONCE /ST`` because a one-time task
    needs a start time that is not in the past, and ``/SD`` dates depend on the
    computer's language settings.
    """

    from xml.sax.saxutils import escape

    arguments = f'/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG="{log_file}"'
    return f"""<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Installs a verified SyncCam Box update.</Description></RegistrationInfo>
  <Principals>
    <Principal id="System">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="System">
    <Exec>
      <Command>{escape(str(installer))}</Command>
      <Arguments>{escape(arguments)}</Arguments>
    </Exec>
  </Actions>
</Task>
"""


def schedule_installer(installer: Path, log_file: Path, *, run: Callable[..., Any] = subprocess.run) -> None:
    """Run the installer as a one-off SYSTEM task, outside this process."""

    definition = installer.with_name(installer.name + ".task.xml")
    definition.write_text(update_task_xml(installer, log_file), encoding="utf-16")
    try:
        run(["schtasks", "/Create", "/TN", UPDATE_TASK, "/XML", str(definition), "/F"], check=True, capture_output=True)
    finally:
        definition.unlink(missing_ok=True)
    run(["schtasks", "/Run", "/TN", UPDATE_TASK], check=True, capture_output=True)


class Updater(threading.Thread):
    """Checks once a day; installs only when ``install`` is set (installed boxes)."""

    def __init__(
        self,
        current: str,
        directory: Path,
        *,
        install: bool,
        check: Callable[[str], UpdateInfo | None] = check_for_update,
        download: Callable[[UpdateInfo, Path], Path] = download_installer,
        launch: Callable[[Path, Path], None] = schedule_installer,
        interval: float = CHECK_INTERVAL_SECONDS,
        first_delay: float = FIRST_CHECK_DELAY_SECONDS,
    ) -> None:
        super().__init__(name="updater", daemon=True)
        self._current = current
        self._directory = directory
        self._install = install
        self._check = check
        self._download = download
        self._launch = launch
        self._interval = interval
        self._first_delay = first_delay
        self._stopped = threading.Event()
        self._lock = threading.Lock()
        self._state: dict[str, Any] = {"message": f"Version {current}. Checking for updates shortly."}

    def state(self) -> dict[str, Any]:
        with self._lock:
            return dict(self._state)

    def _set(self, message: str, **extra: Any) -> None:
        with self._lock:
            self._state = {"message": message, "checked_at": datetime.now(timezone.utc).isoformat(), **extra}

    def check_once(self) -> None:
        try:
            info = self._check(self._current)
        except UpdatesDisabled as error:
            self._set(f"Version {self._current}. Automatic updates are off: {error}.")
            return
        except Exception as error:  # network, signature or format problems: try again tomorrow
            _LOG.warning("update check failed: %s", error)
            self._set(f"Version {self._current}. The last update check failed ({error}); it will be retried.")
            return
        if info is None:
            self._set(f"Version {self._current} is up to date.")
            return
        if not self._install:
            self._set(f"Version {info.version} is available (this copy is not installed, so it does not update itself).", latest=info.version)
            return
        try:
            installer = self._download(info, self._directory)
            _LOG.info("installing update %s from %s", info.version, info.url)
            self._set(f"Installing version {info.version}; the box restarts in a minute.", latest=info.version)
            self._launch(installer, self._directory / f"install-{info.version}.log")
        except Exception as error:
            _LOG.warning("update to %s failed: %s", info.version, error)
            self._set(f"Version {info.version} is available but could not be installed ({error}).", latest=info.version)

    def run(self) -> None:
        if self._stopped.wait(self._first_delay):
            return
        while True:
            self.check_once()
            if self._stopped.wait(self._interval):
                return

    def stop(self) -> None:
        self._stopped.set()
