import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.fall_review import (  # noqa: E402
    MAX_TRACKS_PER_FRAME,
    FallReviewConfirmation,
    FallTrackFrame,
    FallTrackObservation,
)


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
START = datetime(2026, 9, 14, 12, 0, tzinfo=timezone.utc)


def track(
    posture: str,
    motion_state: str,
    *,
    track_id: int = 42,
    confidence: float = 0.9,
    model_version: str = "synthetic-source-1",
) -> FallTrackObservation:
    return FallTrackObservation(
        track_id,
        posture,
        motion_state,
        confidence,
        model_version,
    )


def frame(
    sequence: int,
    *tracks: FallTrackObservation,
    seconds: float | None = None,
) -> FallTrackFrame:
    elapsed = sequence / 10 if seconds is None else seconds
    return FallTrackFrame(
        TENANT,
        SITE,
        CAMERA,
        ZONE,
        sequence,
        START + timedelta(seconds=elapsed),
        tuple(tracks),
    )


def confirm_at_boundary(
    confirmation: FallReviewConfirmation,
    *,
    track_id: int = 42,
    model_version: str = "synthetic-source-1",
) -> dict[str, object]:
    confirmation.confirm(
        frame(
            1,
            track(
                "upright",
                "stable",
                track_id=track_id,
                confidence=0.96,
                model_version=model_version,
            ),
            seconds=0,
        )
    )
    confirmation.confirm(
        frame(
            2,
            track(
                "transitioning",
                "downward",
                track_id=track_id,
                confidence=0.9,
                model_version=model_version,
            ),
            seconds=0.1,
        )
    )
    confirmation.confirm(
        frame(
            3,
            track(
                "transitioning",
                "downward",
                track_id=track_id,
                confidence=0.86,
                model_version=model_version,
            ),
            seconds=0.8,
        )
    )
    confirmation.confirm(
        frame(
            4,
            track(
                "lying",
                "stable",
                track_id=track_id,
                confidence=0.84,
                model_version=model_version,
            ),
            seconds=1.3,
        )
    )
    return confirmation.confirm(
        frame(
            5,
            track(
                "lying",
                "stable",
                track_id=track_id,
                confidence=0.82,
                model_version=model_version,
            ),
            seconds=1.6,
        )
    )[0]


