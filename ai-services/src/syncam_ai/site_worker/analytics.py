"""Anonymous visitor and table counts (ADR-020).

Two dashboard zone kinds feed this module; neither raises alerts:

``door_line``
    A line across an entrance. Inside is on the right of the arrow from the
    first point to the second (as drawn on the camera picture). A person
    crossing it counts as one entry or one exit.
``table``
    A polygon around a table. A seated session starts once someone has been
    there for a minute and ends two minutes after the last person leaves.

Only numbers leave the site box: per zone per hour, the entries, exits,
occupied seconds, finished sessions, their total length and the most people
seen at once. Track ids, positions and pictures never do.
"""

from __future__ import annotations

import logging
import math
import uuid
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from typing import Any, Protocol

_LOG = logging.getLogger("syncam.site_worker.analytics")

KINDS = frozenset({"door_line", "table"})
# A track must be this far (in frame widths/heights) past the line on each
# side, so someone standing on the line is not counted again and again.
DOOR_MARGIN = 0.02
DOOR_REPEAT_SECONDS = 10.0
SEAT_SECONDS = 60.0
EMPTY_SECONDS = 120.0
UPLOAD_SECONDS = 300.0
# A stalled camera must not count its gap as a long stay.
MAX_FRAME_GAP_SECONDS = 5.0
TRACK_MEMORY_SECONDS = 900.0
KEEP_HOURS = 48
MAX_UPLOAD_ROWS = 200


class Track(Protocol):
    track_id: int
    center_x: float
    center_y: float
    subject_class: str


class AnalyticsBackend(Protocol):
    def post_analytics(self, rows: Sequence[Mapping[str, object]]) -> None: ...


@dataclass(frozen=True)
class AnalyticsZone:
    id: str
    camera_id: str
    kind: str
    points: tuple[tuple[float, float], ...]


def parse_zones(raw_zones: Iterable[Mapping[str, Any]]) -> list[AnalyticsZone]:
    """The enabled door_line and table zones of the dashboard config."""

    zones: list[AnalyticsZone] = []
    for raw in raw_zones:
        kind = str(raw.get("kind", ""))
        if kind not in KINDS or raw.get("enabled") is False or not raw.get("camera_id"):
            continue
        geometry = raw.get("geometry") or {}
        try:
            if kind == "door_line" and geometry.get("type") == "LineString":
                coordinates = geometry["coordinates"]
                points = (_point(coordinates[0]), _point(coordinates[-1]))
            elif kind == "table" and geometry.get("type") == "Polygon":
                points = tuple(_point(point) for point in geometry["coordinates"][0])
                if len(points) < 4:
                    continue
            else:
                continue
        except (KeyError, IndexError, TypeError, ValueError):
            continue
        zones.append(AnalyticsZone(id=str(raw["id"]), camera_id=str(raw["camera_id"]), kind=kind, points=points))
    return zones


def _point(value: Any) -> tuple[float, float]:
    x, y = float(value[0]), float(value[1])
    if not (math.isfinite(x) and math.isfinite(y)):
        raise ValueError("coordinate is not finite")
    return x, y


def _inside(x: float, y: float, polygon: Sequence[tuple[float, float]]) -> bool:
    inside = False
    for (x1, y1), (x2, y2) in zip(polygon, polygon[1:]):
        if (y1 > y) != (y2 > y) and x < (x2 - x1) * (y - y1) / (y2 - y1) + x1:
            inside = not inside
    return inside


def _door_position(zone: AnalyticsZone, x: float, y: float) -> tuple[float, float]:
    """Signed distance from the line (positive = inside) and position along it (0..1)."""

    (ax, ay), (bx, by) = zone.points
    dx, dy = bx - ax, by - ay
    length = math.hypot(dx, dy) or 1e-9
    # Image y grows downwards, so the visual right of the arrow is (-dy, dx).
    distance = ((x - ax) * -dy + (y - ay) * dx) / length
    along = ((x - ax) * dx + (y - ay) * dy) / (length * length)
    return distance, along


def _hour(at: datetime) -> datetime:
    return at.astimezone(timezone.utc).replace(minute=0, second=0, microsecond=0)


@dataclass
class _Totals:
    entries: int = 0
    exits: int = 0
    occupied_seconds: float = 0.0
    sessions: int = 0
    session_seconds: float = 0.0
    peak_people: int = 0


@dataclass
class _DoorTrack:
    side: int = 0
    seen_at: datetime | None = None
    last_counted: dict[int, datetime] = field(default_factory=dict)


@dataclass
class _Table:
    present_since: datetime | None = None
    session_start: datetime | None = None
    last_person: datetime | None = None
    unpaid_gap: float = 0.0


