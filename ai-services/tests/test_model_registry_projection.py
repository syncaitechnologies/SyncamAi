import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_evaluation_plan import MODEL_EVALUATION_PLANS  # noqa: E402
from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402
from syncam_ai.model_registry_projection import (  # noqa: E402
    EXTERNAL_MODEL_PROMOTION_STATUS,
    MODEL_REGISTRY_PROJECTION,
    PLANNED_EVALUATION_STATUS,
    PLANNED_REGISTRY_PROJECTION_STATUS,
    build_model_registry_projection,
)
from syncam_ai.model_release_boundary import (  # noqa: E402
    MODEL_RELEASE_BOUNDARIES,
    ModelReleaseBoundary,
)


class ModelRegistryProjectionTest(unittest.TestCase):
    def test_projection_preserves_every_planned_and_blocked_capability(self) -> None:
        self.assertEqual(
            [entry.capability_identifier for entry in MODEL_REGISTRY_PROJECTION],
            [entry.identifier for entry in MODEL_REGISTRY],
        )
        self.assertEqual(len(MODEL_REGISTRY_PROJECTION), len(MODEL_REGISTRY))
        for entry in MODEL_REGISTRY_PROJECTION:
            self.assertEqual(entry.status, PLANNED_REGISTRY_PROJECTION_STATUS)
            self.assertEqual(entry.evaluation_status, PLANNED_EVALUATION_STATUS)
            self.assertEqual(
                entry.external_model_promotion_status,
                EXTERNAL_MODEL_PROMOTION_STATUS,
            )
            self.assertTrue(entry.required_evaluation_categories)

    def test_projection_rejects_incomplete_or_permissive_inputs(self) -> None:
        with self.assertRaises(ValueError):
            build_model_registry_projection(plans=MODEL_EVALUATION_PLANS[:-1])

        first = MODEL_RELEASE_BOUNDARIES[0]
        permissive = ModelReleaseBoundary(
            **{
                **first.__dict__,
                "external_model_promotion_status": "eligible",
            }
        )
        with self.assertRaises(ValueError):
            build_model_registry_projection(
                boundaries=(permissive,) + MODEL_RELEASE_BOUNDARIES[1:]
            )


if __name__ == "__main__":
    unittest.main()
