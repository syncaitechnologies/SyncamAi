import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.vehicle_activity import (  # noqa: E402
    VEHICLE_CLASSES,
    VehicleTrackObservation,
    build_vehicle_activity_event,
)


def observation() -> VehicleTrackObservation:
    return VehicleTrackObservation(
        tenant_id="11111111-1111-4111-8111-111111111111",
        site_id="22222222-2222-4222-8222-222222222222",
        camera_id="33333333-3333-4333-8333-333333333333",
        zone_id="44444444-4444-4444-8444-444444444444",
        track_id=42,
        first_seen_at=datetime(2026, 8, 15, 6, 30, tzinfo=timezone.utc),
        subject_class=" car ",
        confidence=0.93,
        model_version=" shared-detector-1 ",
        evidence_refs=(" evidence://vehicle-42 ",),
    )


class VehicleActivityTest(unittest.TestCase):
    def test_builds_review_required_event_without_identity_or_theft_claims(self) -> None:
        event = build_vehicle_activity_event(observation())

        self.assertEqual(event["event_type"], "vehicle_activity")
        self.assertEqual(event["observed_behavior"], "detected")
        self.assertEqual(event["subject_class"], "car")
        self.assertEqual(event["model_version"], "shared-detector-1")
        self.assertEqual(event["evidence_refs"], ["evidence://vehicle-42"])
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in ("theft", "plate", "lpr", "reid", "speed", "embedding"):
            self.assertNotIn(prohibited, serialized)

    def test_retries_are_stable_and_classes_are_bounded(self) -> None:
        first = build_vehicle_activity_event(observation())
        self.assertEqual(first, build_vehicle_activity_event(observation()))
        for subject_class in VEHICLE_CLASSES:
            event = build_vehicle_activity_event(
                replace(observation(), subject_class=subject_class)
            )
            self.assertEqual(event["subject_class"], subject_class)
        changed = build_vehicle_activity_event(replace(observation(), track_id=43))
        self.assertNotEqual(first["event_id"], changed["event_id"])
        self.assertNotEqual(first["dedupe_key"], changed["dedupe_key"])

    def test_rejects_unbounded_or_identity_enriching_inputs(self) -> None:
        invalid = (
            replace(observation(), tenant_id="bad"),
            replace(observation(), track_id=-1),
            replace(observation(), track_id=True),
            replace(observation(), track_id=1.5),
            replace(observation(), first_seen_at=datetime(2026, 8, 15, 6, 30)),
            replace(observation(), subject_class="license_plate"),
            replace(observation(), confidence=float("nan")),
            replace(observation(), confidence=True),
            replace(observation(), confidence=1.01),
            replace(observation(), model_version=" "),
            replace(observation(), evidence_refs=(" ",)),
            replace(observation(), evidence_refs=tuple(f"evidence://{index}" for index in range(33))),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    build_vehicle_activity_event(value)


if __name__ == "__main__":
    unittest.main()
