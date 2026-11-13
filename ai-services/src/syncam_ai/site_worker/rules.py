"""Zone protection rules for one site, built on the existing zone runtime.

``ZoneRuntime`` / ``ZoneRuleEngine`` (zone_rules.py) already evaluate
intrusion, restricted-zone, tripwire and loitering rules. This module adds the
three behaviours a security client needs on top of them:

* **Schedules** - a zone only alerts while it is armed (e.g. 22:00-06:00).
* **Presence** - for intrusion and restricted zones, a *person* who first
  appears *already inside* an armed zone also alerts, after a short
  confirmation, because the existing engine only fires on an outside-to-inside
  crossing.
* **Loitering near vehicles** - a loitering zone whose subject classes include
  vehicles alerts only for *people* who dwell there while a vehicle is present
  (car-theft risk). Parked vehicles themselves never trigger it.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field
from datetime import datetime
from typing import Final, Iterable, Mapping, Sequence

from ..track_ingestion import TrackFrame
from ..zone_rules import (
    TrackObservation,
    ZoneRule,
    _compile_rule,
    _CompiledRule,
    _event,
    _point_in_polygon,
    load_zone_rules,
)
from ..zone_runtime import ZoneRuntime
from .schedule import is_active

VEHICLE_CLASSES: Final = frozenset({"bicycle", "bus", "car", "motorcycle", "truck", "van"})
PRESENCE_CONFIRMATION_FRAMES: Final = 3
_TRACK_STATE_TTL_SECONDS: Final = 120


@dataclass(slots=True)
class _PresenceState:
    last_seen: datetime
    first_inside: bool
    inside_frames: int = 0
    emitted: bool = False


@dataclass(frozen=True, slots=True)
class ZoneInfo:
    """What the worker needs to know about one configured zone."""

    id: str
    name: str
    camera_id: str
    kind: str
    schedule: Mapping[str, object] | None
    near_vehicles: bool
    vehicle_classes: frozenset[str]
    compiled: _CompiledRule | None


@dataclass(slots=True)
class SiteRules:
    """Evaluates every camera's tracks against the site's zones."""

    tenant_id: str
    site_id: str
    _runtime: ZoneRuntime = field(default_factory=ZoneRuntime)
    _revision: int = 0
    _fingerprint: str = ""
    _zones: dict[str, ZoneInfo] = field(default_factory=dict)
    _presence: dict[tuple[str, str, int], _PresenceState] = field(default_factory=dict)

    @property
    def zones(self) -> Mapping[str, ZoneInfo]:
        return self._zones

    def zones_for_camera(self, camera_id: str) -> list[ZoneInfo]:
        return [zone for zone in self._zones.values() if zone.camera_id == camera_id]

    def apply_zones(self, zones: Sequence[Mapping[str, object]]) -> bool:
        """Activate a new zone set when it changed. Returns True on change."""

        fingerprint = hashlib.sha256(json.dumps(list(zones), sort_keys=True, default=str).encode()).hexdigest()
        if fingerprint == self._fingerprint:
            return False
        payload = {"zones": [dict(zone) for zone in zones]}
        rules = {rule.id: rule for rule in load_zone_rules(payload)}
        self._runtime.activate_verified_configuration(self._revision + 1, payload)
        self._revision += 1
        self._fingerprint = fingerprint
        self._zones = {}
        for raw in zones:
            zone_id = str(raw.get("id", ""))
            rule = rules.get(zone_id)
            if rule is None:
                continue
            subject_classes = rule.subject_classes
            vehicle_classes = frozenset(subject_classes & VEHICLE_CLASSES)
            schedule = raw.get("schedule")
            self._zones[zone_id] = ZoneInfo(
                id=zone_id,
                name=str(raw.get("name", "")),
                camera_id=rule.camera_id,
                kind=rule.kind,
                schedule=schedule if isinstance(schedule, Mapping) else None,
                near_vehicles=rule.kind == "loitering" and "person" in subject_classes and bool(vehicle_classes),
                vehicle_classes=vehicle_classes,
                compiled=_compile_rule(rule) if rule.kind != "tripwire" else None,
            )
        self._presence.clear()
        return True

    def evaluate(self, frame: TrackFrame) -> list[dict[str, object]]:
        """Return the alert-worthy events for one tracked camera frame."""

        if frame.tenant_id != self.tenant_id or frame.site_id != self.site_id:
            raise ValueError("track frame belongs to another site")
        zones = self.zones_for_camera(frame.camera_id)
        if not zones:
            return []
        armed = {zone.id for zone in zones if is_active(zone.schedule, frame.observed_at)}
        vehicles_present = {
            zone.id
            for zone in zones
            if zone.near_vehicles and zone.compiled is not None
            and any(
                track.subject_class in zone.vehicle_classes
                and _point_in_polygon(track.center_x, track.center_y, zone.compiled.points)
                for track in frame.tracks
            )
        }
        events: list[dict[str, object]] = []
        for track in frame.tracks:
            for event in self._runtime.observe(track):
                zone = self._zones.get(str(event.get("zone_id")))
                if zone is None or zone.id not in armed:
                    continue
                if zone.near_vehicles and (track.subject_class != "person" or zone.id not in vehicles_present):
                    continue
                events.append(event)
            events.extend(self._presence_events(track, zones, armed))
        self._evict(frame.observed_at)
        return events

    def _presence_events(
        self, track: TrackObservation, zones: Iterable[ZoneInfo], armed: set[str]
    ) -> list[dict[str, object]]:
        events: list[dict[str, object]] = []
        if track.subject_class != "person":
            # Vehicles parked before a zone arms must not alert when it arms.
            return events
        for zone in zones:
            if zone.kind not in {"intrusion", "restricted_zone"} or zone.compiled is None:
                continue
            if zone.compiled.subject_classes and track.subject_class not in zone.compiled.subject_classes:
                continue
            inside = _point_in_polygon(track.center_x, track.center_y, zone.compiled.points)
            key = (zone.id, track.camera_id, track.track_id)
            state = self._presence.get(key)
            if state is None:
                state = _PresenceState(last_seen=track.observed_at, first_inside=inside)
                self._presence[key] = state
            state.last_seen = track.observed_at
            if not state.first_inside or state.emitted:
                continue
            state.inside_frames = state.inside_frames + 1 if inside else 0
            if state.inside_frames >= PRESENCE_CONFIRMATION_FRAMES and zone.id in armed:
                state.emitted = True
                events.append(_event(zone.compiled, track, "present", track.observed_at))
        return events

    def _evict(self, now: datetime) -> None:
        cutoff = now.timestamp() - _TRACK_STATE_TTL_SECONDS
        for key in [key for key, state in self._presence.items() if state.last_seen.timestamp() < cutoff]:
            del self._presence[key]


def near_vehicle_zone_classes() -> list[str]:
    """Subject classes the dashboard uses for a "loitering near vehicles" zone."""

    return sorted({"person"} | VEHICLE_CLASSES)


__all__ = ["SiteRules", "ZoneInfo", "ZoneRule", "VEHICLE_CLASSES", "near_vehicle_zone_classes"]
