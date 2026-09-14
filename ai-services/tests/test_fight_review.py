import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.fight_review import (  # noqa: E402
    MAX_TRACKS_PER_FRAME,
    FightReviewConfirmation,
    FightTrackFrame,
    FightTrackObservation,
)


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
START = datetime(2026, 9, 14, 12, 0, tzinfo=timezone.utc)


def track(
    track_id: int,
    motion_state: str = "aggressive",
    *,
    cluster_id: int = 7,
    confidence: float = 0.9,
    model_version: str = "synthetic-motion-1",
) -> FightTrackObservation:
    return FightTrackObservation(
        track_id,
        cluster_id,
        motion_state,
        confidence,
        model_version,
    )


def frame(
    sequence: int,
    *tracks: FightTrackObservation,
    seconds: float | None = None,
) -> FightTrackFrame:
    elapsed = sequence / 10 if seconds is None else seconds
    return FightTrackFrame(
        TENANT,
        SITE,
        CAMERA,
        ZONE,
        sequence,
        START + timedelta(seconds=elapsed),
        tuple(tracks),
    )


def confirm_at_boundary(
    confirmation: FightReviewConfirmation,
    *,
    cluster_id: int = 7,
    track_ids: tuple[int, int] = (10, 20),
    start_sequence: int = 1,
    start_seconds: float = 0,
) -> dict[str, object]:
    first, second = track_ids
    confirmation.confirm(
        frame(
            start_sequence,
            track(first, cluster_id=cluster_id, confidence=0.96),
            track(second, cluster_id=cluster_id, confidence=0.93),
            seconds=start_seconds,
        )
    )
    confirmation.confirm(
        frame(
            start_sequence + 1,
            track(first, cluster_id=cluster_id, confidence=0.9),
            track(second, cluster_id=cluster_id, confidence=0.88),
            seconds=start_seconds + 0.5,
        )
    )
    return confirmation.confirm(
        frame(
            start_sequence + 2,
            track(first, cluster_id=cluster_id, confidence=0.87),
            track(second, cluster_id=cluster_id, confidence=0.84),
            seconds=start_seconds + 1,
        )
    )[0]


