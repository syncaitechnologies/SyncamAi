"""Anonymous door and table counting (ADR-020)."""

import pathlib
import sys
import unittest
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.analytics import AnalyticsCounter, parse_zones  # noqa: E402
from syncam_ai.site_worker.client import BackendError  # noqa: E402

CAMERA = "33333333-3333-4333-8333-333333333333"
DOOR = "d1111111-1111-4111-8111-111111111111"
TABLE = "d2222222-2222-4222-8222-222222222222"
START = datetime(2026, 11, 25, 18, 0, tzinfo=timezone.utc)
# A vertical door line at x = 0.5 drawn top to bottom: inside is on the left
# of the picture (the visual right of a downward arrow).
DOOR_ZONE = {"id": DOOR, "camera_id": CAMERA, "kind": "door_line", "enabled": True,
             "geometry": {"type": "LineString", "coordinates": [[0.5, 0.1], [0.5, 0.9]]}}
TABLE_ZONE = {"id": TABLE, "camera_id": CAMERA, "kind": "table", "enabled": True,
              "geometry": {"type": "Polygon", "coordinates": [[[0.6, 0.2], [0.9, 0.2], [0.9, 0.8], [0.6, 0.8], [0.6, 0.2]]]}}


@dataclass
class Person:
    track_id: int
    center_x: float
    center_y: float = 0.5
    subject_class: str = "person"


def counter(*zones):
    analytics = AnalyticsCounter(run_id="e1111111-1111-4111-8111-111111111111")
    analytics.apply_zones(list(zones))
    return analytics


def row(analytics, zone):
    rows = [item for item in analytics.pending_rows() if item["zone_id"] == zone]
    totals = {"entries": 0, "exits": 0, "occupied_seconds": 0, "sessions": 0, "session_seconds": 0, "peak_people": 0}
    for item in rows:
        for key in totals:
            totals[key] = max(totals[key], item[key]) if key == "peak_people" else totals[key] + item[key]
    return totals


class _Backend:
    def __init__(self, fail=False):
        self.sent, self.fail = [], fail

    def post_analytics(self, rows):
        if self.fail:
            raise BackendError("offline", retryable=True)
        self.sent.append(list(rows))


class ZoneParsingTest(unittest.TestCase):
    def test_only_enabled_counter_zones_with_valid_geometry(self) -> None:
        zones = parse_zones([
            DOOR_ZONE, TABLE_ZONE,
            {**DOOR_ZONE, "id": "x", "enabled": False},
            {"id": "y", "camera_id": CAMERA, "kind": "intrusion", "geometry": TABLE_ZONE["geometry"]},
            {**DOOR_ZONE, "id": "z", "geometry": TABLE_ZONE["geometry"]},
            {**TABLE_ZONE, "id": "w", "camera_id": ""},
        ])
        self.assertEqual([zone.id for zone in zones], [DOOR, TABLE])


class DoorTest(unittest.TestCase):
    def walk(self, analytics, track, xs, start=START, step=0.25):
        for index, x in enumerate(xs):
            analytics.observe(CAMERA, [Person(track, x)], start + timedelta(seconds=index * step))

    def test_in_and_out_by_direction(self) -> None:
        analytics = counter(DOOR_ZONE)
        self.walk(analytics, 1, [0.7, 0.6, 0.45, 0.3])  # right to left: into the restaurant
        self.walk(analytics, 2, [0.3, 0.45, 0.6, 0.7], start=START + timedelta(seconds=5))  # left to right: out
        totals = row(analytics, DOOR)
        self.assertEqual((totals["entries"], totals["exits"]), (1, 1))

    def test_someone_hovering_on_the_line_is_not_counted_again(self) -> None:
        analytics = counter(DOOR_ZONE)
        self.walk(analytics, 1, [0.7, 0.51, 0.49, 0.51, 0.49, 0.51, 0.3])
        self.assertEqual(row(analytics, DOOR)["entries"], 1)

    def test_one_count_per_direction_every_ten_seconds(self) -> None:
        analytics = counter(DOOR_ZONE)
        self.walk(analytics, 1, [0.7, 0.3, 0.7, 0.3, 0.7, 0.3], step=1.0)
        self.assertEqual(row(analytics, DOOR)["entries"], 1)
        self.walk(analytics, 1, [0.7, 0.3], start=START + timedelta(seconds=30))
        self.assertEqual(row(analytics, DOOR)["entries"], 2)

    def test_crossing_beyond_the_end_of_the_door_line_is_ignored(self) -> None:
        analytics = counter(DOOR_ZONE)
        for index, x in enumerate([0.7, 0.3]):
            analytics.observe(CAMERA, [Person(1, x, center_y=0.99)], START + timedelta(seconds=index))
        self.assertEqual(row(analytics, DOOR)["entries"], 0)

    def test_vehicles_and_other_cameras_are_not_counted(self) -> None:
        analytics = counter(DOOR_ZONE)
        for index, x in enumerate([0.7, 0.3]):
            analytics.observe(CAMERA, [Person(1, x, subject_class="car")], START + timedelta(seconds=index))
            analytics.observe("other-camera", [Person(2, x)], START + timedelta(seconds=index))
        self.assertEqual(row(analytics, DOOR)["entries"], 0)


