import pathlib
import sys
import unittest
from dataclasses import replace

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.model_registry import MODEL_REGISTRY  # noqa: E402
from syncam_ai.model_registry_catalog import (  # noqa: E402
    MODEL_REGISTRY_CATALOG,
    MODEL_REGISTRY_CATALOG_SCHEMA_VERSION,
    SYNTHETIC_READ_ONLY_CATALOG_MODE,
    validate_model_registry_catalog,
)


class ModelRegistryCatalogTest(unittest.TestCase):
    def test_catalog_is_versioned_synthetic_and_covers_the_registry(self) -> None:
        self.assertEqual(
            MODEL_REGISTRY_CATALOG.schema_version,
            MODEL_REGISTRY_CATALOG_SCHEMA_VERSION,
        )
        self.assertEqual(MODEL_REGISTRY_CATALOG.mode, SYNTHETIC_READ_ONLY_CATALOG_MODE)
        self.assertEqual(
            [entry.identifier for entry in MODEL_REGISTRY_CATALOG.capabilities],
            [entry.identifier for entry in MODEL_REGISTRY],
        )
        for entry in MODEL_REGISTRY_CATALOG.capabilities:
            self.assertTrue(entry.family)
            self.assertTrue(entry.owner)
            self.assertTrue(entry.hardware_tier)

    def test_catalog_rejects_live_mode_and_releasable_states(self) -> None:
        with self.assertRaises(ValueError):
            validate_model_registry_catalog(
                replace(MODEL_REGISTRY_CATALOG, mode="live")
            )

        first = MODEL_REGISTRY_CATALOG.capabilities[0]
        releasable = replace(
            first,
            external_model_promotion_status="eligible",
        )
        with self.assertRaises(ValueError):
            validate_model_registry_catalog(
                replace(
                    MODEL_REGISTRY_CATALOG,
                    capabilities=(releasable,) + MODEL_REGISTRY_CATALOG.capabilities[1:],
                )
            )


if __name__ == "__main__":
    unittest.main()
