import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.abandoned_object import (  # noqa: E402
    ABANDONED_OBJECT_CLASSES,
    AbandonedObjectEngine,
    AbandonedObjectObservation,
    AbandonedObjectThresholds,
    EvaluationCase,
    PersonTrack,
    evaluate_cases,
)


BASE_TIME = datetime(2026, 8, 20, 6, 0, tzinfo=timezone.utc)
THRESHOLDS = AbandonedObjectThresholds(static_dwell_seconds=30, alert_dwell_seconds=60)


def observation(
    seconds: int,
    people: tuple[PersonTrack, ...] = (PersonTrack(8, 0.51, 0.50),),
    **changes: object,
) -> AbandonedObjectObservation:
    values: dict[str, object] = {
        "tenant_id": "11111111-1111-4111-8111-111111111111",
        "site_id": "22222222-2222-4222-8222-222222222222",
        "camera_id": "33333333-3333-4333-8333-333333333333",
        "zone_id": "44444444-4444-4444-8444-444444444444",
        "object_track_id": 42,
        "object_class": " bag ",
        "observed_at": BASE_TIME + timedelta(seconds=seconds),
        "center_x": 0.50,
        "center_y": 0.50,
        "confidence": 0.91,
        "model_version": " static-object-logic-1 ",
        "evidence_refs": (" evidence://object-42 ",),
        "person_tracks": people,
    }
    values.update(changes)
    return AbandonedObjectObservation(**values)  # type: ignore[arg-type]


class AbandonedObjectEngineTest(unittest.TestCase):
    def test_emits_one_deterministic_review_required_alert_after_owner_leaves(self) -> None:
        engine = AbandonedObjectEngine(THRESHOLDS)
        self.assertIsNone(engine.observe(observation(0)))
        self.assertIsNone(engine.observe(observation(30, ())))
        event = engine.observe(observation(60, ()))

        self.assertIsNotNone(event)
        assert event is not None
        self.assertEqual(event["event_type"], "abandoned_object_review")
        self.assertEqual(event["model_version"], "static-object-logic-1")
        self.assertEqual(event["evidence_refs"], ["evidence://object-42"])
        self.assertIs(event["requires_human_review"], True)
        self.assertEqual(event["review_state"], "pending")
        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in (
            "owner_track",
            "person_track",
            "identity",
            "theft",
            "plate",
            "reid",
            "center_x",
        ):
            self.assertNotIn(prohibited, serialized)
        self.assertIsNone(engine.observe(observation(90, ())))

    def test_owner_present_or_object_movement_suppresses_alert(self) -> None:
        engine = AbandonedObjectEngine(THRESHOLDS)
        self.assertIsNone(engine.observe(observation(0)))
        self.assertIsNone(engine.observe(observation(60)))

        engine = AbandonedObjectEngine(THRESHOLDS)
        self.assertIsNone(engine.observe(observation(0)))
        self.assertIsNone(engine.observe(observation(30, (), center_x=0.60)))
        self.assertIsNone(engine.observe(observation(60, ())))

        engine = AbandonedObjectEngine(THRESHOLDS)
        self.assertIsNone(engine.observe(observation(0)))
        self.assertIsNone(engine.observe(observation(20, (), center_x=0.515)))
        self.assertIsNone(engine.observe(observation(40, (), center_x=0.530)))
        self.assertIsNone(engine.observe(observation(80, ())))

    def test_owner_association_requires_static_dwell_and_timestamp_order(self) -> None:
        engine = AbandonedObjectEngine(THRESHOLDS)
        self.assertIsNone(engine.observe(observation(0, ())))
        self.assertIsNone(
            engine.observe(observation(31, (PersonTrack(8, 0.51, 0.50),)))
        )
        self.assertIsNone(engine.observe(observation(60, ())))

        engine = AbandonedObjectEngine(THRESHOLDS)
        engine.observe(observation(10))
        with self.assertRaises(ValueError):
            engine.observe(observation(9, center_x=0.60))

    def test_input_and_thresholds_are_bounded(self) -> None:
        invalid = (
            replace(observation(0), tenant_id="bad"),
            replace(observation(0), object_track_id=-1),
            replace(observation(0), object_track_id=True),
            replace(observation(0), object_class="wheelbarrow"),
            replace(observation(0), observed_at=datetime(2026, 8, 20, 6, 0)),
            replace(observation(0), center_x=1.1),
            replace(observation(0), confidence=float("nan")),
            replace(observation(0), evidence_refs=(" ",)),
            replace(
                observation(0),
                person_tracks=(PersonTrack(8, 0.5, 0.5), PersonTrack(8, 0.6, 0.6)),
            ),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    AbandonedObjectEngine(THRESHOLDS).observe(value)
        for object_class in ABANDONED_OBJECT_CLASSES:
            self.assertIsNone(
                AbandonedObjectEngine(THRESHOLDS).observe(
                    replace(observation(0), object_class=object_class)
                )
            )
        for invalid_thresholds in (
            {"static_dwell_seconds": 29},
            {"static_dwell_seconds": 61},
            {"static_dwell_seconds": 60, "alert_dwell_seconds": 59},
            {"movement_tolerance": -0.1},
        ):
            with self.subTest(thresholds=invalid_thresholds):
                with self.assertRaises(ValueError):
                    AbandonedObjectThresholds(**invalid_thresholds)

    def test_evaluation_harness_reports_deterministic_temporal_results(self) -> None:
        cases = (
            EvaluationCase(
                "owner leaves after static dwell",
                (observation(0), observation(30, ()), observation(60, ())),
                True,
            ),
            EvaluationCase(
                "owner remains beside object", (observation(0), observation(60)), False
            ),
            EvaluationCase(
                "object moves before T2",
                (observation(0), observation(30, (), center_x=0.60), observation(60, ())),
                False,
            ),
        )
        summary = evaluate_cases(cases, THRESHOLDS)
        self.assertEqual(summary.cases, 3)
        self.assertEqual((summary.true_positives, summary.true_negatives), (1, 2))
        self.assertEqual((summary.false_positives, summary.false_negatives), (0, 0))
        self.assertEqual(summary.detection_rate, 1.0)


if __name__ == "__main__":
    unittest.main()