class TableTest(unittest.TestCase):
    def sit(self, analytics, seconds, start, people=1, step=1.0):
        for index in range(int(seconds / step)):
            analytics.observe(CAMERA, [Person(track, 0.75) for track in range(people)], start + timedelta(seconds=index * step))

    def empty(self, analytics, seconds, start, step=1.0):
        for index in range(int(seconds / step)):
            analytics.observe(CAMERA, [], start + timedelta(seconds=index * step))

    def test_a_passer_by_under_a_minute_is_not_a_session(self) -> None:
        analytics = counter(TABLE_ZONE)
        self.sit(analytics, 30, START)
        self.empty(analytics, 200, START + timedelta(seconds=30))
        totals = row(analytics, TABLE)
        self.assertEqual((totals["sessions"], totals["occupied_seconds"]), (0, 0))
        self.assertEqual(totals["peak_people"], 1)

    def test_a_meal_with_a_short_break_is_one_session(self) -> None:
        analytics = counter(TABLE_ZONE)
        self.sit(analytics, 600, START, people=3)
        self.empty(analytics, 90, START + timedelta(seconds=600))  # a washroom trip
        self.sit(analytics, 600, START + timedelta(seconds=690), people=2)
        self.empty(analytics, 125, START + timedelta(seconds=1290))
        totals = row(analytics, TABLE)
        self.assertEqual(totals["sessions"], 1)
        self.assertAlmostEqual(totals["session_seconds"], 1289, delta=2)
        self.assertAlmostEqual(totals["occupied_seconds"], 1289, delta=3)
        self.assertEqual(totals["peak_people"], 3)

    def test_a_session_ending_in_the_next_hour_is_counted_where_it_ends(self) -> None:
        analytics = counter(TABLE_ZONE)
        start = START + timedelta(minutes=50)
        self.sit(analytics, 1200, start)
        self.empty(analytics, 125, start + timedelta(seconds=1200))
        rows = {item["hour_start"]: item for item in analytics.pending_rows()}
        self.assertEqual(sorted(rows), ["2026-11-25T18:00:00Z", "2026-11-25T19:00:00Z"])
        self.assertEqual(rows["2026-11-25T19:00:00Z"]["sessions"], 1)
        self.assertGreater(rows["2026-11-25T18:00:00Z"]["occupied_seconds"], 500)


