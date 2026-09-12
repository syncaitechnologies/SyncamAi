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
    MAX_DETECTIONS_PER_FRAME,
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
START = datetime(2026, 9, 12, 12, 0, tzinfo=timezone.utc)


def detection(
    left: float = 0.1,
    right: float = 0.5,
    *,
    subject_class: str = "person",
    confidence: float = 0.9,
) -> Detection:
    return Detection(subject_class, confidence, left, 0.4, right, 0.6, "detector-candidate-1")


def frame(sequence: int, seconds: float, *detections: Detection) -> DetectionFrame:
    return DetectionFrame(TENANT, SITE, CAMERA, DEVICE, sequence, START + timedelta(seconds=seconds), tuple(detections))


class CameraLocalDetectionTrackerTest(unittest.TestCase):
    def test_associates_stable_camera_local_tracks_deterministically(self) -> None:
        tracker = CameraLocalDetectionTracker(minimum_iou=0.2)
        first = tracker.track(frame(1, 0, detection(), detection(0.7, 0.9, subject_class="car")))
        second = tracker.track(frame(2, 0.1, detection(0.2, 0.6), detection(0.68, 0.88, subject_class="car")))
        self.assertEqual([item.track_id for item in first.tracks], [1, 2])
        self.assertEqual([item.track_id for item in second.tracks], [1, 2])
        self.assertEqual([item.subject_class for item in second.tracks], ["car", "person"])
        self.assertEqual(tracker.metrics.accepted_frames, 2)
        self.assertEqual(tracker.metrics.created_tracks, 2)
        self.assertEqual(tracker.metrics.matched_detections, 2)
        self.assertEqual(tracker.metrics.active_tracks, 2)

        reversed_tracker = CameraLocalDetectionTracker(minimum_iou=0.2)
        reordered = reversed_tracker.track(
            frame(1, 0, detection(0.7, 0.9, subject_class="car"), detection())
        )
        self.assertEqual(
            [(item.track_id, item.subject_class) for item in reordered.tracks],
            [(item.track_id, item.subject_class) for item in first.tracks],
        )

    def test_expires_missed_tracks_without_reusing_identifiers(self) -> None:
        tracker = CameraLocalDetectionTracker(max_missed_frames=1)
        self.assertEqual(tracker.track(frame(1, 0, detection())).tracks[0].track_id, 1)
        self.assertEqual(tracker.track(frame(2, 0.1)).tracks, ())
        self.assertEqual(tracker.track(frame(3, 0.2)).tracks, ())
        new = tracker.track(frame(4, 0.3, detection()))
        self.assertEqual(new.tracks[0].track_id, 2)
        self.assertEqual(tracker.metrics.expired_tracks, 1)

    def test_rejects_invalid_or_out_of_order_input_without_mutation(self) -> None:
        tracker = CameraLocalDetectionTracker()
        tracker.track(frame(1, 0, detection()))
        before = tracker.metrics
        invalid = (
            frame(1, 1, detection()),
            frame(2, -1, detection()),
            replace(frame(2, 1, detection()), camera_id=DEVICE),
            frame(2, 1, replace(detection(), subject_class="face")),
            frame(2, 1, replace(detection(), left=0.8, right=0.2)),
            frame(2, 1, replace(detection(), confidence=float("nan"))),
            frame(2, 1, replace(detection(), model_version="private model details")),
            replace(frame(2, 1), detections=tuple(detection() for _ in range(MAX_DETECTIONS_PER_FRAME + 1))),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    tracker.track(value)
                self.assertEqual(tracker.metrics, before)

    def test_capacity_rejection_is_atomic(self) -> None:
        tracker = CameraLocalDetectionTracker(max_active_tracks=1, max_missed_frames=1)
        tracker.track(frame(1, 0, detection()))
        before = tracker.metrics
        with self.assertRaises(ValueError):
            tracker.track(frame(2, 0.1, detection(0.7, 0.9, subject_class="car")))
        self.assertEqual(tracker.metrics, before)
        resumed = tracker.track(frame(2, 0.1, detection(0.12, 0.52)))
        self.assertEqual(resumed.tracks[0].track_id, 1)

    def test_composes_with_track_ingress_and_zone_runtime(self) -> None:
        runtime = ZoneRuntime()
        runtime.activate_verified_configuration(1, {
            "zones": [{
                "id": ZONE,
                "tenant_id": TENANT,
                "site_id": SITE,
                "camera_id": CAMERA,
                "kind": "intrusion",
                "enabled": True,
                "geometry": {"type": "Polygon", "coordinates": [[[0.4, 0], [1, 0], [1, 1], [0.4, 1], [0.4, 0]]]},
            }]
        })
        ingress = SampledTrackIngress(runtime, TrackIngressLimits(10, 100), sample_fps=10)
        tracker = CameraLocalDetectionTracker(minimum_iou=0.2)
        self.assertEqual(ingress.ingest(tracker.track(frame(1, 0, detection(0.1, 0.5)))), [])
        events = ingress.ingest(tracker.track(frame(2, 0.1, detection(0.3, 0.7))))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "intrusion")
        self.assertNotIn("track_id", events[0])
        self.assertNotIn("coordinates", events[0])

    def test_constructor_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"minimum_iou": 0},
            {"minimum_iou": float("inf")},
            {"max_missed_frames": 31},
            {"max_active_tracks": 0},
        ):
            with self.assertRaises(ValueError):
                CameraLocalDetectionTracker(**kwargs)


if __name__ == "__main__":
    unittest.main()