class AnalyticsCounter:
    """Counts door crossings and table sessions, and uploads hourly totals."""

    def __init__(self, *, run_id: str | None = None) -> None:
        self.run_id = run_id or str(uuid.uuid4())
        self._zones: list[AnalyticsZone] = []
        self._totals: dict[tuple[str, datetime], _Totals] = {}
        self._dirty: set[tuple[str, datetime]] = set()
        self._doors: dict[tuple[str, int], _DoorTrack] = {}
        self._tables: dict[str, _Table] = {}
        self._camera_seen: dict[str, datetime] = {}
        self._last_upload: datetime | None = None

    @property
    def zones(self) -> list[AnalyticsZone]:
        return list(self._zones)

    def apply_zones(self, raw_zones: Iterable[Mapping[str, Any]]) -> None:
        zones = parse_zones(raw_zones)
        if zones != self._zones:
            _LOG.info("counting %d door counter(s) and %d table(s)",
                      sum(zone.kind == "door_line" for zone in zones), sum(zone.kind == "table" for zone in zones))
        self._zones = zones
        live = {zone.id for zone in zones}
        self._tables = {zone_id: table for zone_id, table in self._tables.items() if zone_id in live}

    def _bucket(self, zone_id: str, at: datetime) -> _Totals:
        key = (zone_id, _hour(at))
        self._dirty.add(key)
        return self._totals.setdefault(key, _Totals())

    def observe(self, camera_id: str, tracks: Sequence[Track], observed_at: datetime) -> None:
        zones = [zone for zone in self._zones if zone.camera_id == camera_id]
        previous = self._camera_seen.get(camera_id)
        self._camera_seen[camera_id] = observed_at
        if not zones:
            return
        gap = 0.0 if previous is None else min(MAX_FRAME_GAP_SECONDS, max(0.0, (observed_at - previous).total_seconds()))
        people = [track for track in tracks if track.subject_class == "person"]
        for zone in zones:
            if zone.kind == "door_line":
                self._observe_door(zone, people, observed_at)
            else:
                self._observe_table(zone, people, observed_at, gap)
        self._forget_old_tracks(observed_at)

    def _observe_door(self, zone: AnalyticsZone, people: Sequence[Track], at: datetime) -> None:
        for person in people:
            state = self._doors.setdefault((zone.id, person.track_id), _DoorTrack())
            state.seen_at = at
            distance, along = _door_position(zone, person.center_x, person.center_y)
            if abs(distance) < DOOR_MARGIN or not -0.1 <= along <= 1.1:
                continue
            side = 1 if distance > 0 else -1
            if state.side == 0:
                state.side = side
                continue
            if side == state.side:
                continue
            state.side = side
            last = state.last_counted.get(side)
            if last is not None and (at - last).total_seconds() < DOOR_REPEAT_SECONDS:
                continue
            state.last_counted[side] = at
            totals = self._bucket(zone.id, at)
            if side > 0:
                totals.entries += 1
            else:
                totals.exits += 1

    def _observe_table(self, zone: AnalyticsZone, people: Sequence[Track], at: datetime, gap: float) -> None:
        table = self._tables.setdefault(zone.id, _Table())
        count = sum(1 for person in people if _inside(person.center_x, person.center_y, zone.points))
        totals = self._bucket(zone.id, at)
        totals.peak_people = max(totals.peak_people, count)
        if table.session_start is None:
            if count == 0:
                table.present_since = None
                return
            table.present_since = table.present_since or at
            table.last_person = at
            if (at - table.present_since).total_seconds() >= SEAT_SECONDS:
                table.session_start = table.present_since
                totals.occupied_seconds = min(3600.0, totals.occupied_seconds + (at - table.present_since).total_seconds())
            return
        if count > 0:
            # A short absence (a washroom trip) is part of the session.
            totals.occupied_seconds = min(3600.0, totals.occupied_seconds + gap + table.unpaid_gap)
            table.unpaid_gap = 0.0
            table.last_person = at
            return
        table.unpaid_gap += gap
        if table.last_person is not None and (at - table.last_person).total_seconds() >= EMPTY_SECONDS:
            ended = self._bucket(zone.id, table.last_person)
            ended.sessions += 1
            ended.session_seconds += (table.last_person - table.session_start).total_seconds()
            self._tables[zone.id] = _Table()

    def _forget_old_tracks(self, now: datetime) -> None:
        if len(self._doors) < 2000:
            return
        cutoff = now - timedelta(seconds=TRACK_MEMORY_SECONDS)
        self._doors = {key: state for key, state in self._doors.items() if state.seen_at is not None and state.seen_at >= cutoff}

    def pending_rows(self) -> list[dict[str, object]]:
        rows = []
        for zone_id, hour in sorted(self._dirty, key=lambda key: (key[1], key[0]))[:MAX_UPLOAD_ROWS]:
            totals = self._totals[(zone_id, hour)]
            rows.append({
                "zone_id": zone_id,
                "hour_start": hour.isoformat().replace("+00:00", "Z"),
                "run_id": self.run_id,
                "entries": totals.entries,
                "exits": totals.exits,
                "occupied_seconds": int(min(3600, round(totals.occupied_seconds))),
                "sessions": totals.sessions,
                "session_seconds": int(round(totals.session_seconds)),
                "peak_people": totals.peak_people,
            })
        return rows

    def flush(self, backend: AnalyticsBackend, now: datetime, *, force: bool = False) -> int:
        """Send changed hourly totals every few minutes. Returns rows sent."""

        if not force and self._last_upload is not None and (now - self._last_upload).total_seconds() < UPLOAD_SECONDS:
            return 0
        self._last_upload = now
        # shortcut: totals not yet sent are kept in memory only and are lost if
        # the box restarts mid-hour; spool them to disk if that matters.
        cutoff = _hour(now) - timedelta(hours=KEEP_HOURS - 1)
        self._dirty = {key for key in self._dirty if key[1] >= cutoff}
        self._totals = {key: totals for key, totals in self._totals.items() if key[1] >= cutoff}
        rows = self.pending_rows()
        if not rows:
            return 0
        backend.post_analytics(rows)
        sent = {(str(row["zone_id"]), datetime.fromisoformat(str(row["hour_start"]).replace("Z", "+00:00"))) for row in rows}
        current = _hour(now)
        # The current hour keeps changing, so it stays marked until it ends.
        self._dirty -= {key for key in sent if key[1] < current}
        return len(rows)


__all__ = ["AnalyticsCounter", "AnalyticsZone", "KINDS", "parse_zones"]
