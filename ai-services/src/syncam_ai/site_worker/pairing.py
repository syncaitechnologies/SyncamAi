"""Pair a new site box with the 8-character code from the dashboard.

The person installing the box types the code once. The box creates its own
P-256 key pair, sends the public key with the code, and receives a site key
that only this box holds. Both secrets go into the encrypted :class:`BoxVault`;
nothing has to be copied from the dashboard by hand. The public key lets the
dashboard encrypt camera passwords so that only this box can read them.
"""

from __future__ import annotations

import json
import socket
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable

from .client import BackendError, _validated_base_url
from .vault import BoxVault


@dataclass(frozen=True)
class PairingResult:
    site_id: str
    key_id: str


def new_box_keypair() -> tuple[str, str]:
    """A fresh P-256 key pair as (private PEM, public PEM)."""

    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ec

    private = ec.generate_private_key(ec.SECP256R1())
    private_pem = private.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    ).decode()
    public_pem = private.public_key().public_bytes(
        serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo
    ).decode()
    return private_pem, public_pem


def normalize_code(code: str) -> str:
    cleaned = "".join(char for char in code.upper() if char.isalnum())
    if len(cleaned) != 8:
        raise ValueError("a pairing code has 8 letters and digits, like K7Q4-XM92")
    return cleaned


def redeem_pairing_code(base_url: str, code: str, public_key: str, label: str, *, timeout: float = 30.0) -> dict[str, Any]:
    """Call ``POST /v1/site-ingest/pair``; returns the response data."""

    body = json.dumps({"code": code, "public_key": public_key, "label": label}).encode()
    request = urllib.request.Request(_validated_base_url(base_url) + "/v1/site-ingest/pair", data=body, method="POST")
    request.add_header("Content-Type", "application/json")
    request.add_header("Accept", "application/json")
    request.add_header("User-Agent", "syncam-site-worker/1")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        message = ""
        try:
            message = json.loads(error.read() or b"{}").get("error", {}).get("message", "")
        except (ValueError, AttributeError):
            pass
        raise BackendError(message or f"pairing failed with HTTP {error.code}", status=error.code, retryable=error.code == 429 or error.code >= 500) from None
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        raise BackendError(f"could not reach SyncCam: {getattr(error, 'reason', error)}") from None
    data = payload.get("data") if isinstance(payload, dict) else None
    if not isinstance(data, dict) or not str(data.get("site_key", "")).startswith("sck_"):
        raise BackendError("pairing response is invalid", retryable=False)
    return data


def pair_box(
    vault: BoxVault,
    base_url: str,
    code: str,
    *,
    label: str | None = None,
    redeem: Callable[..., dict[str, Any]] = redeem_pairing_code,
    keypair: Callable[[], tuple[str, str]] = new_box_keypair,
) -> PairingResult:
    """Pair this box and store its secrets in ``vault``."""

    code = normalize_code(code)
    private_pem, public_pem = keypair()
    name = (label or socket.gethostname() or "Site box").strip()[:120]
    data = redeem(base_url, code, public_pem, name)
    vault.save({
        "site_key": data["site_key"],
        "site_id": str(data.get("site_id", "")),
        "key_id": str(data.get("key_id", "")),
        "backend_url": base_url,
        "private_key_pem": private_pem,
        "public_key_pem": public_pem,
        "label": name,
    })
    return PairingResult(site_id=str(data.get("site_id", "")), key_id=str(data.get("key_id", "")))


def vault_site_key(vault: BoxVault) -> str | None:
    """The paired site key, if this box has been paired."""

    try:
        key = vault.load().get("site_key")
    except (OSError, ValueError):
        return None
    return key if isinstance(key, str) and key.startswith("sck_") else None
