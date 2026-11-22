"""Feature switches from the dashboard (ADR-019).

The backend sends, with the site configuration every minute, which features
are on and on which cameras::

    "features": {"weapon": {"on": true, "cameras": ["<camera id>"]},
                 "zone_alerts": {"on": false, "cameras": null}, ...}

``cameras`` is null for every camera. The box applies it at once, with no
restart. A backend that sends no ``features`` (older than this box) leaves
everything on, as before. The backend also drops events from switched-off
features, so this gate mainly saves the work of running them.
"""

from __future__ import annotations

from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from typing import Any

# Zone rule kinds and the feature each belongs to.
KIND_FEATURES: dict[str, str] = {
    "intrusion": "zone_alerts",
    "restricted_zone": "zone_alerts",
    "tripwire": "zone_alerts",
    "loitering": "loitering",
}

# What every box can do without extra models.
CORE_CAPABILITIES: tuple[str, ...] = ("zone_alerts", "loitering", "camera_health")


@dataclass(frozen=True)
class FeatureGate:
    """Which features run, and where. Unknown features are allowed."""

    switches: Mapping[str, tuple[bool, frozenset[str] | None]] = field(default_factory=dict)

    @classmethod
    def allow_all(cls) -> "FeatureGate":
        return cls()

    @classmethod
    def from_config(cls, raw: Any) -> "FeatureGate":
        if not isinstance(raw, Mapping):
            return cls.allow_all()
        switches: dict[str, tuple[bool, frozenset[str] | None]] = {}
        for key, value in raw.items():
            if not isinstance(value, Mapping):
                continue
            cameras = value.get("cameras")
            chosen = frozenset(str(item) for item in cameras) if isinstance(cameras, list) else None
            switches[str(key)] = (bool(value.get("on", True)), chosen)
        return cls(switches)

    def allows(self, feature: str, camera_id: str) -> bool:
        switch = self.switches.get(feature)
        if switch is None:
            return True
        on, cameras = switch
        return on and (cameras is None or camera_id in cameras)

    def allows_event(self, event_type: str, camera_id: str) -> bool:
        feature = KIND_FEATURES.get(event_type)
        return feature is None or self.allows(feature, camera_id)

    def describe(self) -> dict[str, str]:
        """A short summary for the log: feature -> "on" / "off" / "N cameras"."""

        summary: dict[str, str] = {}
        for key, (on, cameras) in sorted(self.switches.items()):
            if not on:
                summary[key] = "off"
            elif cameras is None:
                summary[key] = "on"
            else:
                summary[key] = f"{len(cameras)} camera(s)"
        return summary


def capabilities(processor_features: Iterable[str | None], *, live_view: bool) -> list[str]:
    """What this box has loaded, for the heartbeat."""

    result = list(CORE_CAPABILITIES)
    for feature in processor_features:
        if feature and feature not in result:
            result.append(feature)
    if live_view:
        result.append("live_view")
    return result


__all__ = ["CORE_CAPABILITIES", "FeatureGate", "KIND_FEATURES", "capabilities"]
