"""End-to-end encryption of camera passwords from the dashboard to one box.

The dashboard encrypts ``{"username": ..., "password": ...}`` with the box's
public key using only browser WebCrypto primitives:

    ephemeral ECDH P-256 key  x  box public key  ->  32-byte shared secret
    HKDF-SHA256(secret, salt, info=INFO)        ->  AES-256-GCM key
    AES-256-GCM(key, iv, plaintext)             ->  ciphertext + tag

The envelope carries the ephemeral public key (uncompressed point), salt, IV
and ciphertext, all base64. The API only ever relays the envelope, so the
cloud never sees a camera password, and no other box can open it.
"""

from __future__ import annotations

import base64
import json
import os
from typing import Any, Mapping

INFO = b"syncam-camera-credentials-v1"


def _b64(data: bytes) -> str:
    return base64.b64encode(data).decode()


def _unb64(value: Any, name: str, size: int | None = None) -> bytes:
    if not isinstance(value, str):
        raise ValueError(f"credential envelope field {name} is missing")
    try:
        data = base64.b64decode(value, validate=True)
    except ValueError as error:
        raise ValueError(f"credential envelope field {name} is not base64") from error
    if size is not None and len(data) != size:
        raise ValueError(f"credential envelope field {name} has the wrong length")
    return data


def _derive(private_key: Any, peer_public: Any, salt: bytes) -> bytes:
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.hazmat.primitives.kdf.hkdf import HKDF

    secret = private_key.exchange(ec.ECDH(), peer_public)
    return HKDF(algorithm=hashes.SHA256(), length=32, salt=salt, info=INFO).derive(secret)


def encrypt_credentials(
    box_public_pem: str,
    credentials: Mapping[str, str],
    *,
    ephemeral_private: Any = None,
    salt: bytes | None = None,
    iv: bytes | None = None,
) -> dict[str, Any]:
    """Python twin of the dashboard's encryption (tests and test vectors)."""

    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM

    box_public = serialization.load_pem_public_key(box_public_pem.encode())
    ephemeral = ephemeral_private or ec.generate_private_key(ec.SECP256R1())
    salt = salt if salt is not None else os.urandom(16)
    iv = iv if iv is not None else os.urandom(12)
    key = _derive(ephemeral, box_public, salt)
    plaintext = json.dumps(dict(credentials), separators=(",", ":")).encode()
    point = ephemeral.public_key().public_bytes(serialization.Encoding.X962, serialization.PublicFormat.UncompressedPoint)
    return {"v": 1, "epk": _b64(point), "salt": _b64(salt), "iv": _b64(iv), "ct": _b64(AESGCM(key).encrypt(iv, plaintext, None))}


def decrypt_credentials(box_private_pem: str, envelope: Mapping[str, Any]) -> dict[str, str]:
    """Open an envelope addressed to this box; raises ValueError if it is not."""

    from cryptography.exceptions import InvalidTag
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM

    if envelope.get("v") != 1:
        raise ValueError("unsupported credential envelope version")
    epk = _unb64(envelope.get("epk"), "epk", 65)
    salt = _unb64(envelope.get("salt"), "salt")
    iv = _unb64(envelope.get("iv"), "iv", 12)
    ciphertext = _unb64(envelope.get("ct"), "ct")
    private = serialization.load_pem_private_key(box_private_pem.encode(), password=None)
    try:
        peer = ec.EllipticCurvePublicKey.from_encoded_point(ec.SECP256R1(), epk)
        plaintext = AESGCM(_derive(private, peer, salt)).decrypt(iv, ciphertext, None)
    except (InvalidTag, ValueError) as error:
        raise ValueError("these camera credentials were not encrypted for this site box") from error
    data = json.loads(plaintext)
    if not isinstance(data, dict) or not isinstance(data.get("username"), str) or not isinstance(data.get("password"), str):
        raise ValueError("camera credentials must have a username and a password")
    return {"username": data["username"], "password": data["password"]}
