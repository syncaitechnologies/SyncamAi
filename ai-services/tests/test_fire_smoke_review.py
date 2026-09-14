import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.fire_smoke_review import (  # noqa: E402
    MAX_CANDIDATES_PER_FRAME,
    FireSmokeCandidate,
    FireSmokeCandidateFrame,
    FireSmokeReviewConfirmation,
)


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
START = datetime(2026, 9, 14, 12, 0, tzinfo=timezone.utc)


def candidate(
    object_class: str = "fire",
    confidence: float = 0.9,
    *,
    left: float = 0.1,
    top: float = 0.4,
    model_version: str = "synthetic-detector-1",
) -> FireSmokeCandidate:
    return FireSmokeCandidate(
        object_class,
        confidence,
        left,
        top,
        left + 0.2,
        top + 0.2,
        model_version,
    )


def frame(
    sequence: int,
    *values: FireSmokeCandidate,
    seconds: float | None = None,
) -> FireSmokeCandidateFrame:
    elapsed = sequence / 10 if seconds is None else seconds
    return FireSmokeCandidateFrame(
        TENANT,
        SITE,
        CAMERA,
        ZONE,
        sequence,
        START + timedelta(seconds=elapsed),
        tuple(values),
    )


class FireSmokeReviewConfirmationTest(unittest.TestCase):
    def test_fire_requires_three_frames_and_emits_one_pending_review(self) -> None:
        confirmation = FireSmokeReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        self.assertEqual(confirmation.confirm(frame(1, candidate(confidence=0.95))), [])
        self.assertEqual(
            confirmation.confirm(frame(2, candidate(confidence=0.9, left=0.11))),
            [],
        )
        events = confirmation.confirm(
            frame(3, candidate(confidence=0.84, left=0.12))
        )
        self.assertEqual(len(events), 1)
        self.assertEqual(
            confirmation.confirm(frame(4, candidate(left=0.13))),
            [],
        )
        event = events[0]
        self.assertEqual(event["event_type"], "fire_review")
        self.assertEqual(event["confidence"], 0.84)
        self.assertEqual(event["tenant_id"], TENANT)
        self.assertEqual(event["site_id"], SITE)
        self.assertEqual(event["camera_id"], CAMERA)
        self.assertEqual(event["zone_id"], ZONE)
        self.assertEqual(event["dedupe_key"], f"fire_review:{event['event_id']}")
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        self.assertEqual(event["evidence_refs"], [])
        self.assertEqual(confirmation.metrics.confirmed_events, 1)

        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "candidate_id",
            "bounding",
            "coordinate",
            "pixel",
            "image",
            "identity",
            "biometric",
            "plate",
            "embedding",
            "alarm",
            "evacuation",
            "dispatch",
            "access_control",
            "autonomous",
        ):
            self.assertNotIn(prohibited, serialized)

    def test_smoke_requires_five_consistently_rising_frames(self) -> None:
        confirmation = FireSmokeReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        for sequence, top in enumerate((0.50, 0.48, 0.46, 0.44), start=1):
            self.assertEqual(
                confirmation.confirm(frame(sequence, candidate("smoke", top=top))),
                [],
            )
        events = confirmation.confirm(frame(5, candidate("smoke", top=0.42)))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "smoke_review")
        self.assertEqual(confirmation.confirm(frame(6, candidate("smoke", top=0.42))), [])

    def test_non_rising_smoke_gap_and_provenance_change_reset_confirmation(self) -> None:
        confirmation = FireSmokeReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, candidate("smoke", top=0.50)))
        confirmation.confirm(frame(2, candidate("smoke", top=0.49)))
        self.assertEqual(
            confirmation.confirm(frame(3, candidate("smoke", top=0.50))),
            [],
        )
        self.assertEqual(
            confirmation.confirm(
                frame(4, candidate("smoke", top=0.48), seconds=2.5)
            ),
            [],
        )
        self.assertEqual(
            confirmation.confirm(
                frame(
                    5,
                    candidate("smoke", top=0.46, model_version="synthetic-detector-2"),
                    seconds=2.6,
                )
            ),
            [],
        )
        for sequence, top in ((6, 0.44), (7, 0.42), (8, 0.40), (9, 0.38)):
            events = confirmation.confirm(
                frame(
                    sequence,
                    candidate("smoke", top=top, model_version="synthetic-detector-2"),
                    seconds=2.1 + sequence / 10,
                )
            )
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "smoke_review")

    def test_spatial_mismatch_restarts_fire_confirmation(self) -> None:
        confirmation = FireSmokeReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
        confirmation.confirm(frame(1, candidate(left=0.1)))
        self.assertEqual(confirmation.confirm(frame(2, candidate(left=0.7))), [])
        self.assertEqual(confirmation.confirm(frame(3, candidate(left=0.69))), [])
        events = confirmation.confirm(frame(4, candidate(left=0.68)))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "fire_review")

    def test_replay_scope_malformed_input_and_capacity_fail_atomically(self) -> None:
        confirmation = FireSmokeReviewConfirmation(
            TENANT,
            SITE,
            CAMERA,
            ZONE,
            max_candidates=1,
        )
        confirmation.confirm(frame(1, candidate()))
        before = confirmation.metrics
        invalid = (
            frame(1, candidate()),
            replace(frame(2, candidate()), camera_id=ZONE),
            frame(2, replace(candidate(), object_class="steam")),
            frame(2, replace(candidate(), confidence=float("nan"))),
            frame(2, replace(candidate(), left=0.8, right=0.2)),
            frame(2, replace(candidate(), model_version="private details")),
            frame(2, candidate(), candidate("smoke", left=0.7, top=0.6)),
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
        self.assertEqual(
            confirmation.confirm(frame(2, candidate(left=0.11))),
            [],
        )

    def test_retry_output_is_deterministic(self) -> None:
        outputs: list[dict[str, object]] = []
        for _ in range(2):
            confirmation = FireSmokeReviewConfirmation(TENANT, SITE, CAMERA, ZONE)
            confirmation.confirm(frame(1, candidate()))
            confirmation.confirm(frame(2, candidate(left=0.11)))
            outputs.append(confirmation.confirm(frame(3, candidate(left=0.12)))[0])
        self.assertEqual(outputs[0], outputs[1])

    def test_configuration_bounds_fail_closed(self) -> None:
        for kwargs in (
            {"minimum_iou": 0},
            {"minimum_iou": float("inf")},
            {"minimum_smoke_rise": 0},
            {"minimum_smoke_rise": -0.1},
            {"minimum_smoke_rise": 0.251},
            {"max_candidates": 0},
            {"emitted_retention_frames": 31},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    FireSmokeReviewConfirmation(
                        TENANT,
                        SITE,
                        CAMERA,
                        ZONE,
                        **kwargs,
                    )


if __name__ == "__main__":
    unittest.main()
