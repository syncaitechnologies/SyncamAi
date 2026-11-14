"""Weekly zone schedules, matching the control plane's zones.Active."""

from __future__ import annotations

import logging
from datetime import datetime, timezone, tzinfo
from typing import Mapping
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

_DAYS = {"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6}
_LOG = logging.getLogger(__name__)
_WARNED: set[str] = set()


def _zone(name: str) -> tzinfo | None:
    if name in {"UTC", "Etc/UTC"}:
        return timezone.utc
    try:
        return ZoneInfo(name)
    except (ZoneInfoNotFoundError, ValueError):
        if name not in _WARNED:
            _WARNED.add(name)
            _LOG.warning("timezone %r is unavailable (install tzdata); zone stays armed all the time", name)
        return None


def _minutes(value: object) -> int | None:
    if not isinstance(value, str) or len(value) != 5 or value[2] != ":":
        return None
    try:
        hours, minutes = int(value[:2]), int(value[3:])
    except ValueError:
        return None
    if not (0 <= hours <= 23 and 0 <= minutes <= 59):
        return None
    return hours * 60 + minutes


def is_active(schedule: Mapping[str, object] | None, at: datetime) -> bool:
    """Return whether a zone is armed at ``at``. No schedule means always armed.

    A window whose end is before its start crosses midnight, so
    ``22:00-06:00`` on Monday also covers early Tuesday morning.
    """

    if not schedule:
        return True
    if at.tzinfo is None:
        raise ValueError("schedule checks need a timezone-aware instant")
    zone = _zone(str(schedule.get("timezone", "")))
    if zone is None:
        # Fail armed: a missed intrusion is worse than an extra alert.
        return True
    local = at.astimezone(zone)
    minute = local.hour * 60 + local.minute
    today = local.weekday()
    yesterday = (today + 6) % 7
    windows = schedule.get("windows")
    if not isinstance(windows, list):
        return True
    for window in windows:
        if not isinstance(window, Mapping):
            continue
        start, end = _minutes(window.get("start")), _minutes(window.get("end"))
        days = window.get("days")
        if start is None or end is None or start == end or not isinstance(days, list):
            continue
        for day in days:
            index = _DAYS.get(str(day).lower())
            if index is None:
                continue
            if start < end and index == today and start <= minute < end:
                return True
            if start > end and ((index == today and minute >= start) or (index == yesterday and minute < end)):
                return True
    return False
