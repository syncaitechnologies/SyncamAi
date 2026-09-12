import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.weapon_review import (  # noqa: E402
    MAX_CANDIDATES_PER_FRAME,
    WeaponCandidate,
    WeaponCandidateFrame,
    WeaponReviewConfirmation,
)


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
START = datetime(2026, 9, 12, 12, 0, tzinfo=timezone.utc)


def candidate(
    object_class: str = "knife",
    confidence: float = 0.9,
    left: float = 0.1,
) -> WeaponCandidate:
    return WeaponCandidate(
        object_class,
        confidence,
        left,
        0.2,
        left + 0.2,
        0.5,
        "detector-candidate-1",
    )


def frame(sequence: int, *values: WeaponCandidate) -> WeaponCandidateFrame:
    return WeaponCandidateFrame(
        TENANT,
        SITE,
        CAMERA,
        ZONE,
        sequence,
        START + timedelta(milliseconds=100 * sequence),
        tuple(values),
    )


class WeaponReviewConfirmationTest(unittest.TestCase):
    def test_requires_three_consistent_frames_and_emits_one_review(self) -> None:
        confirmation = WeaponReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        self.assertEqual(confirmation.confirm(frame(1, candidate(confidence=0.95))), [])
        self.assertEqual(
            confirmation.confirm(frame(2, candidate(confidence=0.9, left=0.11))),
            [],
        )
        events = confirmation.confirm(frame(3, candidate(confidence=0.85, left=0.12)))
        self.assertEqual(len(events), 1)
        event = events[0]
        self.assertEqual(event["event_type"], "weapon_review")
        self.assertEqual(event["confidence"], 0.85)
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        self.assertEqual(confirmation.confirm(frame(4, candidate(left=0.13))), [])
        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "knife",
            "firearm",
            "candidate_id",
            "bounding",
            "coordinates",
            "identity",
            "automatic",
            "dispatch",
        ):
            self.assertNotIn(prohibited, serialized)

    def test_class_change_or_spatial_mismatch_restarts_confirmation(self) -> None:
        confirmation = WeaponReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, candidate("knife")))
        confirmation.confirm(frame(2, candidate("firearm")))
        self.assertEqual(
            confirmation.confirm(frame(3, candidate("firearm", left=0.7))), []
        )
        self.assertEqual(
            confirmation.confirm(frame(4, candidate("firearm", left=0.7))), []
        )
        events = confirmation.confirm(frame(5, candidate("firearm", left=0.71)))
        self.assertEqual(len(events), 1)

    def test_replay_malformed_scope_and_capacity_fail_atomically(self) -> None:
        confirmation = WeaponReviewConfirmation(
            TENANT, SITE, CAMERA, ZONE, max_candidates=1
        )
        confirmation.confirm(frame(1, candidate()))
        before = confirmation.metrics
        invalid = (
            frame(1, candidate()),
            replace(frame(2, candidate()), camera_id=ZONE),
            frame(2, replace(candidate(), object_class="tool")),
            frame(2, replace(candidate(), confidence=float("nan"))),
            frame(2, replace(candidate(), left=0.8, right=0.2)),
            frame(2, replace(candidate(), model_version="private details")),
            frame(2, candidate(), candidate("firearm", left=0.7)),
            replace(
                frame(2),
                candidates=tuple(
                    candidate(left=(index % 4) / 10)
                    for index in range(MAX_CANDIDATES_PER_FRAME + 1)
                ),
            ),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    confirmation.confirm(value)
                self.assertEqual(confirmation.metrics, before)
        self.assertEqual(confirmation.confirm(frame(2, candidate(left=0.11))), [])

    def test_gap_resets_unconfirmed_streak_and_retains_emitted_cooldown(self) -> None:
        confirmation = WeaponReviewConfirmation(
            TENANT, SITE, CAMERA, ZONE, emitted_retention_frames=3
        )
        confirmation.confirm(frame(1, candidate()))
        late = replace(
            frame(2, candidate()),
            observed_at=START + timedelta(seconds=2),
        )
        self.assertEqual(confirmation.confirm(late), [])
        self.assertEqual(
            confirmation.confirm(
                replace(frame(3, candidate()), observed_at=START + timedelta(seconds=2.1))
            ),
            [],
        )
        events = confirmation.confirm(
            replace(frame(4, candidate()), observed_at=START + timedelta(seconds=2.2))
        )
        self.assertEqual(len(events), 1)
        self.assertEqual(confirmation.confirm(frame(30)), [])
        self.assertGreaterEqual(confirmation.metrics.expired_candidates, 1)

    def test_configuration_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"minimum_iou": 0},
            {"minimum_iou": float("inf")},
            {"max_candidates": 0},
            {"emitted_retention_frames": 31},
        ):
            with self.assertRaises(ValueError):
                WeaponReviewConfirmation(TENANT, SITE, CAMERA, ZONE, **kwargs)


if __name__ == "__main__":
    unittest.main()
