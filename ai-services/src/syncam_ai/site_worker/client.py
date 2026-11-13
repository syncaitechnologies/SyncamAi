"""HTTPS client for the control plane's site-ingest API (standard library only)."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
import uuid
from typing import Any, Mapping


class BackendError(Exception):
    """A failed call. ``retryable`` is False for permanent rejections."""

    def __init__(self, message: str, *, status: int | None = None, retryable: bool = True) -> None:
        super().__init__(message)
        self.status = status
        self.retryable = retryable


def _validated_base_url(base_url: str) -> str:
    parsed = urllib.parse.urlsplit(base_url.strip())
    local = parsed.hostname in {"localhost", "127.0.0.1", "::1"}
    if parsed.scheme not in {"https", "http"} or (parsed.scheme == "http" and not local):
        raise ValueError("backend_url must use HTTPS (HTTP is allowed only for localhost)")
    if parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError("backend_url must not contain credentials, a query or a fragment")
    return base_url.strip().rstrip("/")


class BackendClient:
    """Calls ``/v1/site-ingest/*`` with the site key. Never logs the key."""

    def __init__(self, base_url: str, site_key: str, *, timeout: float = 30.0) -> None:
        if not site_key.startswith("sck_"):
            raise ValueError("site key must start with sck_; create one in the dashboard")
        self._base_url = _validated_base_url(base_url)
        self._site_key = site_key
        self._timeout = timeout

    def _request(
        self,
        method: str,
        path: str,
        *,
        body: bytes | None = None,
        content_type: str | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> Any:
        request = urllib.request.Request(self._base_url + path, data=body, method=method)
        request.add_header("Authorization", f"Bearer {self._site_key}")
        request.add_header("Accept", "application/json")
        request.add_header("User-Agent", "syncam-site-worker/1")
        if content_type:
            request.add_header("Content-Type", content_type)
        for name, value in (headers or {}).items():
            request.add_header(name, value)
        try:
            with urllib.request.urlopen(request, timeout=self._timeout) as response:
                payload = response.read()
        except urllib.error.HTTPError as error:
            retryable = error.code in {408, 425, 429} or error.code >= 500
            detail = ""
            try:
                detail = json.loads(error.read() or b"{}").get("error", {}).get("message", "")
            except (ValueError, AttributeError):
                pass
            raise BackendError(f"{method} {path} failed with HTTP {error.code} {detail}".strip(), status=error.code, retryable=retryable) from None
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            raise BackendError(f"{method} {path} failed: {getattr(error, 'reason', error)}") from None
        if not payload:
            return None
        try:
            return json.loads(payload)
        except ValueError as error:
            raise BackendError(f"{method} {path} returned invalid JSON") from error

    def config(self) -> dict[str, Any]:
        response = self._request("GET", "/v1/site-ingest/config")
        data = response.get("data") if isinstance(response, dict) else None
        if not isinstance(data, dict):
            raise BackendError("site configuration response is invalid", retryable=False)
        return data

    def upload_evidence(self, jpeg: bytes) -> str:
        response = self._request("POST", "/v1/site-ingest/evidence", body=jpeg, content_type="image/jpeg")
        ref = (response or {}).get("data", {}).get("ref")
        if not isinstance(ref, str) or not ref:
            raise BackendError("evidence upload response is invalid", retryable=False)
        return ref

    def put_reference(self, camera_id: str, jpeg: bytes) -> None:
        self._request("PUT", f"/v1/site-ingest/cameras/{urllib.parse.quote(camera_id)}/reference", body=jpeg, content_type="image/jpeg")

    def post_event(self, event: Mapping[str, object]) -> None:
        body = json.dumps(event, separators=(",", ":")).encode()
        self._request(
            "POST",
            "/v1/site-ingest/events",
            body=body,
            content_type="application/json",
            headers={"X-SentinelVision-Request-ID": str(uuid.uuid4())},
        )

    def heartbeat(self, cameras: Mapping[str, Mapping[str, object]]) -> None:
        body = json.dumps({"cameras": cameras}, separators=(",", ":")).encode()
        self._request("POST", "/v1/site-ingest/heartbeat", body=body, content_type="application/json")
