import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_registry import (  # noqa: E402
    CAPABILITY_COUNT,
    CANONICAL_EVALUATION_REFERENCE,
    MODULE_COUNT,
    MODEL_REGISTRY,
    PLANNED_EVALUATION_STATUS,
    ModelRegistryEntry,
    validate_model_registry,
)


class ModelRegistryTest(unittest.TestCase):
    def test_registry_matches_canonical_module_and_bonus_count(self) -> None:
        self.assertEqual(len(MODEL_REGISTRY), CAPABILITY_COUNT)
        self.assertEqual(sum(not entry.bonus_capability for entry in MODEL_REGISTRY), MODULE_COUNT)
        self.assertEqual([entry.identifier for entry in MODEL_REGISTRY if entry.bonus_capability], ["camera_health"])

    def test_registry_is_metadata_only_and_evaluations_are_planned(self) -> None:
        for entry in MODEL_REGISTRY:
            self.assertEqual(entry.evaluation_reference, CANONICAL_EVALUATION_REFERENCE)
            self.assertEqual(entry.evaluation_status, PLANNED_EVALUATION_STATUS)
            self.assertTrue(entry.owner)
            self.assertTrue(entry.hardware_tier)

    def test_registry_rejects_duplicate_identifiers_and_published_claims(self) -> None:
        duplicate = MODEL_REGISTRY[0]
        with self.assertRaises(ValueError):
            validate_model_registry((duplicate,) * CAPABILITY_COUNT)
        published = ModelRegistryEntry(
            **{**duplicate.__dict__, "evaluation_status": "published"}
        )
        with self.assertRaises(ValueError):
            validate_model_registry((published,) + MODEL_REGISTRY[1:])


if __name__ == "__main__":
    unittest.main()

