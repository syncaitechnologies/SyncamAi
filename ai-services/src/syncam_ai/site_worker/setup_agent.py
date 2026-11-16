"""Runs the dashboard's "Find cameras" tasks on the site box.

Tasks arrive with the box's regular poll and their results are posted back:

* ``scan``  - find recorders and cameras on this network.
* ``probe`` - with credentials encrypted for this box, find the working
  RTSP template and channels of one device and grab a thumbnail of each.
  The credentials are kept only in this box's encrypted vault.
* ``adopt`` - start reading the channels the user picked, as the cameras
  the dashboard created for them. No restart and no config file edits.

Only private (LAN) addresses are ever probed, so the dashboard cannot use a
site box to reach machines on the internet.
"""

from __future__ import annotations

import ipaddress
import logging
import threading
import uuid
from typing import Any, Callable, Mapping

from . import discovery
from .brands import FAMILIES, rtsp_url
from .capture import CameraStream, StreamSet
from .client import BackendError
from .setup_crypto import decrypt_credentials
from .vault import BoxVault

_LOG = logging.getLogger(__name__)
MAX_CHANNEL = 64


def _private_host(value: Any) -> str:
    try:
        ip = ipaddress.ip_address(str(value))
    except ValueError as error:
        raise ValueError("the device address must be an IP address") from error
    if ip.version != 4 or not ip.is_private or ip.is_loopback or ip.is_link_local:
        raise ValueError("only devices on the site's private network can be added")
    return str(ip)


def _port(value: Any) -> int:
    try:
        port = int(value)
    except (TypeError, ValueError) as error:
        raise ValueError("the RTSP port must be a number") from error
    if not 1 <= port <= 65535:
        raise ValueError("the RTSP port is out of range")
    return port


def vault_camera_sources(vault: BoxVault) -> dict[str, str]:
    """RTSP addresses of the cameras adopted through the dashboard."""

    try:
        data = vault.load()
    except (OSError, ValueError) as error:
        _LOG.warning("could not read the site box vault: %s", error)
        return {}
    devices: Mapping[str, Any] = data.get("devices") or {}
    sources: dict[str, str] = {}
    for camera_id, camera in (data.get("cameras") or {}).items():
        device = devices.get(camera.get("device_id"))
        if not device or device.get("family") not in FAMILIES:
            continue
        sources[camera_id] = rtsp_url(
            device["ip"], int(device["port"]), device["family"], int(camera["channel"]),
            device.get("username", ""), device.get("password", ""), stream=camera.get("stream", "sub"),
        )
    return sources


class SetupAgent:
    def __init__(
        self,
        backend: Any,
        vault: BoxVault,
        streams: StreamSet,
        *,
        discover: Callable[..., list[discovery.FoundDevice]] = discovery.discover,
        probe: Callable[..., discovery.ProbeResult] = discovery.probe_channels,
        thumbnail: Callable[[str], dict[str, Any] | None] | None = discovery.grab_thumbnail,
        make_stream: Callable[[str, str], Any] = CameraStream,
        run_async: bool = True,
    ) -> None:
        self._backend = backend
        self._vault = vault
        self._streams = streams
        self._discover = discover
        self._probe = probe
        self._thumbnail = thumbnail
        self._make_stream = make_stream
        self._run_async = run_async
        self._seen: set[str] = set()
        self._lock = threading.Lock()

    def handle(self, tasks: list[Mapping[str, Any]]) -> None:
        """Start every task not seen before (each runs once)."""

        for task in tasks:
            task_id = str(task.get("id", ""))
            with self._lock:
                if not task_id or task_id in self._seen:
                    continue
                self._seen.add(task_id)
            if self._run_async:
                threading.Thread(target=self.run, args=(task,), name=f"setup-{task_id[:8]}", daemon=True).start()
            else:
                self.run(task)

    def run(self, task: Mapping[str, Any]) -> None:
        task_id, kind = str(task.get("id")), str(task.get("type"))
        handlers = {"scan": self._scan, "probe": self._probe_device, "adopt": self._adopt}
        body: dict[str, Any]
        try:
            if kind not in handlers:
                raise ValueError(f"unknown setup task {kind!r}")
            _LOG.info("setup task %s started", kind)
            body = {"status": "done", "result": handlers[kind](task.get("payload") or {})}
        except ValueError as error:
            body = {"status": "failed", "error": str(error)}
        except Exception:  # never let a setup task stop the box
            _LOG.exception("setup task %s failed", kind)
            body = {"status": "failed", "error": "The site box hit an unexpected error; see its log."}
        try:
            self._backend.post_setup_result(task_id, body)
        except BackendError as error:
            _LOG.warning("could not report setup task %s: %s", kind, error)

    def _scan(self, _: Mapping[str, Any]) -> dict[str, Any]:
        return {"devices": [device.as_dict() for device in self._discover()]}

    def _probe_device(self, payload: Mapping[str, Any]) -> dict[str, Any]:
        device = payload.get("device") or {}
        host, port = _private_host(device.get("ip")), _port(device.get("rtsp_port", 554))
        family = device.get("family") if device.get("family") in FAMILIES else None
        private_key = self._vault.load().get("private_key_pem")
        if not private_key:
            raise ValueError("this site box was not paired with a code, so it cannot open encrypted passwords")
        credentials = decrypt_credentials(private_key, payload.get("credentials") or {})
        result = self._probe(host, port, credentials["username"], credentials["password"], family=family, thumbnail=self._thumbnail)
        if not result.channels:
            raise ValueError(result.error or "No working channels were found.")
        device_id = f"{host}:{port}"
        devices = dict(self._vault.load().get("devices") or {})
        devices[device_id] = {"ip": host, "port": port, "family": result.family, **credentials}
        self._vault.update(devices=devices)
        return {"device_id": device_id, **result.as_dict()}

    def _adopt(self, payload: Mapping[str, Any]) -> dict[str, Any]:
        data = self._vault.load()
        device_id = str(payload.get("device_id", ""))
        device = (data.get("devices") or {}).get(device_id)
        if not device:
            raise ValueError("check this device with its password first")
        picks = payload.get("cameras") or []
        if not isinstance(picks, list) or not picks:
            raise ValueError("choose at least one channel")
        cameras = dict(data.get("cameras") or {})
        adopted = []
        for pick in picks:
            try:
                camera_id = str(uuid.UUID(str(pick.get("camera_id"))))
                channel = int(pick.get("channel"))
            except (TypeError, ValueError, AttributeError) as error:
                raise ValueError("each camera needs a camera_id and a channel") from error
            if not 1 <= channel <= MAX_CHANNEL:
                raise ValueError("channel out of range")
            cameras[camera_id] = {"device_id": device_id, "channel": channel, "stream": "sub"}
            adopted.append(camera_id)
        self._vault.update(cameras=cameras)
        for camera_id, source in vault_camera_sources(self._vault).items():
            if camera_id in adopted and self._streams.add(camera_id, self._make_stream(camera_id, source)):
                _LOG.info("camera %s added from %s channel %s", camera_id, device_id, cameras[camera_id]["channel"])
        return {"adopted": adopted}