class UploadTest(unittest.TestCase):
    def test_upload_every_five_minutes_and_retry_after_a_failure(self) -> None:
        analytics = counter(DOOR_ZONE)
        for index, x in enumerate([0.7, 0.3]):
            analytics.observe(CAMERA, [Person(1, x)], START + timedelta(seconds=index))
        offline = _Backend(fail=True)
        with self.assertRaises(BackendError):
            analytics.flush(offline, START + timedelta(minutes=1))
        backend = _Backend()
        self.assertEqual(analytics.flush(backend, START + timedelta(minutes=2)), 0, "waits five minutes after the last try")
        self.assertEqual(analytics.flush(backend, START + timedelta(minutes=7)), 1)
        sent = backend.sent[0][0]
        self.assertEqual(set(sent), {"zone_id", "hour_start", "run_id", "entries", "exits", "occupied_seconds", "sessions", "session_seconds", "peak_people"})
        self.assertEqual((sent["entries"], sent["hour_start"]), (1, "2026-11-25T18:00:00Z"))
        # After the hour ends it is sent once more with its final totals, then no more.
        self.assertEqual(analytics.flush(backend, START + timedelta(hours=1, minutes=10)), 1)
        self.assertEqual(analytics.flush(backend, START + timedelta(hours=1, minutes=20)), 0)

    def test_nothing_is_sent_without_counter_zones(self) -> None:
        analytics = counter()
        analytics.observe(CAMERA, [Person(1, 0.5)], START)
        self.assertEqual(analytics.flush(_Backend(), START, force=True), 0)

    def test_hours_follow_a_half_hour_site_time_zone(self) -> None:
        analytics = AnalyticsCounter(run_id="e1111111-1111-4111-8111-111111111111")
        analytics.apply_zones([DOOR_ZONE], timezone_name="Asia/Kolkata")
        # 18:40 UTC is 00:10 in India, so it falls in the hour that started at 18:30 UTC.
        for index, x in enumerate([0.7, 0.3]):
            analytics.observe(CAMERA, [Person(1, x)], START + timedelta(minutes=40, seconds=index))
        self.assertEqual([item["hour_start"] for item in analytics.pending_rows()], ["2026-11-25T18:30:00Z"])


class RestartTest(unittest.TestCase):
    def test_counts_not_sent_yet_survive_a_restart(self) -> None:
        import tempfile

        with tempfile.TemporaryDirectory() as directory:
            state = pathlib.Path(directory) / "counts.json"
            first = AnalyticsCounter(state_path=state)
            first.apply_zones([DOOR_ZONE])
            for index, x in enumerate([0.7, 0.3]):
                first.observe(CAMERA, [Person(1, x)], START + timedelta(seconds=index))
            with self.assertRaises(BackendError):
                first.flush(_Backend(fail=True), START + timedelta(minutes=1))

            second = AnalyticsCounter(state_path=state)
            second.apply_zones([DOOR_ZONE])
            self.assertEqual(second.run_id, first.run_id, "keeps adding to the same server rows")
            for index, x in enumerate([0.3, 0.7]):
                second.observe(CAMERA, [Person(2, x)], START + timedelta(minutes=2, seconds=index))
            backend = _Backend()
            self.assertEqual(second.flush(backend, START + timedelta(minutes=3)), 1)
            self.assertEqual((backend.sent[0][0]["entries"], backend.sent[0][0]["exits"]), (1, 1))

    def test_a_damaged_save_file_starts_from_zero(self) -> None:
        import tempfile

        with tempfile.TemporaryDirectory() as directory:
            state = pathlib.Path(directory) / "counts.json"
            state.write_text("{not json", encoding="utf-8")
            with self.assertLogs("syncam.site_worker.analytics", level="WARNING"):
                analytics = AnalyticsCounter(state_path=state)
            self.assertEqual(analytics.pending_rows(), [])


class WorkerWiringTest(unittest.TestCase):
    def test_the_worker_loads_counter_zones_from_the_dashboard(self) -> None:
        import tempfile

        from syncam_ai.site_worker.runner import SiteWorker
        from syncam_ai.site_worker.spool import Spool

        class _ConfigBackend:
            def config(self):
                return {"tenant_id": "11111111-1111-4111-8111-111111111111", "site_id": "22222222-2222-4222-8222-222222222222",
                        "site": {"name": "Cafe"}, "cameras": [{"id": CAMERA, "name": "Door"}], "zones": [DOOR_ZONE, TABLE_ZONE]}

        class _Detector:
            model_version = "test"

            def detect(self, frame, *, minimum_confidence=0.45):
                return []

        with tempfile.TemporaryDirectory() as directory:
            worker = SiteWorker(backend=_ConfigBackend(), detector=_Detector(), streams={}, spool=Spool(pathlib.Path(directory)), now=lambda: START)
            worker.refresh_config()
            self.assertEqual(sorted(zone.kind for zone in worker.analytics.zones), ["door_line", "table"])
            self.assertEqual(len(worker.rules.zones), 0, "counter zones never become alert rules")
            worker.process_frame(CAMERA, None, START)


if __name__ == "__main__":
    unittest.main()
