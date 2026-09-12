import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.vehicle_activity import (  # noqa: E402
    VEHICLE_CLASSES,
    VehicleActivityConfirmation,
    VehicleTrackObservation,
    build_vehicle_activity_event,
)
from syncam_ai.detection_tracking import (  # noqa: E402
    CameraLocalDetectionTracker,
    Detection,
    DetectionFrame,
)
from syncam_ai.track_ingestion import TrackFrame  # noqa: E402
from syncam_ai.zone_rules import TrackObservation  # noqa: E402


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
DEVICE = "55555555-5555-4555-8555-555555555555"
START = datetime(2026, 9, 12, 12, 0, tzinfo=timezone.utc)


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

    def test_temporal_confirmation_emits_once_with_conservative_confidence(self) -> None:
        confirmation = VehicleActivityConfirmation(
            TENANT, SITE, CAMERA, ZONE, confirmation_frames=3
        )
        self.assertEqual(confirmation.ingest(track_frame(0, vehicle_track(0, 0.95))), [])
        self.assertEqual(confirmation.ingest(track_frame(1, vehicle_track(1, 0.9))), [])
        events = confirmation.ingest(track_frame(2, vehicle_track(2, 0.85)))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["confidence"], 0.85)
        self.assertEqual(events[0]["subject_class"], "car")
        self.assertIs(events[0]["requires_human_review"], True)
        self.assertEqual(confirmation.ingest(track_frame(3, vehicle_track(3, 0.99))), [])
        self.assertEqual(confirmation.metrics.confirmed_events, 1)
        self.assertEqual(confirmation.metrics.active_tracks, 1)

    def test_confirmation_rejects_replay_provenance_change_and_capacity_atomically(self) -> None:
        confirmation = VehicleActivityConfirmation(
            TENANT, SITE, CAMERA, ZONE, confirmation_frames=2, max_tracks=1
        )
        confirmation.ingest(track_frame(0, vehicle_track(0)))
        before = confirmation.metrics
        invalid_frames = (
            track_frame(0, vehicle_track(0)),
            track_frame(1, replace(vehicle_track(1), model_version="changed-model")),
            track_frame(1, vehicle_track(1), replace(vehicle_track(1), track_id=2)),
            replace(track_frame(1), camera_id=DEVICE),
            track_frame(1, replace(vehicle_track(1), track_id=-1)),
            track_frame(1, replace(vehicle_track(1), subject_class="face")),
            track_frame(1, replace(vehicle_track(1), center_x=float("nan"))),
            track_frame(1, replace(vehicle_track(1), model_version=" ")),
            track_frame(1, replace(vehicle_track(1), observed_at=datetime(2026, 9, 12, 12, 0))),
        )
        for value in invalid_frames:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    confirmation.ingest(value)
                self.assertEqual(confirmation.metrics, before)
        events = confirmation.ingest(track_frame(1, vehicle_track(1)))
        self.assertEqual(len(events), 1)

    def test_person_tracks_are_ignored_and_gaps_restart_confirmation(self) -> None:
        confirmation = VehicleActivityConfirmation(
            TENANT, SITE, CAMERA, ZONE, confirmation_frames=2, max_missed_frames=1
        )
        self.assertEqual(confirmation.ingest(track_frame(0, vehicle_track(0))), [])
        person = replace(vehicle_track(1), subject_class="person")
        self.assertEqual(confirmation.ingest(track_frame(1, person)), [])
        self.assertEqual(confirmation.ingest(track_frame(2, vehicle_track(2))), [])
        events = confirmation.ingest(track_frame(3, vehicle_track(3)))
        self.assertEqual(len(events), 1)
        self.assertEqual(confirmation.metrics.ignored_person_observations, 1)

    def test_detection_tracker_composes_into_vehicle_confirmation(self) -> None:
        tracker = CameraLocalDetectionTracker(minimum_iou=0.2)
        confirmation = VehicleActivityConfirmation(
            TENANT, SITE, CAMERA, ZONE, confirmation_frames=3
        )
        events: list[dict[str, object]] = []
        for sequence, offset in enumerate((0.0, 0.01, 0.02), start=1):
            detected = DetectionFrame(
                TENANT,
                SITE,
                CAMERA,
                DEVICE,
                sequence,
                START + timedelta(seconds=sequence),
                (Detection("car", 0.9, 0.1 + offset, 0.2, 0.5 + offset, 0.6, "detector-candidate-1"),),
            )
            events.extend(confirmation.ingest(tracker.track(detected)))
        self.assertEqual(len(events), 1)
        serialized = json.dumps(events[0], sort_keys=True).lower()
        for prohibited in ("track_id", "plate", "speed", "reid", "embedding", "theft"):
            self.assertNotIn(prohibited, serialized)

    def test_confirmation_configuration_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"confirmation_frames": 1},
            {"confirmation_frames": 11},
            {"max_missed_frames": 31},
            {"max_tracks": 0},
        ):
            with self.assertRaises(ValueError):
                VehicleActivityConfirmation(TENANT, SITE, CAMERA, ZONE, **kwargs)


def vehicle_track(seconds: int, confidence: float = 0.9) -> TrackObservation:
    return TrackObservation(
        TENANT,
        SITE,
        CAMERA,
        42,
        START + timedelta(seconds=seconds),
        0.4,
        0.5,
        "car",
        confidence,
        "detector-candidate-1",
    )


def track_frame(seconds: int, *tracks: TrackObservation) -> TrackFrame:
    return TrackFrame(TENANT, SITE, CAMERA, DEVICE, START + timedelta(seconds=seconds), tuple(tracks))


if __name__ == "__main__":
    unittest.main()
