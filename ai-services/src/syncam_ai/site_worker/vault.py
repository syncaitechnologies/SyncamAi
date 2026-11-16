"""Encrypted on-box storage for the site box's secrets.

Holds the site key and the box's private key (later also camera passwords).
On Windows the file is protected with DPAPI in machine scope: Windows encrypts
it with this computer's own credentials, so there is no key to manage and the
background service can read it, but the file is useless on another computer.
Elsewhere it is encrypted with Fernet and a random key kept beside it in a
file readable only by its owner.
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path
from typing import Any, Mapping

_DPAPI_MAGIC = b"SCV1"  # SyncCam vault, DPAPI
_FERNET_MAGIC = b"SCF1"  # SyncCam vault, Fernet


def default_vault_path(config_path: Path) -> Path:
    return config_path.with_name("syncam-box.vault")


def _dpapi(data: bytes, *, protect: bool) -> bytes:  # pragma: no cover - Windows only
    import ctypes
    from ctypes import wintypes

    class DataBlob(ctypes.Structure):
        _fields_ = [("cbData", wintypes.DWORD), ("pbData", ctypes.POINTER(ctypes.c_char))]

    crypt32 = ctypes.windll.crypt32
    kernel32 = ctypes.windll.kernel32
    buffer = ctypes.create_string_buffer(data, len(data))
    source = DataBlob(len(data), ctypes.cast(buffer, ctypes.POINTER(ctypes.c_char)))
    result = DataBlob()
    local_machine = 0x4  # CRYPTPROTECT_LOCAL_MACHINE
    ui_forbidden = 0x1  # CRYPTPROTECT_UI_FORBIDDEN
    function = crypt32.CryptProtectData if protect else crypt32.CryptUnprotectData
    ok = function(ctypes.byref(source), None, None, None, None, local_machine | ui_forbidden, ctypes.byref(result))
    if not ok:
        raise OSError("Windows could not " + ("protect" if protect else "unprotect") + " the site box vault")
    try:
        return ctypes.string_at(result.pbData, result.cbData)
    finally:
        kernel32.LocalFree(result.pbData)


class BoxVault:
    """A small JSON document stored encrypted at ``path``."""

    def __init__(self, path: Path, *, use_dpapi: bool | None = None) -> None:
        self.path = path
        self._use_dpapi = sys.platform == "win32" if use_dpapi is None else use_dpapi

    @property
    def _key_path(self) -> Path:
        return self.path.with_name(self.path.name + ".key")

    def exists(self) -> bool:
        return self.path.is_file()

    def _fernet(self, create: bool) -> Any:
        from cryptography.fernet import Fernet

        if self._key_path.is_file():
            return Fernet(self._key_path.read_bytes().strip())
        if not create:
            raise ValueError(f"the vault key {self._key_path} is missing")
        key = Fernet.generate_key()
        self._key_path.parent.mkdir(parents=True, exist_ok=True)
        descriptor = os.open(self._key_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(key)
        return Fernet(key)

    def load(self) -> dict[str, Any]:
        if not self.path.is_file():
            return {}
        payload = self.path.read_bytes()
        magic, body = payload[:4], payload[4:]
        if magic == _DPAPI_MAGIC:
            plain = _dpapi(body, protect=False)
        elif magic == _FERNET_MAGIC:
            plain = self._fernet(create=False).decrypt(body)
        else:
            raise ValueError("the site box vault is not a SyncCam vault")
        data = json.loads(plain)
        return data if isinstance(data, dict) else {}

    def save(self, data: Mapping[str, Any]) -> None:
        plain = json.dumps(dict(data), separators=(",", ":")).encode()
        if self._use_dpapi:
            payload = _DPAPI_MAGIC + _dpapi(plain, protect=True)
        else:
            payload = _FERNET_MAGIC + self._fernet(create=True).encrypt(plain)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        temporary = self.path.with_name(self.path.name + ".tmp")
        temporary.write_bytes(payload)
        os.replace(temporary, self.path)

    def update(self, **values: Any) -> dict[str, Any]:
        data = self.load()
        data.update(values)
        self.save(data)
        return data
