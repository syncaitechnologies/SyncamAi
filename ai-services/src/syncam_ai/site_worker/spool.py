"""Durable local queue so alerts survive internet outages at the site."""

from __future__ import annotations

import json
import logging
import os
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from .client import BackendError

_LOG = logging.getLogger(__name__)
MAX_AGE_SECONDS = 24 * 3600
MAX_ENTRIES = 2000


class Delivery(Protocol):
    def upload_evidence(self, jpeg: bytes) -> str: ...

    def post_event(self, event: dict[str, object]) -> None: ...


@dataclass(frozen=True)
class DeliveryResult:
    delivered: int
    dropped: int
    pending: int


class Spool:
    """Each alert is an ``<id>.json`` event plus optional ``<id>.jpg`` snapshot.

    The snapshot is uploaded first and its reference is written back into the
    event file before the event is posted, so retries reuse the same reference
    and the backend's dedupe key stays stable.
    """

    def __init__(self, directory: Path) -> None:
        self._directory = directory
        self._directory.mkdir(parents=True, exist_ok=True)

    def enqueue(self, event: dict[str, object], snapshot: bytes | None) -> Path:
        entries = sorted(self._directory.glob("*.json"))
        if len(entries) >= MAX_ENTRIES:
            self._remove(entries[0])
        event_id = str(event["event_id"])
        if snapshot is not None:
            _atomic_write(self._directory / f"{event_id}.jpg", snapshot)
        target = self._directory / f"{event_id}.json"
        _atomic_write(target, json.dumps({"queued_at": time.time(), "event": event}).encode())
        return target

    def deliver(self, client: Delivery, *, now: float | None = None) -> DeliveryResult:
        current = time.time() if now is None else now
        delivered = dropped = 0
        for path in sorted(self._directory.glob("*.json"), key=lambda item: item.stat().st_mtime):
            try:
                record = json.loads(path.read_text(encoding="utf-8"))
                event = record["event"]
                queued_at = float(record["queued_at"])
            except (OSError, ValueError, KeyError, TypeError):
                _LOG.warning("dropping unreadable spool entry %s", path.name)
                self._remove(path)
                dropped += 1
                continue
            if current - queued_at > MAX_AGE_SECONDS:
                _LOG.warning("dropping alert %s older than 24h", event.get("event_id"))
                self._remove(path)
                dropped += 1
                continue
            snapshot = path.with_suffix(".jpg")
            try:
                if snapshot.exists() and not event.get("evidence_refs"):
                    event["evidence_refs"] = [client.upload_evidence(snapshot.read_bytes())]
                    _atomic_write(path, json.dumps({"queued_at": queued_at, "event": event}).encode())
                client.post_event(event)
            except BackendError as error:
                if error.retryable:
                    _LOG.info("alert %s will be retried: %s", event.get("event_id"), error)
                    break
                _LOG.error("alert %s rejected permanently: %s", event.get("event_id"), error)
                self._remove(path)
                dropped += 1
                continue
            self._remove(path)
            delivered += 1
        return DeliveryResult(delivered=delivered, dropped=dropped, pending=len(list(self._directory.glob("*.json"))))

    def _remove(self, path: Path) -> None:
        for candidate in (path, path.with_suffix(".jpg")):
            try:
                candidate.unlink()
            except FileNotFoundError:
                pass


def _atomic_write(path: Path, data: bytes) -> None:
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_bytes(data)
    os.replace(temporary, path)
