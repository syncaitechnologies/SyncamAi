import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.dataset_provenance_boundary import (  # noqa: E402
    CANONICAL_DATASET_PROVENANCE_REFERENCE,
    CUSTOMER_DATA_POLICY,
    DATASET_PROVENANCE_BOUNDARIES,
    DVC_VERSIONING_REQUIREMENT,
    HELD_OUT_EVALUATION_DATASET_USAGE,
    MINIMUM_LABELING_QA_SAMPLE_RATE,
    PLANNED_DATASET_STATUS,
    REQUIRED_DATASET_PROVENANCE_METADATA,
    DatasetProvenanceBoundary,
    validate_dataset_provenance_boundaries,
)
from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402


class DatasetProvenanceBoundaryTest(unittest.TestCase):
    def test_every_registry_capability_has_only_planned_held_out_requirements(self) -> None:
        self.assertEqual(
            {boundary.capability_identifier for boundary in DATASET_PROVENANCE_BOUNDARIES},
            {entry.identifier for entry in MODEL_REGISTRY},
        )
        for boundary in DATASET_PROVENANCE_BOUNDARIES:
            self.assertEqual(
                boundary.provenance_reference, CANONICAL_DATASET_PROVENANCE_REFERENCE
            )
            self.assertEqual(boundary.dataset_usage, HELD_OUT_EVALUATION_DATASET_USAGE)
            self.assertEqual(boundary.versioning_requirement, DVC_VERSIONING_REQUIREMENT)
            self.assertEqual(boundary.required_metadata, REQUIRED_DATASET_PROVENANCE_METADATA)
            self.assertGreaterEqual(
                boundary.minimum_labeling_qa_sample_rate,
                MINIMUM_LABELING_QA_SAMPLE_RATE,
            )
            self.assertEqual(boundary.customer_data_policy, CUSTOMER_DATA_POLICY)
            self.assertEqual(boundary.status, PLANNED_DATASET_STATUS)

    def test_validation_rejects_weakened_qa_or_a_ready_dataset_claim(self) -> None:
        first = DATASET_PROVENANCE_BOUNDARIES[0]
        insufficient_qa = DatasetProvenanceBoundary(
            **{
                **first.__dict__,
                "minimum_labeling_qa_sample_rate": MINIMUM_LABELING_QA_SAMPLE_RATE - 0.01,
            }
        )
        with self.assertRaises(ValueError):
            validate_dataset_provenance_boundaries(
                (insufficient_qa,) + DATASET_PROVENANCE_BOUNDARIES[1:]
            )
        ready = DatasetProvenanceBoundary(**{**first.__dict__, "status": "ready"})
        with self.assertRaises(ValueError):
            validate_dataset_provenance_boundaries(
                (ready,) + DATASET_PROVENANCE_BOUNDARIES[1:]
            )


if __name__ == "__main__":
    unittest.main()
