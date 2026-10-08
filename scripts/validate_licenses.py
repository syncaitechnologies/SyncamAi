"""Enforce the foundation dependency and model-license allowlists."""

from __future__ import annotations

import json
import pathlib
import re
import sys
import tomllib

ROOT = pathlib.Path(__file__).resolve().parents[1]
POLICY = ROOT / "licenses/allowlist.json"


def dependency_names(package_file: pathlib.Path) -> set[str]:
    package = json.loads(package_file.read_text(encoding="utf-8"))
    return set(package.get("dependencies", {})) | set(package.get("devDependencies", {}))


def python_dependency_names(pyproject_file: pathlib.Path) -> set[str]:
    """Return normalized direct project and optional-extra dependency names."""

    project = tomllib.loads(pyproject_file.read_text(encoding="utf-8")).get("project", {})
    requirements = list(project.get("dependencies", []))
    for dependencies in project.get("optional-dependencies", {}).values():
        requirements.extend(dependencies)
    return {
        re.split(r"[<>=!~;[\\s]", requirement, maxsplit=1)[0].lower().replace("_", "-")
        for requirement in requirements
    }


def go_module_names(go_mod: pathlib.Path) -> set[str]:
    dependencies: set[str] = set()
    in_block = False
    for raw_line in go_mod.read_text(encoding="utf-8").splitlines():
        if "// indirect" in raw_line:
            continue
        line = raw_line.split("//", 1)[0].strip()
        if line == "require (":
            in_block = True
            continue
        if in_block and line == ")":
            in_block = False
            continue
        if line.startswith("require "):
            fields = line.split()
            if len(fields) >= 3:
                dependencies.add(fields[1])
        elif in_block:
            fields = line.split()
            if len(fields) >= 2 and re.match(r"^[A-Za-z0-9.-]+/", fields[0]):
                dependencies.add(fields[0])
    return dependencies


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
    permitted_python = set(policy.get("python_package_allowlist", []))
    python_dependencies: set[str] = set()
    for pyproject_file in ROOT.rglob("pyproject.toml"):
        if ".venv" not in pyproject_file.parts and ".git" not in pyproject_file.parts:
            python_dependencies.update(python_dependency_names(pyproject_file))
    unapproved_python = sorted(python_dependencies - permitted_python)
    if unapproved_python:
        errors.append(f"unapproved direct Python packages: {', '.join(unapproved_python)}")
    permitted_go = set(policy.get("go_module_allowlist", []))
    go_dependencies: set[str] = set()
    for go_mod in ROOT.rglob("go.mod"):
        if ".git" not in go_mod.parts:
            go_dependencies.update(go_module_names(go_mod))
    unapproved_go = sorted(go_dependencies - permitted_go)
    if unapproved_go:
        errors.append(f"unapproved direct Go modules: {', '.join(unapproved_go)}")
    if policy.get("model_licenses") != ["Apache-2.0"]:
        errors.append("foundation model license allowlist must contain only Apache-2.0")
    if policy.get("legal_gate_adr") != "docs/adr/ADR-001-model-license.md":
        errors.append("license policy must point to ADR-001")

    if errors:
        print("License validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print(
        "licenses: ok "
        f"({len(dependencies)} direct package dependencies, "
        f"{len(python_dependencies)} direct Python packages, {len(go_dependencies)} direct Go modules)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

