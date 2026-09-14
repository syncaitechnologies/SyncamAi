import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.ppe_review import (  # noqa: E402
    MAX_TRACKS_PER_FRAME,
    PPE_ITEMS,
    PPEItemObservation,
    PPEReviewConfirmation,
    PPETrackFrame,
    PPETrackObservation,
)


TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"
START = datetime(2026, 9, 14, 12, 0, tzinfo=timezone.utc)
REQUIRED = frozenset({"helmet", "vest"})


def item(name: str, state: str, confidence: float = 0.9) -> PPEItemObservation:
    return PPEItemObservation(name, state, confidence)


def track(
    track_id: int = 42,
    *,
    helmet: str = "absent",
    vest: str = "present",
    helmet_confidence: float = 0.9,
    model_version: str = "synthetic-ppe-metadata-1",
) -> PPETrackObservation:
    return PPETrackObservation(
        track_id,
        model_version,
        (
            item("helmet", helmet, helmet_confidence),
            item("vest", vest, 0.95),
        ),
    )


def frame(
    sequence: int,
    *tracks: PPETrackObservation,
    seconds: float | None = None,
) -> PPETrackFrame:
    elapsed = sequence / 10 if seconds is None else seconds
    return PPETrackFrame(
        TENANT,
        SITE,
        CAMERA,
        ZONE,
        sequence,
        START + timedelta(seconds=elapsed),
        tuple(tracks),
    )


