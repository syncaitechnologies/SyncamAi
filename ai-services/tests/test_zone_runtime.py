import pathlib
import sys
import unittest
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.zone_rules import TrackObservation  # noqa: E402
from syncam_ai.zone_runtime import ZoneRuntime  # noqa: E402


BASE_TIME = datetime(2026, 8, 21, 8, 0, tzinfo=timezone.utc)
TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"


def payload(kind: str = "intrusion") -> dict[str, object]:
    geometry: dict[str, object] = {
        "type": "Polygon",
        "coordinates": [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]]],
    }
    if kind == "tripwire":
        geometry = {"type": "LineString", "coordinates": [[5, 0], [5, 10]]}
    return {
        "zones": [
            {
                "id": ZONE,
                "tenant_id": TENANT,
                "site_id": SITE,
                "camera_id": CAMERA,
                "kind": kind,
                "enabled": True,
                "geometry": geometry,
            }
        ]
    }


def observation(seconds: int, x: float, y: float) -> TrackObservation:
    return TrackObservation(
        tenant_id=TENANT,
        site_id=SITE,
        camera_id=CAMERA,
        track_id=42,
        observed_at=BASE_TIME + timedelta(seconds=seconds),
        center_x=x,
        center_y=y,
        subject_class="person",
        confidence=0.9,
        model_version="synthetic-tracker-1",
    )


class ZoneRuntimeTest(unittest.TestCase):
    def test_activates_verified_configuration_and_evaluates_its_rules(self) -> None:
        runtime = ZoneRuntime()
        self.assertEqual(runtime.observe(observation(0, 5, 5)), [])
        self.assertTrue(runtime.activate_verified_configuration(1, payload()))
        self.assertEqual(runtime.applied_revision, 1)
        self.assertEqual(runtime.observe(observation(0, -1, 5)), [])
        events = runtime.observe(observation(1, 5, 5))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "intrusion")

    def test_rejected_candidate_preserves_last_known_good_runtime(self) -> None:
        runtime = ZoneRuntime()
        runtime.activate_verified_configuration(1, payload())
        invalid = payload("mask")
        with self.assertRaises(ValueError):
            runtime.activate_verified_configuration(2, invalid)
        self.assertEqual(runtime.applied_revision, 1)
        self.assertEqual(runtime.observe(observation(0, -1, 5)), [])
        events = runtime.observe(observation(1, 5, 5))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "intrusion")

    def test_stale_or_replayed_revisions_cannot_replace_active_runtime(self) -> None:
        runtime = ZoneRuntime()
        runtime.activate_verified_configuration(2, payload("tripwire"))
        self.assertFalse(runtime.activate_verified_configuration(2, payload()))
        with self.assertRaises(ValueError):
            runtime.activate_verified_configuration(1, payload())
        self.assertEqual(runtime.observe(observation(0, 3, 5)), [])
        events = runtime.observe(observation(1, 7, 5))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "tripwire")

    def test_invalid_revision_or_payload_fails_closed(self) -> None:
        runtime = ZoneRuntime()
        for revision, config in ((0, payload()), (True, payload()), (1, {})):
            with self.subTest(revision=revision, config=config):
                with self.assertRaises(ValueError):
                    runtime.activate_verified_configuration(revision, config)  # type: ignore[arg-type]
        self.assertEqual(runtime.applied_revision, 0)


if __name__ == "__main__":
    unittest.main()
