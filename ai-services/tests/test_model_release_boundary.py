import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_release_boundary import (  # noqa: E402
    ALLOWED_MODEL_LICENSES,
    EXTERNAL_MODEL_PROMOTION_STATUS,
    LEGAL_GATE_REFERENCE,
    MODEL_RELEASE_BOUNDARIES,
    REQUIRED_RELEASE_METADATA,
    ModelReleaseBoundary,
    validate_model_release_boundaries,
)
from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402


class ModelReleaseBoundaryTest(unittest.TestCase):
    def test_every_registry_capability_is_blocked_pending_complete_evidence(self) -> None:
        self.assertEqual(
            {boundary.capability_identifier for boundary in MODEL_RELEASE_BOUNDARIES},
            {entry.identifier for entry in MODEL_REGISTRY},
        )
        for boundary in MODEL_RELEASE_BOUNDARIES:
            self.assertEqual(boundary.legal_gate_reference, LEGAL_GATE_REFERENCE)
            self.assertEqual(boundary.allowed_model_licenses, ALLOWED_MODEL_LICENSES)
            self.assertEqual(boundary.required_release_metadata, REQUIRED_RELEASE_METADATA)
            self.assertEqual(
                boundary.external_model_promotion_status,
                EXTERNAL_MODEL_PROMOTION_STATUS,
            )

    def test_validation_rejects_promotion_and_missing_provenance_requirements(self) -> None:
        first = MODEL_RELEASE_BOUNDARIES[0]
        eligible = ModelReleaseBoundary(
            **{**first.__dict__, "external_model_promotion_status": "eligible"}
        )
        with self.assertRaises(ValueError):
            validate_model_release_boundaries((eligible,) + MODEL_RELEASE_BOUNDARIES[1:])
        incomplete = ModelReleaseBoundary(
            **{**first.__dict__, "required_release_metadata": ("license",)}
        )
        with self.assertRaises(ValueError):
            validate_model_release_boundaries((incomplete,) + MODEL_RELEASE_BOUNDARIES[1:])


if __name__ == "__main__":
    unittest.main()
