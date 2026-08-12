"""Validate roadmap and executable-backlog task identifiers."""

from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
ROADMAP = ROOT / "ENGINEERING-EXECUTION-ROADMAP-SyncCam-AI.md"
TASKS = ROOT / "traceability" / "tasks.json"
TASK_ID = re.compile(r"T-\d{4}")
DEFINITION = re.compile(r"^- \*\*(T-\d{4})\*\*", re.MULTILINE)
SKIP_PARTS = {".git", "node_modules", "dist", ".venv"}


def main() -> int:
    roadmap_ids = set(DEFINITION.findall(ROADMAP.read_text(encoding="utf-8")))
    payload = json.loads(TASKS.read_text(encoding="utf-8"))
    active = payload.get("tasks", [])
    active_ids = [task.get("id") for task in active]
    duplicates = sorted({task_id for task_id in active_ids if active_ids.count(task_id) > 1})
    known = roadmap_ids | set(active_ids)
    errors: list[str] = []

    if duplicates:
        errors.append(f"duplicate active task IDs: {', '.join(duplicates)}")

    required_fields = {"id", "title", "phase", "owner", "dependencies", "status", "acceptance"}
    allowed_statuses = {"planned", "in_progress", "in_review", "complete", "blocked"}
    for task in active:
        missing = sorted(required_fields - task.keys())
        if missing:
            errors.append(f"{task.get('id', '<unknown>')} missing fields: {', '.join(missing)}")
        if task.get("status") not in allowed_statuses:
            errors.append(f"{task.get('id')} has invalid status {task.get('status')}")
        for dependency in task.get("dependencies", []):
            if dependency not in known:
                errors.append(f"{task.get('id')} has unknown dependency {dependency}")

    for path in ROOT.rglob("*"):
        if not path.is_file() or path.suffix.lower() not in {".md", ".json"}:
            continue
        if SKIP_PARTS.intersection(path.relative_to(ROOT).parts):
            continue
        for reference in TASK_ID.findall(path.read_text(encoding="utf-8")):
            if reference not in known:
                errors.append(f"{path.relative_to(ROOT)} references undefined {reference}")

    if errors:
        print("Task reference validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in sorted(set(errors))), file=sys.stderr)
        return 1
    print(f"task references: ok ({len(roadmap_ids)} historical, {len(active_ids)} active)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