class PPEReviewConfirmationTest(unittest.TestCase):
    def test_exact_six_item_matrix_vocabulary_is_supported(self) -> None:
        self.assertEqual(
            PPE_ITEMS,
            frozenset({"helmet", "vest", "mask", "gloves", "glasses", "boots"}),
        )
        for required_item in PPE_ITEMS:
            with self.subTest(required_item=required_item):
                confirmation = PPEReviewConfirmation(
                    TENANT, SITE, CAMERA, ZONE, frozenset({required_item})
                )
                observation = replace(
                    track(), items=(item(required_item, "absent"),)
                )
                self.assertEqual(confirmation.confirm(frame(1, observation)), [])
                self.assertEqual(confirmation.confirm(frame(2, observation)), [])
                event = confirmation.confirm(frame(3, observation))[0]
                self.assertEqual(event["event_type"], "ppe_review")
                self.assertNotIn(required_item, json.dumps(event).lower())

    def test_requires_three_consistent_frames_and_emits_only_pending_review(self) -> None:
        confirmation = PPEReviewConfirmation(
            TENANT, SITE, CAMERA, ZONE, REQUIRED
        )
        self.assertEqual(confirmation.confirm(frame(1, track(helmet_confidence=0.95))), [])
        self.assertEqual(confirmation.confirm(frame(2, track(helmet_confidence=0.9))), [])
        events = confirmation.confirm(frame(3, track(helmet_confidence=0.84)))
        self.assertEqual(len(events), 1)
        self.assertEqual(confirmation.confirm(frame(4, track())), [])

        event = events[0]
        self.assertEqual(event["event_type"], "ppe_review")
        self.assertEqual(event["confidence"], 0.84)
        self.assertEqual(event["tenant_id"], TENANT)
        self.assertEqual(event["site_id"], SITE)
        self.assertEqual(event["camera_id"], CAMERA)
        self.assertEqual(event["zone_id"], ZONE)
        self.assertEqual(event["dedupe_key"], f"ppe_review:{event['event_id']}")
        self.assertEqual(event["evidence_refs"], [])
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        self.assertEqual(confirmation.metrics.confirmed_events, 1)

        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "track_id",
            "helmet",
            "vest",
            "mask",
            "gloves",
            "glasses",
            "boots",
            "missing",
            "violation",
            "worker",
            "person",
            "identity",
            "biometric",
            "face",
            "plate",
            "embedding",
            "image",
            "alarm",
            "dispatch",
            "access_control",
            "autonomous",
            "compliant",
            "unsafe",
        ):
            self.assertNotIn(prohibited, serialized)

    def test_unknown_incomplete_or_fully_present_metadata_does_not_confirm(self) -> None:
        confirmation = PPEReviewConfirmation(TENANT, SITE, CAMERA, ZONE, REQUIRED)
        self.assertEqual(confirmation.confirm(frame(1, track())), [])
        self.assertEqual(
            confirmation.confirm(frame(2, track(helmet="unknown"))), []
        )
        incomplete = replace(track(), items=(item("helmet", "absent"),))
        self.assertEqual(confirmation.confirm(frame(3, incomplete)), [])
        self.assertEqual(
            confirmation.confirm(frame(4, track(helmet="present"))), []
        )
        self.assertEqual(confirmation.confirm(frame(5, track())), [])
        self.assertEqual(confirmation.confirm(frame(6, track())), [])
        self.assertEqual(len(confirmation.confirm(frame(7, track()))), 1)

    def test_missing_set_model_change_and_time_gap_restart_confirmation(self) -> None:
        confirmation = PPEReviewConfirmation(TENANT, SITE, CAMERA, ZONE, REQUIRED)
        confirmation.confirm(frame(1, track()))
        both_absent = track(vest="absent")
        self.assertEqual(confirmation.confirm(frame(2, both_absent)), [])
        changed_model = track(vest="absent", model_version="synthetic-ppe-metadata-2")
        self.assertEqual(confirmation.confirm(frame(3, changed_model)), [])
        late = frame(4, changed_model, seconds=2.5)
        self.assertEqual(confirmation.confirm(late), [])
        self.assertEqual(
            confirmation.confirm(frame(5, changed_model, seconds=2.6)), []
        )
        events = confirmation.confirm(frame(6, changed_model, seconds=2.7))
        self.assertEqual(len(events), 1)

    def test_ordering_and_retry_output_are_deterministic(self) -> None:
        outputs: list[list[dict[str, object]]] = []
        for _ in range(2):
            confirmation = PPEReviewConfirmation(TENANT, SITE, CAMERA, ZONE, REQUIRED)
            for sequence in (1, 2):
                self.assertEqual(
                    confirmation.confirm(frame(sequence, track(9), track(2))), []
                )
            outputs.append(confirmation.confirm(frame(3, track(9), track(2))))
        self.assertEqual(outputs[0], outputs[1])
        self.assertEqual(len(outputs[0]), 2)
        self.assertNotEqual(outputs[0][0]["event_id"], outputs[0][1]["event_id"])

    def test_replay_malformed_scope_duplicates_and_capacity_fail_atomically(self) -> None:
        confirmation = PPEReviewConfirmation(
            TENANT, SITE, CAMERA, ZONE, REQUIRED, max_tracks=1
        )
        confirmation.confirm(frame(1, track()))
        before = confirmation.metrics
        invalid = (
            frame(1, track()),
            replace(frame(2, track()), camera_id=ZONE),
            frame(2, replace(track(), track_id=True)),
            frame(2, replace(track(), model_version="private details")),
            frame(2, replace(track(), items=(item("helmet", "maybe"),))),
            frame(2, replace(track(), items=(item("helmet", "absent", float("nan")),))),
            frame(2, replace(track(), items=(item("badge", "absent"),))),
            frame(2, replace(track(), items=(item("helmet", "absent"), item("helmet", "present")))),
            frame(2, track(), track()),
            frame(2, track(), track(43)),
            replace(
                frame(2),
                tracks=tuple(track(index) for index in range(MAX_TRACKS_PER_FRAME + 1)),
            ),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    confirmation.confirm(value)
                self.assertEqual(confirmation.metrics, before)
        self.assertEqual(confirmation.confirm(frame(2, track())), [])
        self.assertEqual(len(confirmation.confirm(frame(3, track()))), 1)

    def test_explicit_present_state_and_bounded_cooldown_allow_reconfirmation(self) -> None:
        confirmation = PPEReviewConfirmation(
            TENANT,
            SITE,
            CAMERA,
            ZONE,
            REQUIRED,
            emitted_retention_frames=2,
        )
        for sequence in (1, 2):
            confirmation.confirm(frame(sequence, track()))
        self.assertEqual(len(confirmation.confirm(frame(3, track()))), 1)
        self.assertEqual(confirmation.confirm(frame(4)), [])
        self.assertEqual(confirmation.confirm(frame(5)), [])
        self.assertEqual(confirmation.confirm(frame(6)), [])
        for sequence in (7, 8):
            self.assertEqual(confirmation.confirm(frame(sequence, track())), [])
        self.assertEqual(len(confirmation.confirm(frame(9, track()))), 1)
        self.assertGreaterEqual(confirmation.metrics.expired_tracks, 1)

    def test_configuration_bounds_fail_closed(self) -> None:
        for required, kwargs in (
            (frozenset(), {}),
            (frozenset({"hard_hat"}), {}),
            ({"helmet"}, {}),
            (REQUIRED, {"max_tracks": 0}),
            (REQUIRED, {"max_tracks": 257}),
            (REQUIRED, {"emitted_retention_frames": 0}),
            (REQUIRED, {"emitted_retention_frames": 31}),
        ):
            with self.subTest(required=required, kwargs=kwargs):
                with self.assertRaises(ValueError):
                    PPEReviewConfirmation(
                        TENANT,
                        SITE,
                        CAMERA,
                        ZONE,
                        required,  # type: ignore[arg-type]
                        **kwargs,
                    )


if __name__ == "__main__":
    unittest.main()
