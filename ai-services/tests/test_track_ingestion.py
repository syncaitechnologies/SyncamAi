import pathlib
import sys
import unittest
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.track_ingestion import (  # noqa: E402
    MAX_TRACKS_PER_FRAME,
    SampledTrackIngress,
    TrackFrame,
)
from syncam_ai.zone_rules import TrackObservation  # noqa: E402
from syncam_ai.zone_runtime import ZoneRuntime  # noqa: E402


BASE_TIME = datetime(2026, 8, 21, 8, 0, tzinfo=timezone.utc)
TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
OTHER_CAMERA = "55555555-5555-4555-8555-555555555555"
ZONE = "44444444-4444-4444-8444-444444444444"


def configuration() -> dict[str, object]:
    return {
        "zones": [
            {
                "id": ZONE,
                "tenant_id": TENANT,
                "site_id": SITE,
                "camera_id": CAMERA,
                "kind": "intrusion",
                "enabled": True,
                "geometry": {
                    "type": "Polygon",
                    "coordinates": [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]]],
                },
            }
        ]
    }


def track(at: datetime, x: float, camera_id: str = CAMERA, track_id: int = 42) -> TrackObservation:
    return TrackObservation(
        tenant_id=TENANT,
        site_id=SITE,
        camera_id=camera_id,
        track_id=track_id,
        observed_at=at,
        center_x=x,
        center_y=5,
        subject_class="person",
        confidence=0.9,
        model_version="synthetic-tracker-1",
    )


def frame(at: datetime, *tracks: TrackObservation) -> TrackFrame:
    return TrackFrame(TENANT, SITE, CAMERA, at, tuple(tracks))


class SampledTrackIngressTest(unittest.TestCase):
    def test_samples_at_ten_fps_and_suppresses_excess_metadata(self) -> None:
        runtime = ZoneRuntime()
        runtime.activate_verified_configuration(1, configuration())
        ingress = SampledTrackIngress(runtime, sample_fps=10)
        self.assertEqual(ingress.ingest(frame(BASE_TIME, track(BASE_TIME, -1))), [])
        suppressed_at = BASE_TIME + timedelta(milliseconds=50)
        self.assertEqual(ingress.ingest(frame(suppressed_at, track(suppressed_at, 5))), [])
        sampled_at = BASE_TIME + timedelta(milliseconds=100)
        events = ingress.ingest(frame(sampled_at, track(sampled_at, 5)))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "intrusion")
        self.assertNotIn("track_id", events[0])
        self.assertNotIn("center_x", events[0])
        self.assertEqual(ingress.metrics.accepted_frames, 3)
        self.assertEqual(ingress.metrics.sampled_frames, 2)
        self.assertEqual(ingress.metrics.suppressed_frames, 1)
        self.assertEqual(ingress.metrics.emitted_events, 1)

    def test_only_the_supported_five_to_ten_fps_range_is_accepted(self) -> None:
        runtime = ZoneRuntime()
        for rate in (5, 10):
            with self.subTest(rate=rate):
                SampledTrackIngress(runtime, sample_fps=rate)
        for rate in (4, 11, True):
            with self.subTest(rate=rate):
                with self.assertRaises(ValueError):
                    SampledTrackIngress(runtime, sample_fps=rate)  # type: ignore[arg-type]

    def test_mixed_scope_duplicate_or_misaligned_batches_fail_before_sampling(self) -> None:
        ingress = SampledTrackIngress(ZoneRuntime())
        alternate = track(BASE_TIME, 5, camera_id=OTHER_CAMERA)
        duplicate = track(BASE_TIME, 5)
        delayed = track(BASE_TIME + timedelta(milliseconds=1), 5)
        for candidate in (
            frame(BASE_TIME, alternate),
            frame(BASE_TIME, duplicate, duplicate),
            frame(BASE_TIME, delayed),
        ):
            with self.subTest(candidate=candidate):
                with self.assertRaises(ValueError):
                    ingress.ingest(candidate)
        self.assertEqual(ingress.metrics.accepted_frames, 0)

    def test_out_of_order_frames_fail_closed_even_when_suppressed(self) -> None:
        ingress = SampledTrackIngress(ZoneRuntime(), sample_fps=10)
        ingress.ingest(frame(BASE_TIME, track(BASE_TIME, -1)))
        later = BASE_TIME + timedelta(milliseconds=50)
        ingress.ingest(frame(later, track(later, -1)))
        earlier = BASE_TIME + timedelta(milliseconds=25)
        with self.assertRaises(ValueError):
            ingress.ingest(frame(earlier, track(earlier, -1)))
        self.assertEqual(ingress.metrics.accepted_frames, 2)
        self.assertEqual(ingress.metrics.suppressed_frames, 1)

    def test_empty_metadata_frame_is_bounded_and_emits_nothing_without_runtime_config(self) -> None:
        ingress = SampledTrackIngress(ZoneRuntime())
        self.assertEqual(ingress.ingest(frame(BASE_TIME)), [])
        self.assertEqual(ingress.metrics.sampled_frames, 1)

    def test_track_count_bound_fails_closed(self) -> None:
        ingress = SampledTrackIngress(ZoneRuntime())
        tracks = tuple(track(BASE_TIME, 5, track_id=index) for index in range(MAX_TRACKS_PER_FRAME + 1))
        with self.assertRaises(ValueError):
            ingress.ingest(frame(BASE_TIME, *tracks))


if __name__ == "__main__":
    unittest.main()