class FightReviewConfirmationTest(unittest.TestCase):
    def test_requires_two_tracks_for_exactly_one_sustained_second(self) -> None:
        confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        self.assertEqual(
            confirmation.confirm(
                frame(
                    1,
                    track(10, confidence=0.96),
                    track(20, confidence=0.93),
                    seconds=0,
                )
            ),
            [],
        )
        self.assertEqual(
            confirmation.confirm(
                frame(
                    2,
                    track(10, confidence=0.9),
                    track(20, confidence=0.88),
                    seconds=0.5,
                )
            ),
            [],
        )
        events = confirmation.confirm(
            frame(
                3,
                track(10, confidence=0.87),
                track(20, confidence=0.84),
                seconds=1,
            )
        )
        self.assertEqual(len(events), 1)
        self.assertEqual(
            confirmation.confirm(frame(4, track(10), track(20), seconds=1.1)),
            [],
        )

        event = events[0]
        self.assertEqual(event["event_type"], "fight_review")
        self.assertEqual(event["confidence"], 0.84)
        self.assertEqual(event["tenant_id"], TENANT)
        self.assertEqual(event["site_id"], SITE)
        self.assertEqual(event["camera_id"], CAMERA)
        self.assertEqual(event["zone_id"], ZONE)
        self.assertEqual(event["dedupe_key"], f"fight_review:{event['event_id']}")
        self.assertEqual(event["evidence_refs"], [])
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        self.assertEqual(confirmation.metrics.confirmed_events, 1)

        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "track_id",
            "cluster_id",
            "motion_state",
            "aggressive",
            "ordinary",
            "hugging",
            "jostling",
            "person",
            "identity",
            "biometric",
            "face",
            "plate",
            "embedding",
            "pixel",
            "image",
            "injury",
            "violence",
            "safety conclusion",
            "alarm",
            "dispatch",
            "access_control",
            "autonomous",
        ):
            self.assertNotIn(prohibited, serialized)

    def test_single_track_and_non_aggressive_motion_never_confirm(self) -> None:
        for motion_state in ("ordinary", "hugging", "jostling", "unknown"):
            with self.subTest(motion_state=motion_state):
                confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
                for sequence, seconds in enumerate((0, 0.5, 1, 1.5), start=1):
                    tracks = (
                        track(10, motion_state),
                        track(20, motion_state),
                    )
                    self.assertEqual(
                        confirmation.confirm(frame(sequence, *tracks, seconds=seconds)),
                        [],
                    )
                self.assertEqual(confirmation.metrics.confirmed_events, 0)

        single = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        for sequence, seconds in enumerate((0, 0.5, 1, 1.5), start=1):
            self.assertEqual(
                single.confirm(frame(sequence, track(10), seconds=seconds)),
                [],
            )
        self.assertEqual(single.metrics.confirmed_events, 0)

    def test_missing_track_and_observation_gap_reset_cluster(self) -> None:
        confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, track(10), track(20), seconds=0))
        confirmation.confirm(frame(2, track(10), seconds=0.5))
        self.assertEqual(
            confirmation.confirm(frame(3, track(10), track(20), seconds=1)),
            [],
        )
        self.assertEqual(
            confirmation.confirm(frame(4, track(10), track(20), seconds=1.5)),
            [],
        )
        confirmation.confirm(frame(5, track(10), track(20), seconds=3))
        self.assertEqual(
            confirmation.confirm(frame(6, track(10), track(20), seconds=3.5)),
            [],
        )
        self.assertEqual(confirmation.metrics.confirmed_events, 0)

    def test_provenance_participant_and_motion_changes_reset_cluster(self) -> None:
        confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, track(10), track(20), seconds=0))
        confirmation.confirm(
            frame(
                2,
                track(10, model_version="synthetic-motion-2"),
                track(20, model_version="synthetic-motion-2"),
                seconds=0.5,
            )
        )
        self.assertEqual(
            confirmation.confirm(
                frame(
                    3,
                    track(10, model_version="synthetic-motion-2"),
                    track(20, model_version="synthetic-motion-2"),
                    seconds=1,
                )
            ),
            [],
        )
        confirmation.confirm(
            frame(
                4,
                track(10, model_version="synthetic-motion-2"),
                track(30, model_version="synthetic-motion-2"),
                seconds=1.5,
            )
        )
        confirmation.confirm(
            frame(
                5,
                track(10, model_version="synthetic-motion-2"),
                track(30, "hugging", model_version="synthetic-motion-2"),
                seconds=2,
            )
        )
        self.assertEqual(
            confirmation.confirm(
                frame(
                    6,
                    track(10, model_version="synthetic-motion-2"),
                    track(30, model_version="synthetic-motion-2"),
                    seconds=2.5,
                )
            ),
            [],
        )
        self.assertEqual(confirmation.metrics.confirmed_events, 0)

    def test_output_order_and_retry_are_deterministic(self) -> None:
        outputs: list[list[dict[str, object]]] = []
        for _ in range(2):
            confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
            for sequence, seconds in ((1, 0), (2, 0.5)):
                confirmation.confirm(
                    frame(
                        sequence,
                        track(30, cluster_id=9),
                        track(40, cluster_id=9),
                        track(10, cluster_id=2),
                        track(20, cluster_id=2),
                        seconds=seconds,
                    )
                )
            outputs.append(
                confirmation.confirm(
                    frame(
                        3,
                        track(30, cluster_id=9),
                        track(40, cluster_id=9),
                        track(10, cluster_id=2),
                        track(20, cluster_id=2),
                        seconds=1,
                    )
                )
            )
        self.assertEqual(outputs[0], outputs[1])
        self.assertEqual(len(outputs[0]), 2)

        expected = []
        for cluster_id, track_ids in ((2, (10, 20)), (9, (30, 40))):
            confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
            expected.append(
                confirm_at_boundary(
                    confirmation,
                    cluster_id=cluster_id,
                    track_ids=track_ids,
                )
            )
        self.assertEqual(
            [event["event_id"] for event in outputs[0]],
            [event["event_id"] for event in expected],
        )

    def test_replay_scope_duplicates_malformed_and_capacity_fail_atomically(self) -> None:
        confirmation = FightReviewConfirmation(
            TENANT,
            SITE,
            CAMERA,
            ZONE,
            max_clusters=1,
        )
        confirmation.confirm(frame(1, track(10), track(20)))
        before = confirmation.metrics
        invalid = (
            frame(1, track(10), track(20)),
            replace(frame(2, track(10), track(20)), camera_id=ZONE),
            replace(frame(2, track(10), track(20)), observed_at=START),
            frame(2, track(10, "running"), track(20)),
            frame(2, track(10, confidence=float("nan")), track(20)),
            frame(2, track(10, model_version="private details"), track(20)),
            frame(2, track(10), track(10)),
            frame(2, track(10, cluster_id=-1), track(20)),
            frame(
                2,
                track(10, cluster_id=1),
                track(20, cluster_id=1),
                track(30, cluster_id=2),
                track(40, cluster_id=2),
            ),
            replace(
                frame(2),
                tracks=tuple(
                    track(index, cluster_id=index // 2)
                    for index in range(MAX_TRACKS_PER_FRAME + 1)
                ),
            ),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    confirmation.confirm(value)
                self.assertEqual(confirmation.metrics, before)
        self.assertEqual(confirmation.confirm(frame(2, track(10), track(20))), [])

    def test_reset_allows_new_event_without_duplicate_emission(self) -> None:
        confirmation = FightReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        first = confirm_at_boundary(confirmation)
        self.assertEqual(
            confirmation.confirm(frame(4, track(10), track(20), seconds=1.1)),
            [],
        )
        confirmation.confirm(
            frame(5, track(10, "ordinary"), track(20, "ordinary"), seconds=1.2)
        )
        second = confirm_at_boundary(
            confirmation,
            start_sequence=6,
            start_seconds=1.3,
        )
        self.assertNotEqual(first["event_id"], second["event_id"])

    def test_configuration_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"max_clusters": 0},
            {"max_clusters": 257},
            {"emitted_retention_frames": 0},
            {"emitted_retention_frames": 31},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    FightReviewConfirmation(
                        TENANT,
                        SITE,
                        CAMERA,
                        ZONE,
                        **kwargs,
                    )


if __name__ == "__main__":
    unittest.main()
