"""Atomically activate verified zone configuration for the local rule runtime."""

from __future__ import annotations

from threading import RLock
from typing import Mapping

from .zone_rules import TrackObservation, ZoneRuleEngine, load_zone_rules


class ZoneRuntime:
    """Own one active, camera-local zone rule engine.

    The caller must only pass configuration payloads after the Go edge agent has
    completed its mTLS pull, SHA-256 validation, and durable atomic file apply.
    This class compiles a candidate before swapping it into use; a bad revision
    therefore leaves the last known-good runtime intact.
    """

    def __init__(self) -> None:
        self._lock = RLock()
        self._engine: ZoneRuleEngine | None = None
        self._revision = 0

    @property
    def applied_revision(self) -> int:
        with self._lock:
            return self._revision

    def activate_verified_configuration(
        self, revision: int, payload: Mapping[str, object]
    ) -> bool:
        """Compile and atomically activate a strictly newer verified revision.

        Returns ``False`` for an already-applied revision. Older revisions and
        malformed payloads fail closed with ``ValueError``.
        """

        if not isinstance(revision, int) or isinstance(revision, bool) or revision < 1:
            raise ValueError("configuration revision must be a positive integer")
        if not isinstance(payload, Mapping):
            raise ValueError("configuration payload must be an object")
        with self._lock:
            if revision == self._revision:
                return False
            if revision < self._revision:
                raise ValueError("configuration revision must not roll back")
            candidate = ZoneRuleEngine(load_zone_rules(payload))
            self._engine = candidate
            self._revision = revision
            return True

    def observe(self, observation: TrackObservation) -> list[dict[str, object]]:
        """Evaluate against the active revision, or safely emit nothing."""

        with self._lock:
            if self._engine is None:
                return []
            return self._engine.observe(observation)
