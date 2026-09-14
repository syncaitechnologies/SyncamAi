import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.detection_tracking import (  # noqa: E402
    CameraLocalDetectionTracker,
    Detection,
    DetectionFrame,
)
from syncam_ai.track_ingestion import (  # noqa: E402
    SampledTrackIngress,
    TrackIngressLimits,
)
from syncam_ai.zone_runtime import ZoneRuntime  # noqa: E402


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
DEVICE = "44444444-4444-4444-8444-444444444444"
ZONE = "55555555-5555-4555-8555-555555555555"
START = datetime(2026, 9, 14, 9, 0, tzinfo=timezone.utc)
LIMITS = TrackIngressLimits(10, 2_560)


def configuration(
    *,
    loiter_seconds: int = 30,
    kind: str = "loitering",
    subject_classes: list[str] | None = None,
) -> dict[str, object]:
    return {
        "zones": [
            {
                "id": ZONE,
                "tenant_id": TENANT,
                "site_id": SITE,
                "camera_id": CAMERA,
                "kind": kind,
                "enabled": True,
                "loiter_seconds": loiter_seconds,
                "subject_classes": subject_classes or ["person", "car"],
                "geometry": {
                    "type": "Polygon",
                    "coordinates": [
                        [[0.35, 0.25], [0.85, 0.25], [0.85, 0.75], [0.35, 0.75], [0.35, 0.25]]
                    ],
                },
            }
        ]
    }


def detection(*, inside: bool, subject_class: str = "person") -> Detection:
    left, right = (0.18, 0.58) if inside else (0.05, 0.45)
    return Detection(
        subject_class,
        0.92,
        left,
        0.35,
        right,
        0.65,
        "synthetic-metadata-1",
    )


def frame(
    sequence: int,
    seconds: float,
    *detections: Detection,
) -> DetectionFrame:
    return DetectionFrame(
        TENANT,
        SITE,
        CAMERA,
        DEVICE,
        sequence,
        START + timedelta(seconds=seconds),
        tuple(detections),
    )


def composition(
    *, max_active_tracks: int = 512
) -> tuple[CameraLocalDetectionTracker, SampledTrackIngress, ZoneRuntime]:
    runtime = ZoneRuntime()
    runtime.activate_verified_configuration(1, configuration())
    tracker = CameraLocalDetectionTracker(
        minimum_iou=0.2,
        max_active_tracks=max_active_tracks,
    )
    ingress = SampledTrackIngress(runtime, LIMITS, sample_fps=10)
    return tracker, ingress, runtime


def ingest(
    tracker: CameraLocalDetectionTracker,
    ingress: SampledTrackIngress,
    candidate: DetectionFrame,
) -> list[dict[str, object]]:
    return ingress.ingest(tracker.track(candidate))


