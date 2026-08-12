from __future__ import annotations

import contextlib
import io
import json
import pathlib
import tempfile
import unittest
from unittest import mock

from scripts import validate_contracts
from scripts import validate_go_coverage
from scripts import validate_licenses
from scripts import validate_secrets
from scripts import validate_task_references

ROOT = pathlib.Path(__file__).resolve().parents[2]


class FailureFixtureTests(unittest.TestCase):
    def test_invalid_task_dependency_is_rejected(self) -> None:
        payload = json.loads((ROOT / "traceability/tasks.json").read_text(encoding="utf-8"))
        payload["tasks"][0]["dependencies"] = ["T-9999"]
        with tempfile.TemporaryDirectory() as directory:
            task_file = pathlib.Path(directory) / "tasks.json"
            task_file.write_text(json.dumps(payload), encoding="utf-8")
            with mock.patch.object(validate_task_references, "TASKS", task_file):
                with contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(validate_task_references.main(), 1)

    def test_removed_contract_field_is_rejected(self) -> None:
        contract = json.loads(
            (ROOT / "shared/contracts/avro/detection-event-v1.avsc").read_text(encoding="utf-8")
        )
        contract["fields"] = [field for field in contract["fields"] if field["name"] != "tenant_id"]
        with tempfile.TemporaryDirectory() as directory:
            avro_file = pathlib.Path(directory) / "invalid.avsc"
            avro_file.write_text(json.dumps(contract), encoding="utf-8")
            with mock.patch.object(validate_contracts, "AVRO", avro_file):
                with contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(validate_contracts.main(), 1)

    def test_disallowed_model_license_is_rejected(self) -> None:
        policy = json.loads((ROOT / "licenses/allowlist.json").read_text(encoding="utf-8"))
        policy["model_licenses"].append("AGPL-3.0-only")
        with tempfile.TemporaryDirectory() as directory:
            policy_file = pathlib.Path(directory) / "allowlist.json"
            policy_file.write_text(json.dumps(policy), encoding="utf-8")
            with mock.patch.object(validate_licenses, "POLICY", policy_file):
                with contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(validate_licenses.main(), 1)

    def test_go_module_parser_finds_direct_requirements(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            go_mod = pathlib.Path(directory) / "go.mod"
            go_mod.write_text(
                "module example.test/service\n\ngo 1.22\n\nrequire (\n"
                "\texample.test/approved v1.0.0\n"
                "\texample.test/indirect v1.0.0 // indirect\n)\n",
                encoding="utf-8",
            )
            self.assertEqual(
                validate_licenses.go_module_names(go_mod),
                {"example.test/approved"},
            )

    def test_go_coverage_parser_weights_statements(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            profile = pathlib.Path(directory) / "coverage.out"
            profile.write_text(
                "mode: set\nexample.go:1.1,2.2 3 1\nexample.go:4.1,5.2 1 0\n",
                encoding="utf-8",
            )
            self.assertEqual(validate_go_coverage.coverage_percent(profile), 75.0)

    def test_generated_fake_secret_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            fake = "AKIA" + ("A" * 16)
            (root / "fixture.txt").write_text(fake, encoding="utf-8")
            self.assertEqual(validate_secrets.scan(root), ["fixture.txt: AWS access key"])


if __name__ == "__main__":
    unittest.main()
