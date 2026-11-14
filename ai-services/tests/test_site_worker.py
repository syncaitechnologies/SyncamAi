import json
import pathlib
import sys
import tempfile
import threading
import unittest
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, HTTPServer

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_artifacts import ModelArtifact, verify_model_artifact  # noqa: E402
from syncam_ai.site_worker.client import BackendClient, BackendError  # noqa: E402
from syncam_ai.site_worker.config import parse_local_config  # noqa: E402
from syncam_ai.site_worker.detector import ObjectDetection, decode_yolox, non_max_suppression  # noqa: E402
from syncam_ai.site_worker.rules import SiteRules, near_vehicle_zone_classes  # noqa: E402
from syncam_ai.site_worker.runner import SiteWorker, camera_health_event  # noqa: E402
from syncam_ai.site_worker.schedule import is_active  # noqa: E402
from syncam_ai.site_worker.spool import Spool  # noqa: E402
from syncam_ai.track_ingestion import TrackFrame  # noqa: E402
from syncam_ai.zone_rules import TrackObservation  # noqa: E402

TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
DEVICE = "44444444-4444-4444-8444-444444444444"
YARD = "55555555-5555-4555-8555-555555555555"
PARKING = "66666666-6666-4666-8666-666666666666"
SQUARE = {"type": "Polygon", "coordinates": [[[0.5, 0.0], [1.0, 0.0], [1.0, 1.0], [0.5, 1.0], [0.5, 0.0]]]}
NIGHT = {"timezone": "UTC", "windows": [{"days": ["mon", "tue", "wed", "thu", "fri", "sat", "sun"], "start": "22:00", "end": "06:00"}]}
START = datetime(2026, 11, 16, 23, 0, tzinfo=timezone.utc)  # a Monday night


def zone(zone_id, kind, **extra):
    payload = {
        "id": zone_id, "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "name": extra.pop("name", kind),
        "kind": kind, "geometry": SQUARE, "enabled": True,
    }
    payload.update(extra)
    return payload


def frame(at, *tracks):
    return TrackFrame(tenant_id=TENANT, site_id=SITE, camera_id=CAMERA, device_id=DEVICE, observed_at=at, tracks=tuple(
        TrackObservation(tenant_id=TENANT, site_id=SITE, camera_id=CAMERA, track_id=track_id, observed_at=at,
                         center_x=x, center_y=0.5, subject_class=subject, confidence=0.9, model_version="m1")
        for track_id, x, subject in tracks
    ))


class ScheduleTest(unittest.TestCase):
    def test_overnight_window_and_always_active(self) -> None:
        self.assertTrue(is_active(None, START))
        self.assertTrue(is_active(NIGHT, START))
        self.assertTrue(is_active(NIGHT, START + timedelta(hours=6, minutes=59)))
        self.assertFalse(is_active(NIGHT, START + timedelta(hours=7)))
        self.assertFalse(is_active(NIGHT, START - timedelta(minutes=61)))
        with self.assertRaises(ValueError):
            is_active(NIGHT, datetime(2026, 11, 16, 23, 0))

    def test_named_timezones(self) -> None:
        try:
            from zoneinfo import ZoneInfo

            ZoneInfo("Asia/Kolkata")
        except Exception:
            self.skipTest("no timezone database (install tzdata)")
        weekend = {"timezone": "Asia/Kolkata", "windows": [{"days": ["sat"], "start": "09:00", "end": "17:00"}]}
        self.assertTrue(is_active(weekend, datetime(2026, 11, 21, 6, 0, tzinfo=timezone.utc)))  # 11:30 IST Saturday
        self.assertFalse(is_active(weekend, datetime(2026, 11, 22, 6, 0, tzinfo=timezone.utc)))

    def test_unknown_timezone_fails_armed(self) -> None:
        unknown = {"timezone": "Mars/Base", "windows": [{"days": ["sat"], "start": "09:00", "end": "10:00"}]}
        self.assertTrue(is_active(unknown, START))


