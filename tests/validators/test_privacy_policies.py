"""Negative fixtures for the document-only privacy inventory gate."""

import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("privacy_validator", ROOT / "scripts/validate_privacy_policies.py")
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


class PrivacyInventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.inventory = {
            "schema_version": 1, "status": "draft", "traceability": validator.TRACEABILITY,
            "policies": [],
        }
        for reference in sorted(validator.REQUIRED):
            document = f"# Example draft\n\n{validator.WARNING}\n\nVersion: `2026-09-08.draft-1`.\n"
            target = self.root / reference
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(document, encoding="utf-8", newline="\n")
            self.inventory["policies"].append({
                "path": reference, "version": "2026-09-08.draft-1",
                "sha256": hashlib.sha256(target.read_bytes()).hexdigest(),
            })
        self.save()

    def save(self):
        (self.root / validator.INVENTORY).write_text(json.dumps(self.inventory), encoding="utf-8")

    def test_complete_draft_inventory_passes(self):
        self.assertEqual(validator.validate(self.root), [])

    def test_missing_and_duplicate_documents_fail(self):
        self.inventory["policies"][-1] = self.inventory["policies"][0]
        self.save()
        errors = validator.validate(self.root)
        self.assertTrue(any("duplicate" in e for e in errors))
        self.assertTrue(any("all 14" in e for e in errors))

    def test_missing_file_and_unlisted_file_fail(self):
        (self.root / self.inventory["policies"][0]["path"]).unlink()
        (self.root / "docs/privacy/unreviewed.md").write_text("Unreviewed", encoding="utf-8")
        errors = validator.validate(self.root)
        self.assertTrue(any("unreadable" in e for e in errors))
        self.assertTrue(any("scope" in e for e in errors))

    def test_content_change_without_inventory_update_fails(self):
        target = self.root / self.inventory["policies"][0]["path"]
        with target.open("a", encoding="utf-8") as stream:
            stream.write("Changed processing claim.\n")
        self.assertTrue(any("content differs" in e for e in validator.validate(self.root)))

    def test_version_mismatch_and_invalid_date_fail(self):
        for version in ["2026-09-08.draft-2", "2026-02-30.draft-1", "approved", None]:
            with self.subTest(version=version):
                self.inventory["policies"][0]["version"] = version
                self.save()
                self.assertTrue(validator.validate(self.root))

    def test_removed_or_buried_draft_warning_fails_even_with_updated_digest(self):
        item = self.inventory["policies"][0]
        target = self.root / item["path"]
        for prefix in ["", "\n" * 10 + validator.WARNING]:
            target.write_text(f"# Example\nVersion: `2026-09-08.draft-1`.\n{prefix}", encoding="utf-8")
            item["sha256"] = hashlib.sha256(target.read_bytes()).hexdigest()
            self.save()
            self.assertTrue(any("draft warning" in e for e in validator.validate(self.root)))

    def test_unsafe_paths_are_rejected(self):
        for reference in ["../private.md", "/private.md", "C:/private.md",
                          "docs/privacy/../../private.md", "https://example.test/policy",
                          "docs\\privacy\\global-privacy-policy.md", None]:
            with self.subTest(reference=reference):
                self.inventory["policies"][0]["path"] = reference
                self.save()
                self.assertTrue(any("path" in e for e in validator.validate(self.root)))

    def test_false_approval_and_external_traceability_fail(self):
        self.inventory["status"] = "approved"
        self.inventory["traceability"] = "https://example.test/approval"
        self.save()
        errors = validator.validate(self.root)
        self.assertTrue(any("not approval" in e for e in errors))
        self.assertTrue(any("canonical local traceability" in e for e in errors))

    def test_malformed_inventory_shapes_fail(self):
        inventory_file = self.root / validator.INVENTORY
        for payload in ["{", "null", '[]', '{"status":"draft","status":"approved"}']:
            inventory_file.write_text(payload, encoding="utf-8")
            self.assertTrue(validator.validate(self.root))
        self.inventory["policies"] = [None, {}]
        self.save()
        self.assertTrue(validator.validate(self.root))

    def test_invalid_schema_digest_and_extra_approval_field_fail(self):
        self.inventory["schema_version"] = True
        self.inventory["policies"][0]["sha256"] = 123
        self.inventory["policies"][1]["approved"] = True
        self.save()
        errors = validator.validate(self.root)
        self.assertTrue(any("schema_version" in e for e in errors))
        self.assertTrue(any("invalid content digest" in e for e in errors))
        self.assertTrue(any("invalid fields" in e for e in errors))

    def test_symlink_outside_privacy_directory_fails(self):
        target = self.root / self.inventory["policies"][0]["path"]
        outside = self.root / "outside.md"
        outside.write_bytes(target.read_bytes())
        target.unlink()
        try:
            target.symlink_to(outside)
        except OSError:
            self.skipTest("host does not allow creating test symlinks")
        self.assertTrue(any("outside the privacy directory" in e for e in validator.validate(self.root)))


if __name__ == "__main__":
    unittest.main()
