"""A small status page for the person installing the box.

Served on http://127.0.0.1:8790 only (never on the network): is the box
paired, which cameras send frames, when it last reached SyncCam and the last
alert. It shows no addresses, passwords or keys. Requests whose Host header is
not this loopback address are refused, so a web page cannot read it through
DNS rebinding.
"""

from __future__ import annotations

import json
import logging
import threading
from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable, Mapping

_LOG = logging.getLogger(__name__)
StatusProvider = Callable[[], Mapping[str, Any]]


def _age(seconds: Any) -> str:
    if seconds is None:
        return "no frames yet"
    value = float(seconds)
    if value < 90:
        return f"frame {value:.0f} s ago"
    return f"frame {value / 60:.0f} min ago"


def render_status_html(status: Mapping[str, Any]) -> str:
    worker = status.get("worker") or {}
    update = status.get("update") or {}
    cameras = worker.get("cameras") or []
    paired = status.get("paired")
    connected = bool(worker.get("connected"))
    last_contact = worker.get("connected_at")
    offline_hint = (
        f"Last reached SyncCam at {escape(str(last_contact))}. Check the internet connection of this computer."
        if last_contact
        else "Check the internet connection of this computer."
    )
    rows = "".join(
        f"<tr><td>{escape(str(camera.get('name') or camera.get('id', '')))}</td>"
        f"<td class=\"{'ok' if camera.get('last_frame_seconds') is not None and float(camera['last_frame_seconds']) < 15 else 'bad'}\">"
        f"{escape(_age(camera.get('last_frame_seconds')))}</td></tr>"
        for camera in cameras
    ) or '<tr><td colspan="2">No cameras yet. In the dashboard open Configuration &gt; Cameras &gt; Find cameras.</td></tr>'
    checks = []
    problem = status.get("problem")
    if problem:
        # Something stops the box from starting at all (for example its
        # settings cannot be read); say so instead of showing a blank page.
        checks.append(("Site box started", False, escape(str(problem))))
    if paired is not None:
        checks.append(("Paired with SyncCam", bool(paired), "Run the installer again, or: syncam-box.exe pair --code &lt;code&gt;"))
    checks.append(("Connected to SyncCam", connected, offline_hint))
    if "keeps_awake" in status:
        checks.append((
            "Keeping this computer awake",
            bool(status.get("keeps_awake")),
            "If this computer goes to sleep, alerts stop. In Windows Settings &gt; System &gt; Power, set Sleep to Never.",
        ))
    if worker.get("trial_ended"):
        checks.append(("SyncCam plan active", False, "The free trial of this workspace has ended, so alerts are not sent. Contact SyncCam to keep using it."))
    check_rows = "".join(
        f"<li class=\"{'ok' if passed else 'bad'}\">{'&#10003;' if passed else '&#10007;'} {label}"
        f"{'' if passed else ' <small>' + hint + '</small>'}</li>"
        for label, passed, hint in checks
    )
    site = escape(str(worker.get("site") or "not connected yet"))
    last_alert = escape(str(worker.get("last_alert_at") or "none since start"))
    return f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="5"><title>SyncCam Box</title>
<style>
:root {{ color-scheme: light dark; --ok: #1a7f37; --bad: #cf222e; }}
body {{ font: 15px/1.5 system-ui, sans-serif; max-width: 720px; margin: 24px auto; padding: 0 16px; }}
h1 {{ font-size: 22px; margin-bottom: 4px; }} .muted {{ opacity: .7; }}
ul {{ list-style: none; padding: 0; }} li {{ margin: 6px 0; }} small {{ opacity: .8; }}
table {{ width: 100%; border-collapse: collapse; }} td {{ padding: 6px 4px; border-bottom: 1px solid #8884; }}
.ok {{ color: var(--ok); }} .bad {{ color: var(--bad); }}
</style></head><body>
<h1>SyncCam Box</h1>
<p class="muted">Site: <strong>{site}</strong> &middot; version {escape(str(status.get("version", "")))}</p>
<ul>{check_rows}</ul>
<h2>Cameras</h2>
<table>{rows}</table>
<h2>Alerts</h2>
<p>Last alert: {last_alert}. Sent since start: {int(worker.get("alerts_queued") or 0)}. Waiting to be delivered: {int(worker.get("alerts_pending") or 0)}.</p>
<h2>Updates</h2>
<p>{escape(str(update.get("message") or "Automatic updates are off."))}</p>
<p class="muted">This page refreshes every 5 seconds and is only visible on this computer.</p>
</body></html>
"""


class StatusServer:
    def __init__(self, provider: StatusProvider, *, port: int, host: str = "127.0.0.1") -> None:
        if host not in {"127.0.0.1", "::1"}:
            raise ValueError("the status page is served on the loopback address only")
        self._provider = provider
        self._server = ThreadingHTTPServer((host, port), self._handler())
        self._server.daemon_threads = True
        self._thread = threading.Thread(target=self._server.serve_forever, name="status-page", daemon=True)

    @property
    def port(self) -> int:
        return int(self._server.server_address[1])

    def start(self) -> None:
        self._thread.start()

    def stop(self) -> None:
        self._server.shutdown()
        self._server.server_close()

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        server = self

        class Handler(BaseHTTPRequestHandler):
            server_version = "SyncCamBox"
            sys_version = ""

            def log_message(self, format: str, *args: Any) -> None:  # noqa: A002 - stdlib signature
                return

            def _send(self, status: int, body: bytes, content_type: str) -> None:
                self.send_response(status)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("X-Frame-Options", "DENY")
                self.send_header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
                self.end_headers()
                self.wfile.write(body)

            def do_GET(self) -> None:  # noqa: N802 - stdlib name
                allowed = {f"127.0.0.1:{server.port}", f"localhost:{server.port}", f"[::1]:{server.port}"}
                if (self.headers.get("Host") or "").lower() not in allowed:
                    self._send(403, b"forbidden\n", "text/plain; charset=utf-8")
                    return
                path = self.path.split("?", 1)[0]
                if path not in {"/", "/status.json"}:
                    self._send(404, b"not found\n", "text/plain; charset=utf-8")
                    return
                try:
                    status = dict(server._provider())
                except Exception:  # the page must never take the box down
                    _LOG.exception("status page failed")
                    self._send(500, b"status unavailable\n", "text/plain; charset=utf-8")
                    return
                if path == "/status.json":
                    self._send(200, json.dumps(status, default=str).encode(), "application/json")
                else:
                    self._send(200, render_status_html(status).encode(), "text/html; charset=utf-8")

        return Handler