class FallReviewConfirmationTest(unittest.TestCase):
    def test_requires_ordered_transition_for_exactly_one_point_five_seconds(self) -> None:
        confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(
            frame(1, track("upright", "stable", confidence=0.96), seconds=0)
        )
        confirmation.confirm(
            frame(
                2,
                track("transitioning", "downward", confidence=0.9),
                seconds=0.1,
            )
        )
        confirmation.confirm(
            frame(
                3,
                track("transitioning", "downward", confidence=0.86),
                seconds=0.8,
            )
        )
        self.assertEqual(
            confirmation.confirm(frame(4, track("lying", "stable", confidence=0.84), seconds=1.3)),
            [],
        )
        events = confirmation.confirm(
            frame(5, track("lying", "stable", confidence=0.82), seconds=1.6)
        )
        self.assertEqual(len(events), 1)
        self.assertEqual(
            confirmation.confirm(frame(6, track("lying", "stable"), seconds=1.7)),
            [],
        )

        event = events[0]
        self.assertEqual(event["event_type"], "fall_review")
        self.assertEqual(event["confidence"], 0.82)
        self.assertEqual(event["tenant_id"], TENANT)
        self.assertEqual(event["site_id"], SITE)
        self.assertEqual(event["camera_id"], CAMERA)
        self.assertEqual(event["zone_id"], ZONE)
        self.assertEqual(event["dedupe_key"], f"fall_review:{event['event_id']}")
        self.assertEqual(event["evidence_refs"], [])
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        self.assertEqual(confirmation.metrics.confirmed_events, 1)

        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "track_id",
            "posture",
            "motion",
            "upright",
            "lying",
            "person",
            "identity",
            "biometric",
            "face",
            "plate",
            "embedding",
            "pixel",
            "image",
            "medical",
            "injury",
            "emergency",
            "alarm",
            "dispatch",
            "access_control",
            "autonomous",
        ):
            self.assertNotIn(prohibited, serialized)

    def test_sitting_unknown_ambiguous_and_unordered_lying_never_confirm(self) -> None:
        confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        observations = (
            track("sitting", "stable"),
            track("lying", "stable"),
            track("unknown", "unknown"),
            track("upright", "ambiguous"),
            track("transitioning", "downward"),
            track("lying", "stable"),
        )
        for sequence, observation in enumerate(observations, start=1):
            self.assertEqual(
                confirmation.confirm(frame(sequence, observation)),
                [],
            )
        self.assertEqual(confirmation.metrics.confirmed_events, 0)

    def test_missing_observation_and_tracking_gap_reset_transition(self) -> None:
        confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, track("upright", "stable"), seconds=0))
        confirmation.confirm(frame(2, track("transitioning", "downward"), seconds=0.1))
        self.assertEqual(confirmation.confirm(frame(3, seconds=0.2)), [])
        self.assertEqual(
            confirmation.confirm(frame(4, track("lying", "stable"), seconds=1.6)),
            [],
        )
        confirmation.confirm(frame(5, track("upright", "stable"), seconds=1.7))
        confirmation.confirm(frame(6, track("transitioning", "downward"), seconds=1.8))
        self.assertEqual(
            confirmation.confirm(frame(7, track("lying", "stable"), seconds=3.0)),
            [],
        )
        self.assertEqual(confirmation.metrics.confirmed_events, 0)

    def test_provenance_change_and_reversed_motion_reset_transition(self) -> None:
        confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, track("upright", "stable"), seconds=0))
        confirmation.confirm(frame(2, track("transitioning", "downward"), seconds=0.1))
        self.assertEqual(
            confirmation.confirm(
                frame(
                    3,
                    track(
                        "transitioning",
                        "downward",
                        model_version="synthetic-source-2",
                    ),
                    seconds=0.8,
                )
            ),
            [],
        )
        self.assertEqual(
            confirmation.confirm(
                frame(
                    4,
                    track(
                        "lying",
                        "stable",
                        model_version="synthetic-source-2",
                    ),
                    seconds=1.7,
                )
            ),
            [],
        )
        confirmation.confirm(frame(5, track("upright", "stable"), seconds=1.8))
        confirmation.confirm(frame(6, track("transitioning", "downward"), seconds=1.9))
        self.assertEqual(
            confirmation.confirm(frame(7, track("transitioning", "stable"), seconds=2.5)),
            [],
        )
        self.assertEqual(
            confirmation.confirm(frame(8, track("lying", "stable"), seconds=3.5)),
            [],
        )

    def test_output_order_and_retry_are_deterministic(self) -> None:
        outputs: list[list[dict[str, object]]] = []
        for _ in range(2):
            confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
            confirmation.confirm(
                frame(
                    1,
                    track("upright", "stable", track_id=9),
                    track("upright", "stable", track_id=2),
                    seconds=0,
                )
            )
            confirmation.confirm(
                frame(
                    2,
                    track("transitioning", "downward", track_id=9),
                    track("transitioning", "downward", track_id=2),
                    seconds=0.1,
                )
            )
            confirmation.confirm(
                frame(
                    3,
                    track("transitioning", "downward", track_id=9),
                    track("transitioning", "downward", track_id=2),
                    seconds=0.8,
                )
            )
            confirmation.confirm(
                frame(
                    4,
                    track("lying", "stable", track_id=9),
                    track("lying", "stable", track_id=2),
                    seconds=1.3,
                )
            )
            outputs.append(
                confirmation.confirm(
                    frame(
                        5,
                        track("lying", "stable", track_id=9),
                        track("lying", "stable", track_id=2),
                        seconds=1.6,
                    )
                )
            )
        self.assertEqual(outputs[0], outputs[1])
        self.assertEqual(len(outputs[0]), 2)
        expected = []
        for track_id in (2, 9):
            confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
            expected.append(confirm_at_boundary(confirmation, track_id=track_id))
        self.assertEqual(
            [event["event_id"] for event in outputs[0]],
            [event["event_id"] for event in expected],
        )

    def test_replay_scope_duplicates_malformed_and_capacity_fail_atomically(self) -> None:
        confirmation = FallReviewConfirmation(
            TENANT,
            SITE,
            CAMERA,
            ZONE,
            max_tracks=1,
        )
        confirmation.confirm(frame(1, track("upright", "stable")))
        before = confirmation.metrics
        invalid = (
            frame(1, track("upright", "stable")),
            replace(frame(2, track("upright", "stable")), camera_id=ZONE),
            replace(frame(2, track("upright", "stable")), observed_at=START),
            frame(2, track("crouching", "stable")),
            frame(2, track("upright", "rising")),
            frame(2, track("upright", "stable", confidence=float("nan"))),
            frame(2, track("upright", "stable", model_version="private details")),
            frame(2, track("upright", "stable"), track("upright", "stable")),
            frame(
                2,
                track("upright", "stable", track_id=1),
                track("upright", "stable", track_id=2),
            ),
            replace(
                frame(2),
                tracks=tuple(
                    track("upright", "stable", track_id=index)
                    for index in range(MAX_TRACKS_PER_FRAME + 1)
                ),
            ),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    confirmation.confirm(value)
                self.assertEqual(confirmation.metrics, before)
        self.assertEqual(
            confirmation.confirm(frame(2, track("transitioning", "downward"))),
            [],
        )

    def test_recovery_allows_a_new_event_only_after_a_new_transition(self) -> None:
        confirmation = FallReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        first = confirm_at_boundary(confirmation)
        self.assertEqual(
            confirmation.confirm(frame(6, track("lying", "stable"), seconds=1.7)),
            [],
        )
        confirmation.confirm(frame(7, track("upright", "stable"), seconds=1.8))
        confirmation.confirm(frame(8, track("transitioning", "downward"), seconds=1.9))
        confirmation.confirm(frame(9, track("transitioning", "downward"), seconds=2.6))
        confirmation.confirm(frame(10, track("lying", "stable"), seconds=3.0))
        second = confirmation.confirm(
            frame(11, track("lying", "stable"), seconds=3.4)
        )[0]
        self.assertNotEqual(first["event_id"], second["event_id"])

    def test_configuration_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"max_tracks": 0},
            {"max_tracks": 257},
            {"emitted_retention_frames": 0},
            {"emitted_retention_frames": 31},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    FallReviewConfirmation(
                        TENANT,
                        SITE,
                        CAMERA,
                        ZONE,
                        **kwargs,
                    )


if __name__ == "__main__":
    unittest.main()