class SiteRulesTest(unittest.TestCase):
    def rules(self, *zones):
        rules = SiteRules(tenant_id=TENANT, site_id=SITE)
        self.assertTrue(rules.apply_zones(list(zones)))
        self.assertFalse(rules.apply_zones(list(zones)))
        return rules

    def test_entry_into_armed_zone_alerts_once(self) -> None:
        rules = self.rules(zone(YARD, "intrusion", schedule=NIGHT))
        self.assertEqual(rules.evaluate(frame(START, (1, 0.2, "person"))), [])
        events = rules.evaluate(frame(START + timedelta(seconds=1), (1, 0.7, "person")))
        self.assertEqual([event["event_type"] for event in events], ["intrusion"])
        self.assertEqual(events[0]["zone_id"], YARD)
        self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=2), (1, 0.8, "person"))), [])

    def test_disarmed_zone_is_silent_during_the_day(self) -> None:
        rules = self.rules(zone(YARD, "intrusion", schedule=NIGHT))
        noon = START.replace(hour=12)
        rules.evaluate(frame(noon, (1, 0.2, "person")))
        self.assertEqual(rules.evaluate(frame(noon + timedelta(seconds=1), (1, 0.7, "person"))), [])

    def test_person_already_inside_alerts_after_confirmation(self) -> None:
        rules = self.rules(zone(YARD, "restricted_zone", subject_classes=["person"]))
        self.assertEqual(rules.evaluate(frame(START, (7, 0.8, "person"))), [])
        self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=1), (7, 0.8, "person"))), [])
        events = rules.evaluate(frame(START + timedelta(seconds=2), (7, 0.8, "person")))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "restricted_zone")
        self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=3), (7, 0.8, "person"))), [])
        # A parked car inside the zone is not a person.
        for second in range(4):
            self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=10 + second), (9, 0.8, "car"))), [])

    def test_loitering_near_vehicles_needs_a_person_and_a_vehicle(self) -> None:
        rules = self.rules(zone(PARKING, "loitering", loiter_seconds=30, subject_classes=near_vehicle_zone_classes(), name="Parking"))
        self.assertTrue(rules.zones[PARKING].near_vehicles)
        # A person loitering with no vehicle present: no alert.
        rules.evaluate(frame(START, (1, 0.2, "person")))
        rules.evaluate(frame(START + timedelta(seconds=1), (1, 0.7, "person")))
        self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=40), (1, 0.7, "person"))), [])
        # A parked car alone never alerts, however long it stays.
        rules.evaluate(frame(START + timedelta(seconds=50), (2, 0.2, "car")))
        rules.evaluate(frame(START + timedelta(seconds=51), (2, 0.8, "car")))
        self.assertEqual(rules.evaluate(frame(START + timedelta(seconds=200), (2, 0.8, "car"))), [])
        # A new person dwelling beside the parked car alerts once.
        rules.evaluate(frame(START + timedelta(seconds=201), (2, 0.8, "car"), (3, 0.2, "person")))
        rules.evaluate(frame(START + timedelta(seconds=202), (2, 0.8, "car"), (3, 0.7, "person")))
        events = rules.evaluate(frame(START + timedelta(seconds=240), (2, 0.8, "car"), (3, 0.7, "person")))
        self.assertEqual([event["event_type"] for event in events], ["loitering"])

    def test_rejects_frames_from_another_site(self) -> None:
        rules = self.rules(zone(YARD, "intrusion"))
        other = TrackFrame(tenant_id=TENANT, site_id=DEVICE, camera_id=CAMERA, device_id=DEVICE, observed_at=START, tracks=())
        with self.assertRaises(ValueError):
            rules.evaluate(other)


