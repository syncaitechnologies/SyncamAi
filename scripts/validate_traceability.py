"""Validate MVP requirement-to-task/contract/test/release-gate coverage."""

from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
REQUIREMENTS = ROOT / "traceability/requirements.json"
TASKS = ROOT / "traceability/tasks.json"
ROADMAP = ROOT / "ENGINEERING-EXECUTION-ROADMAP-SyncCam-AI.md"
HISTORICAL_TASK = re.compile(r"^- \*\*(T-\d{4})\*\*", re.MULTILINE)
EXPECTED_MVP = {
    "FR-101", "FR-102", "FR-103a", "FR-106", "FR-107", "FR-108", "FR-109",
    "FR-111", "FR-112", "FR-113", "FR-116", "FR-117", "FR-118", "FR-201",
    "FR-202", "FR-203", "FR-204", "FR-205", "FR-206", "FR-207",
    "MVP-ABANDONED-OBJECT",
}


def local_target(reference: str) -> pathlib.Path:
    return ROOT / reference.split("#", 1)[0]


def main() -> int:
    payload = json.loads(REQUIREMENTS.read_text(encoding="utf-8"))
    requirements = payload.get("requirements", [])
    ids = [item.get("id") for item in requirements]
    active = json.loads(TASKS.read_text(encoding="utf-8")).get("tasks", [])
    known_tasks = set(HISTORICAL_TASK.findall(ROADMAP.read_text(encoding="utf-8")))
    known_tasks.update(task["id"] for task in active)
    errors: list[str] = []

    missing_requirements = sorted(EXPECTED_MVP - set(ids))
    unexpected = sorted(set(ids) - EXPECTED_MVP)
    if missing_requirements:
        errors.append(f"missing MVP requirements: {', '.join(missing_requirements)}")
    if unexpected:
        errors.append(f"unexpected MVP requirements: {', '.join(unexpected)}")
    if len(ids) != len(set(ids)):
        errors.append("duplicate requirement IDs")

    fields = {"id", "title", "owner", "phase", "tasks", "contracts", "tests", "release_gate"}
    for item in requirements:
        requirement_id = item.get("id", "<unknown>")
        absent = sorted(fields - item.keys())
        if absent:
            errors.append(f"{requirement_id} missing fields: {', '.join(absent)}")
            continue
        if not item["owner"] or not item["release_gate"]:
            errors.append(f"{requirement_id} needs owner and release gate")
        for task in item["tasks"]:
            if task not in known_tasks:
                errors.append(f"{requirement_id} references unknown task {task}")
        for reference in item["contracts"] + item["tests"]:
            if not local_target(reference).exists():
                errors.append(f"{requirement_id} references missing file {reference}")

    if errors:
        print("Traceability validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print(f"traceability: ok ({len(requirements)} MVP requirements)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
