"""Validate the repository's changed-line coverage policy."""

from __future__ import annotations

import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
POLICY = ROOT / ".github/coverage-policy.json"


def main() -> int:
    policy = json.loads(POLICY.read_text(encoding="utf-8"))
    errors: list[str] = []
    if policy.get("changed_line_minimum_percent") != 80:
        errors.append("changed-line minimum must remain 80 percent")
    if policy.get("allow_threshold_reduction") is not False:
        errors.append("threshold reduction must require a policy change and review")
    if not policy.get("measured_languages"):
        errors.append("measured_languages must not be empty")
    if errors:
        print("Coverage policy validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print("coverage policy: ok (80% changed lines)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