class DecodeTest(unittest.TestCase):
    def test_decodes_letterboxed_person_and_vehicle(self) -> None:
        input_size = 64
        cells = sum((input_size // stride) ** 2 for stride in (8, 16, 32))
        rows = [[0.0] * 85 for _ in range(cells)]
        # Stride-8 cell (x=2, y=3): a person at the centre of that cell.
        index = 3 * 8 + 2
        rows[index][0:4] = [0.5, 0.5, 0.0, 0.0]
        rows[index][4] = 0.9
        rows[index][5 + 0] = 0.95
        # A car in the first stride-16 cell, plus a duplicate box to suppress.
        offset = 64
        for duplicate in (0, 1):
            # Large boxes from neighbouring cells overlap almost completely.
            rows[offset + duplicate][0:5] = [0.5, 0.5, 1.4, 1.4, 0.8]
            rows[offset + duplicate][5 + 2] = 0.9 - duplicate * 0.1
        rows[offset + 1][0] = 0.4
        detections = decode_yolox(rows, input_size=input_size, ratio=0.5, frame_width=128, frame_height=128, minimum_confidence=0.3)
        classes = sorted(item.subject_class for item in detections)
        self.assertEqual(classes, ["car", "person"])
        person = next(item for item in detections if item.subject_class == "person")
        self.assertAlmostEqual(person.center[0], (2.5 * 8 / 0.5) / 128)
        self.assertAlmostEqual(person.center[1], (3.5 * 8 / 0.5) / 128)
        with self.assertRaises(ValueError):
            decode_yolox(rows[:-1], input_size=input_size, ratio=1, frame_width=1, frame_height=1, minimum_confidence=0.3)

    def test_nms_keeps_other_classes(self) -> None:
        box = (0.1, 0.1, 0.5, 0.5)
        kept = non_max_suppression([("person", 0.9, box), ("person", 0.8, box), ("car", 0.7, box), ("truck", 0.6, box)])
        self.assertEqual([item[0] for item in kept], ["person", "car"])

    @unittest.skipUnless(__import__("importlib.util").util.find_spec("numpy"), "numpy not installed")
    def test_vectorised_decode_matches_reference(self) -> None:
        import numpy

        from syncam_ai.site_worker.detector import decode_yolox_fast

        generator = numpy.random.default_rng(7)
        output = generator.uniform(-1, 1, size=(8400, 85)).astype(numpy.float32)
        output[:, 4:] = generator.uniform(0, 0.2, size=(8400, 81))
        for cell, cls in ((100, 0), (5000, 2), (8000, 7)):
            output[cell, 4] = 0.95
            output[cell, 5 + cls] = 0.9
        expected = decode_yolox(output.tolist(), input_size=640, ratio=0.5, frame_width=1280, frame_height=720, minimum_confidence=0.4)
        actual = decode_yolox_fast(output, input_size=640, ratio=0.5, frame_width=1280, frame_height=720, minimum_confidence=0.4)
        self.assertEqual([item.subject_class for item in expected], [item.subject_class for item in actual])
        for left, right in zip(expected, actual):
            self.assertAlmostEqual(left.left, right.left, places=4)
            self.assertAlmostEqual(left.bottom, right.bottom, places=4)


class ArtifactTest(unittest.TestCase):
    def test_hash_license_and_dev_only_gates(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "model.onnx"
            path.write_bytes(b"weights")
            import hashlib

            digest = hashlib.sha384(b"weights").hexdigest()
            good = ModelArtifact("m", "Apache-2.0", "src", (("onnx", digest),))
            verify_model_artifact(good, {"onnx": path})
            with self.assertRaises(ValueError):
                verify_model_artifact(ModelArtifact("m", "AGPL-3.0", "src", (("onnx", digest),)), {"onnx": path})
            with self.assertRaises(ValueError):
                verify_model_artifact(ModelArtifact("m", "Apache-2.0", "src", (("onnx", "0" * 96),)), {"onnx": path})
            dev = ModelArtifact("m", "unknown", "src", (("onnx", digest),), dev_only=True)
            with self.assertRaises(ValueError):
                verify_model_artifact(dev, {"onnx": path})
            verify_model_artifact(dev, {"onnx": path}, allow_dev_only=True)
            with self.assertRaises(ValueError):
                verify_model_artifact(good, {"onnx": path.with_name("missing.onnx")})


class ConfigTest(unittest.TestCase):
    def test_parses_and_validates(self) -> None:
        raw = {"backend_url": "https://api.example", "cameras": {CAMERA: "rtsp://u:p@10.0.0.2/stream"}, "models": {"detector": "D:/m/yolox_s.onnx"}}
        config = parse_local_config(raw, environ={"SYNCAM_SITE_KEY": "sck_secret"})
        self.assertEqual(config.site_key, "sck_secret")
        self.assertEqual(config.fps, 4.0)
        with self.assertRaises(ValueError):
            parse_local_config(raw, environ={})
        with self.assertRaises(ValueError):
            parse_local_config({**raw, "cameras": {"gate": "rtsp://x"}}, environ={"SYNCAM_SITE_KEY": "sck_x"})
        with self.assertRaises(ValueError):
            parse_local_config({**raw, "fps": 60}, environ={"SYNCAM_SITE_KEY": "sck_x"})


class _Handler(BaseHTTPRequestHandler):
    calls: list = []
    fail_events = 0

    def log_message(self, *args) -> None:  # silence test output
        pass

    def _reply(self, status, payload=None):
        body = json.dumps(payload).encode() if payload is not None else b""
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        type(self).calls.append(("GET", self.path, self.headers.get("Authorization"), b""))
        self._reply(200, {"data": {"tenant_id": TENANT, "site_id": SITE, "site": {"name": "Warehouse"}, "cameras": [{"id": CAMERA, "name": "Gate"}], "zones": [zone(YARD, "intrusion", name="Yard")]}})

    def _body(self):
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def do_POST(self):
        body = self._body()
        type(self).calls.append(("POST", self.path, self.headers.get("Authorization"), body))
        if self.path.endswith("/evidence"):
            self._reply(201, {"data": {"ref": "snapshot:" + TENANT + "/x.jpg"}})
        elif self.path.endswith("/events"):
            if type(self).fail_events > 0:
                type(self).fail_events -= 1
                self._reply(503, {"error": {"message": "waking up"}})
            else:
                self._reply(202, {"data": {"accepted": True}})
        elif self.path.endswith("/heartbeat"):
            self._reply(200, {"data": {}})
        else:
            self._reply(404, {"error": {"message": "nope"}})

    def do_PUT(self):
        type(self).calls.append(("PUT", self.path, self.headers.get("Authorization"), self._body()))
        self.send_response(204)
        self.end_headers()


class BackendFixture(unittest.TestCase):
    def setUp(self) -> None:
        _Handler.calls = []
        _Handler.fail_events = 0
        self.server = HTTPServer(("127.0.0.1", 0), _Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.client = BackendClient(f"http://127.0.0.1:{self.server.server_port}", "sck_test-key")

    def tearDown(self) -> None:
        self.server.shutdown()
        self.server.server_close()


class ClientAndSpoolTest(BackendFixture):
    def test_client_rejects_plain_http_and_bad_keys(self) -> None:
        with self.assertRaises(ValueError):
            BackendClient("http://api.example", "sck_x")
        with self.assertRaises(ValueError):
            BackendClient("https://api.example", "not-a-key")

    def test_spool_uploads_snapshot_then_event_and_retries(self) -> None:
        _Handler.fail_events = 1
        with tempfile.TemporaryDirectory() as directory:
            spool = Spool(pathlib.Path(directory))
            event = camera_health_event(TENANT, SITE, CAMERA, START)
            spool.enqueue(event, b"\xff\xd8\xffjpeg")
            first = spool.deliver(self.client)
            self.assertEqual((first.delivered, first.pending), (0, 1))
            second = spool.deliver(self.client)
            self.assertEqual((second.delivered, second.pending), (1, 0))
        uploads = [call for call in _Handler.calls if call[1].endswith("/evidence")]
        self.assertEqual(len(uploads), 1, "the snapshot must be uploaded once across retries")
        posted = json.loads([call for call in _Handler.calls if call[1].endswith("/events")][-1][3])
        self.assertEqual(posted["evidence_refs"], ["snapshot:" + TENANT + "/x.jpg"])
        self.assertTrue(all(call[2] == "Bearer sck_test-key" for call in _Handler.calls))

    def test_permanent_rejection_is_dropped(self) -> None:
        class Rejecting:
            def upload_evidence(self, jpeg):
                return "snapshot:x"

            def post_event(self, event):
                raise BackendError("invalid", status=422, retryable=False)

        with tempfile.TemporaryDirectory() as directory:
            spool = Spool(pathlib.Path(directory))
            spool.enqueue(camera_health_event(TENANT, SITE, CAMERA, START), None)
            result = spool.deliver(Rejecting())
            self.assertEqual((result.dropped, result.pending), (1, 0))
            spool.enqueue(camera_health_event(TENANT, SITE, CAMERA, START + timedelta(hours=1)), None)
            expired = spool.deliver(Rejecting(), now=10 ** 12)
            self.assertEqual(expired.dropped, 1)


class _Health:
    def __init__(self, online):
        self.online = online
        self.last_frame_at = START if online else None
        self.error = "" if online else "cannot connect to camera"


class _Stream:
    camera_id = CAMERA

    def __init__(self):
        self.frames = []
        self.online = True

    def latest(self):
        return self.frames[-1] if self.frames else None

    def health(self):
        return _Health(self.online)


class _Detector:
    model_version = "yolox-s-coco-0.1.1rc0"

    def __init__(self):
        self.next = []

    def detect(self, frame, *, minimum_confidence=0.45):
        return self.next


class RunnerTest(BackendFixture):
    def test_end_to_end_intrusion_and_camera_offline(self) -> None:
        clock = [0.0]
        stream, detector = _Stream(), _Detector()
        with tempfile.TemporaryDirectory() as directory:
            worker = SiteWorker(
                backend=self.client, detector=detector, streams={CAMERA: stream}, spool=Spool(pathlib.Path(directory)),
                fps=4, snapshot=lambda *args: b"\xff\xd8\xffsnap", reference=lambda frame: b"\xff\xd8\xffref",
                clock=lambda: clock[0], now=lambda: START,
            )

            def step(x, seconds):
                clock[0] += 1
                detector.next = [ObjectDetection("person", 0.9, x - 0.15, 0.3, x + 0.15, 0.7)]
                stream.frames.append((len(stream.frames) + 1, object(), START + timedelta(seconds=seconds)))
                return worker.tick()

            self.assertEqual(step(0.30, 0), 0)
            self.assertEqual(step(0.40, 1), 0)
            self.assertEqual(step(0.55, 2), 1, "walking into the yard raises one intrusion alert")
            stream.online = False
            clock[0] += 1
            self.assertEqual(worker.tick(), 0)
            clock[0] += 61
            self.assertEqual(worker.tick(), 1, "a camera offline for over a minute raises one alert")
            clock[0] += 61
            self.assertEqual(worker.tick(), 0)
            worker.deliver_pending()
        events = [json.loads(call[3]) for call in _Handler.calls if call[1].endswith("/events")]
        self.assertEqual(sorted(event["event_type"] for event in events), ["camera_health", "intrusion"])
        intrusion = next(event for event in events if event["event_type"] == "intrusion")
        self.assertEqual(intrusion["model_version"], "yolox-s-coco-0.1.1rc0")
        self.assertTrue(intrusion["evidence_refs"])
        paths = [call[1] for call in _Handler.calls]
        self.assertIn("/v1/site-ingest/config", paths)
        self.assertIn("/v1/site-ingest/heartbeat", paths)
        self.assertIn(f"/v1/site-ingest/cameras/{CAMERA}/reference", paths)


if __name__ == "__main__":
    unittest.main()
