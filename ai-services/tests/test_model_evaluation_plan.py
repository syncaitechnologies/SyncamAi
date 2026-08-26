import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_evaluation_plan import (  # noqa: E402
    CANONICAL_EVALUATION_PLAN_REFERENCE,
    MODEL_EVALUATION_PLANS,
    PLANNED_EVALUATION_STATUS,
    REQUIRED_RELEASE_GATES,
    ModelEvaluationPlan,
    validate_model_evaluation_plans,
)
from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402


class ModelEvaluationPlanTest(unittest.TestCase):
    def test_every_registry_capability_has_one_planned_evaluation_plan(self) -> None:
        self.assertEqual(
            {plan.capability_identifier for plan in MODEL_EVALUATION_PLANS},
            {entry.identifier for entry in MODEL_REGISTRY},
        )
        self.assertEqual(len(MODEL_EVALUATION_PLANS), len(MODEL_REGISTRY))
        for plan in MODEL_EVALUATION_PLANS:
            self.assertEqual(plan.status, PLANNED_EVALUATION_STATUS)
            self.assertEqual(plan.evaluation_reference, CANONICAL_EVALUATION_PLAN_REFERENCE)
            self.assertEqual(plan.required_release_gates, REQUIRED_RELEASE_GATES)
            self.assertTrue(plan.required_evaluation_categories)

    def test_validation_rejects_missing_capability_and_completed_claim(self) -> None:
        with self.assertRaises(ValueError):
            validate_model_evaluation_plans(MODEL_EVALUATION_PLANS[1:])
        first = MODEL_EVALUATION_PLANS[0]
        completed = ModelEvaluationPlan(
            **{**first.__dict__, "status": "completed"}
        )
        with self.assertRaises(ValueError):
            validate_model_evaluation_plans((completed,) + MODEL_EVALUATION_PLANS[1:])


if __name__ == "__main__":
    unittest.main()
