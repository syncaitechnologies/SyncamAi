"""Enforce the foundation dependency and model-license allowlists."""

from __future__ import annotations

import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
POLICY = ROOT / "licenses/allowlist.json"


def dependency_names(package_file: pathlib.Path) -> set[str]:
    package = json.loads(package_file.read_text(encoding="utf-8"))
    return set(package.get("dependencies", {})) | set(package.get("devDependencies", {}))


def main() -> int:
    policy = json.loads(POLICY.read_text(encoding="utf-8"))
    permitted = set(policy["package_allowlist"])
    dependencies: set[str] = set()
    for package_file in ROOT.rglob("package.json"):
        if "node_modules" not in package_file.parts:
            dependencies.update(dependency_names(package_file))

    errors: list[str] = []
    unapproved = sorted(dependencies - permitted)
    if unapproved:
        errors.append(f"unapproved package dependencies: {', '.join(unapproved)}")
    if policy.get("model_licenses") != ["Apache-2.0"]:
        errors.append("foundation model license allowlist must contain only Apache-2.0")
    if policy.get("legal_gate_adr") != "docs/adr/ADR-001-model-license.md":
        errors.append("license policy must point to ADR-001")

    if errors:
        print("License validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print(f"licenses: ok ({len(dependencies)} direct package dependencies)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
