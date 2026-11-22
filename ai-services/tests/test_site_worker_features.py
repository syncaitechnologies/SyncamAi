import json
import pathlib
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.client import BackendClient  # noqa: E402
from syncam_ai.site_worker.detector import ObjectDetection  # noqa: E402
from syncam_ai.site_worker.features import FeatureGate, capabilities  # noqa: E402
from syncam_ai.site_worker.runner import SiteWorker  # noqa: E402
from syncam_ai.site_worker.spool import Spool  # noqa: E402

TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
GATE = "33333333-3333-4333-8333-333333333333"
YARD_CAMERA = "34333333-3333-4333-8333-333333333333"
ZONE = "55555555-5555-4555-8555-555555555555"
SQUARE = {"type": "Polygon", "coordinates": [[[0.5, 0.0], [1.0, 0.0], [1.0, 1.0], [0.5, 1.0], [0.5, 0.0]]]}
START = datetime(2026, 11, 16, 23, 0, tzinfo=timezone.utc)
PERSON = ObjectDetection(subject_class="person", confidence=0.9, left=0.6, top=0.3, right=0.8, bottom=0.9)


class _Detector:
    model_version = "yolox-s-coco-0.1.1rc0"

    def detect(self, frame, *, minimum_confidence=0.45):
        return [PERSON]


class _WeaponProcessor:
    feature = "weapon"

    def __init__(self):
        self.cameras = []

    def process(self, scope, camera_id, frame, detections, observed_at):
        self.cameras.append(camera_id)
        return []


class _Backend:
    def __init__(self, features=None):
        self.features = features
        self.heartbeats = []

    def config(self):
        zones = [
            {"id": ZONE, "tenant_id": TENANT, "site_id": SITE, "camera_id": camera, "name": "Yard", "kind": "restricted_zone",
             "geometry": SQUARE, "enabled": True, "subject_classes": ["person"]}
            for camera in (GATE, YARD_CAMERA)
        ]
        zones[1] = dict(zones[1], id="56555555-5555-4555-8555-555555555555")
        data = {"tenant_id": TENANT, "site_id": SITE, "site": {"name": "Shop"},
                "cameras": [{"id": GATE, "name": "Gate"}, {"id": YARD_CAMERA, "name": "Yard"}], "zones": zones}
        if self.features is not None:
            data["features"] = self.features
        return data

    def heartbeat(self, cameras, capabilities=None):
        self.heartbeats.append(capabilities)


def run_frames(worker, camera_id, count=4):
    queued = 0
    for second in range(count):
        queued += worker.process_frame(camera_id, None, START + timedelta(seconds=second))
    return queued


class FeatureGateTest(unittest.TestCase):
    def test_a_backend_without_features_leaves_everything_on(self) -> None:
        gate = FeatureGate.from_config(None)
        self.assertTrue(gate.allows("weapon", GATE))
        self.assertTrue(gate.allows_event("intrusion", GATE))

    def test_switches_and_chosen_cameras(self) -> None:
        gate = FeatureGate.from_config({
            "weapon": {"on": True, "cameras": [GATE]},
            "zone_alerts": {"on": False, "cameras": None},
            "loitering": {"on": True, "cameras": None},
        })
        self.assertTrue(gate.allows("weapon", GATE))
        self.assertFalse(gate.allows("weapon", YARD_CAMERA))
        self.assertFalse(gate.allows_event("intrusion", GATE))
        self.assertFalse(gate.allows_event("restricted_zone", GATE))
        self.assertTrue(gate.allows_event("loitering", GATE))
        self.assertTrue(gate.allows_event("camera_health", GATE), "camera offline alerts are never filtered")
        self.assertEqual(gate.describe(), {"loitering": "on", "weapon": "1 camera(s)", "zone_alerts": "off"})

    def test_capabilities_list_what_the_box_loaded(self) -> None:
        self.assertEqual(capabilities([None, "weapon"], live_view=True), ["zone_alerts", "loitering", "camera_health", "weapon", "live_view"])
        self.assertEqual(capabilities([], live_view=False), ["zone_alerts", "loitering", "camera_health"])


class WorkerFeatureTest(unittest.TestCase):
    def worker(self, backend, processors=()):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        worker = SiteWorker(backend=backend, detector=_Detector(), streams={}, spool=Spool(pathlib.Path(directory.name)),
                            processors=processors, now=lambda: START)
        worker.refresh_config()
        return worker

    def test_zone_alerts_follow_the_dashboard_switch(self) -> None:
        backend = _Backend()
        worker = self.worker(backend)
        self.assertEqual(run_frames(worker, GATE), 1, "with no switches the zone alert fires as before")

        backend.features = {"zone_alerts": {"on": True, "cameras": [YARD_CAMERA]}}
        worker = self.worker(backend)
        self.assertEqual(run_frames(worker, GATE), 0, "zone alerts are off on cameras that were not chosen")
        self.assertEqual(run_frames(worker, YARD_CAMERA), 1, "and still on for the chosen camera")

    def test_processors_are_skipped_where_their_feature_is_off_without_a_restart(self) -> None:
        backend = _Backend({"weapon": {"on": True, "cameras": None}})
        weapons = _WeaponProcessor()
        worker = self.worker(backend, [weapons])
        worker.process_frame(GATE, None, START)
        self.assertEqual(weapons.cameras, [GATE])

        backend.features = {"weapon": {"on": False, "cameras": None}}
        worker.refresh_config()
        worker.process_frame(GATE, None, START + timedelta(seconds=1))
        self.assertEqual(weapons.cameras, [GATE], "switched off from the dashboard, the next config refresh stops it")

    def test_heartbeat_reports_capabilities(self) -> None:
        backend = _Backend()
        worker = self.worker(backend, [_WeaponProcessor()])
        worker._send_heartbeat()
        self.assertEqual(backend.heartbeats, [["zone_alerts", "loitering", "camera_health", "weapon", "live_view"]])

    def test_client_sends_capabilities_only_when_given(self) -> None:
        sent = []

        class _Client(BackendClient):
            def _request(self, method, path, *, body=None, content_type=None, **_):
                sent.append(json.loads(body))
                return {}

        client = _Client("https://api.example.test", "sck_test_placeholder")
        client.heartbeat({}, capabilities=["zone_alerts"])
        client.heartbeat({})
        self.assertEqual(sent, [{"cameras": {}, "capabilities": ["zone_alerts"]}, {"cameras": {}}])


if __name__ == "__main__":
    unittest.main()