class LoiteringCompositionTest(unittest.TestCase):
    def test_person_or_permitted_vehicle_emits_once_at_thirty_seconds(self) -> None:
        for subject_class in ("person", "car"):
            with self.subTest(subject_class=subject_class):
                tracker, ingress, runtime = composition()
                self.assertEqual(
                    ingest(tracker, ingress, frame(1, 0, detection(inside=False, subject_class=subject_class))),
                    [],
                )
                self.assertEqual(
                    ingest(tracker, ingress, frame(2, 1, detection(inside=True, subject_class=subject_class))),
                    [],
                )
                self.assertEqual(
                    ingest(tracker, ingress, frame(3, 30.9, detection(inside=True, subject_class=subject_class))),
                    [],
                )
                events = ingest(
                    tracker,
                    ingress,
                    frame(4, 31, detection(inside=True, subject_class=subject_class)),
                )
                self.assertEqual(len(events), 1)
                self.assertEqual(
                    ingest(tracker, ingress, frame(5, 61, detection(inside=True, subject_class=subject_class))),
                    [],
                )

                event = events[0]
                self.assertEqual(event["tenant_id"], TENANT)
                self.assertEqual(event["site_id"], SITE)
                self.assertEqual(event["camera_id"], CAMERA)
                self.assertEqual(event["zone_id"], ZONE)
                self.assertEqual(event["event_type"], "loitering")
                self.assertEqual(event["observed_behavior"], "dwell_exceeded")
                self.assertEqual(event["subject_class"], subject_class)
                self.assertTrue(event["requires_human_review"])
                self.assertEqual(event["review_state"], "pending")
                self.assertEqual(event["evidence_refs"], [])
                self.assertEqual(event["dedupe_key"], f"loitering:{event['event_id']}")
                self.assertEqual(ingress.metrics.emitted_events, 1)
                self.assertEqual(runtime.track_state_count, 1)

                serialized = json.dumps(event, sort_keys=True).lower()
                for prohibited in (
                    "track_id",
                    "coordinate",
                    "center_x",
                    "center_y",
                    "left",
                    "right",
                    "top",
                    "bottom",
                    "pixel",
                    "image",
                    "identity",
                    "biometric",
                    "plate",
                    "embedding",
                    "reid",
                    "alarm",
                    "dispatch",
                    "access_control",
                    "autonomous",
                ):
                    self.assertNotIn(prohibited, serialized)

    def test_early_exit_resets_dwell_and_reentry_starts_a_new_window(self) -> None:
        tracker, ingress, _ = composition()
        schedule = (
            (1, 0, False),
            (2, 1, True),
            (3, 20, False),
            (4, 21, True),
            (5, 31, True),
        )
        for sequence, seconds, inside in schedule:
            self.assertEqual(
                ingest(tracker, ingress, frame(sequence, seconds, detection(inside=inside))),
                [],
            )
        events = ingest(tracker, ingress, frame(6, 51, detection(inside=True)))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["occurred_at"], "2026-09-14T09:00:51.000000Z")

    def test_retry_is_deterministic_and_replay_cannot_duplicate_the_event(self) -> None:
        def run() -> tuple[dict[str, object], CameraLocalDetectionTracker, SampledTrackIngress]:
            tracker, ingress, _ = composition()
            ingest(tracker, ingress, frame(1, 0, detection(inside=False)))
            ingest(tracker, ingress, frame(2, 1, detection(inside=True)))
            event = ingest(tracker, ingress, frame(3, 31, detection(inside=True)))[0]
            return event, tracker, ingress

        first, tracker, ingress = run()
        second, _, _ = run()
        self.assertEqual(first, second)
        before_tracker = tracker.metrics
        before_ingress = ingress.metrics
        with self.assertRaisesRegex(ValueError, "sequence must increase"):
            ingest(tracker, ingress, frame(3, 31, detection(inside=True)))
        self.assertEqual(tracker.metrics, before_tracker)
        self.assertEqual(ingress.metrics, before_ingress)

    def test_ordering_scope_and_malformed_metadata_fail_before_downstream_state(self) -> None:
        tracker, ingress, runtime = composition()
        ingest(tracker, ingress, frame(1, 0, detection(inside=False)))
        before_tracker = tracker.metrics
        before_ingress = ingress.metrics
        before_states = runtime.track_state_count
        invalid = (
            frame(2, -1, detection(inside=True)),
            replace(frame(2, 1, detection(inside=True)), camera_id=DEVICE),
            frame(2, 1, replace(detection(inside=True), subject_class="face")),
            frame(2, 1, replace(detection(inside=True), confidence=float("nan"))),
            frame(2, 1, replace(detection(inside=True), model_version="private model details")),
        )
        for candidate in invalid:
            with self.subTest(candidate=candidate):
                with self.assertRaises(ValueError):
                    ingest(tracker, ingress, candidate)
                self.assertEqual(tracker.metrics, before_tracker)
                self.assertEqual(ingress.metrics, before_ingress)
                self.assertEqual(runtime.track_state_count, before_states)

        self.assertEqual(
            ingest(tracker, ingress, frame(2, 1, detection(inside=True))),
            [],
        )

    def test_tracker_capacity_rejection_is_atomic_for_the_composition(self) -> None:
        tracker, ingress, runtime = composition(max_active_tracks=1)
        before_tracker = tracker.metrics
        with self.assertRaisesRegex(ValueError, "capacity"):
            ingest(
                tracker,
                ingress,
                frame(
                    1,
                    0,
                    detection(inside=False),
                    detection(inside=True, subject_class="car"),
                ),
            )
        self.assertEqual(tracker.metrics, before_tracker)
        self.assertEqual(ingress.metrics.accepted_frames, 0)
        self.assertEqual(runtime.track_state_count, 0)
        self.assertEqual(
            ingest(tracker, ingress, frame(1, 0, detection(inside=False))),
            [],
        )

    def test_invalid_loitering_configuration_never_activates(self) -> None:
        invalid = (
            configuration(loiter_seconds=29),
            configuration(loiter_seconds=601),
            configuration(kind="mask"),
            configuration(subject_classes=["face"]),
        )
        for candidate in invalid:
            with self.subTest(candidate=candidate):
                runtime = ZoneRuntime()
                with self.assertRaises(ValueError):
                    runtime.activate_verified_configuration(1, candidate)
                self.assertEqual(runtime.applied_revision, 0)
                self.assertEqual(runtime.track_state_count, 0)


if __name__ == "__main__":
    unittest.main()
