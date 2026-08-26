import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.dataset_manifest_validator import (  # noqa: E402
    DATASET_MANIFEST_VALIDATION_REFERENCE,
    DatasetManifestRecord,
    validate_dataset_manifest_record,
)
from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402


def _record(**overrides: object) -> DatasetManifestRecord:
    values: dict[str, object] = {
        "capability_identifier": MODEL_REGISTRY[0].identifier,
        "source": "synthetic-unit-test-source",
        "license": "synthetic-unit-test-license",
        "capture_date_range": "synthetic-unit-test-range",
        "tenant_consent_class": "synthetic-unit-test-consent-class",
        "dvc_commit": "synthetic-unit-test-dvc-commit",
        "checksum_manifest": "synthetic-unit-test-checksum-manifest",
        "labeling_qa_sample_rate": 0.05,
        "contains_customer_footage": False,
        "customer_footage_opt_in_reference": None,
        "is_development_or_test_dataset": True,
        "contains_production_customer_data": False,
    }
    values.update(overrides)
    return DatasetManifestRecord(**values)  # type: ignore[arg-type]


class DatasetManifestValidatorTest(unittest.TestCase):
    def test_validates_complete_synthetic_test_metadata_without_loading_data(self) -> None:
        self.assertEqual(
            DATASET_MANIFEST_VALIDATION_REFERENCE,
            "docs/development/phase-4-dataset-manifest-validator.md",
        )
        validate_dataset_manifest_record(_record())

    def test_rejects_unknown_capability_or_missing_provenance_metadata(self) -> None:
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(_record(capability_identifier="unknown"))
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(_record(dvc_commit=" "))

    def test_rejects_weakened_qa_and_development_or_test_customer_data(self) -> None:
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(_record(labeling_qa_sample_rate=0.049))
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(
                _record(contains_production_customer_data=True)
            )

    def test_requires_documented_opt_in_for_customer_footage(self) -> None:
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(_record(contains_customer_footage=True))
        validate_dataset_manifest_record(
            _record(
                contains_customer_footage=True,
                customer_footage_opt_in_reference="synthetic-unit-test-opt-in-reference",
            )
        )
        with self.assertRaises(ValueError):
            validate_dataset_manifest_record(_record(customer_footage_opt_in_reference=" "))


if __name__ == "__main__":
    unittest.main()
