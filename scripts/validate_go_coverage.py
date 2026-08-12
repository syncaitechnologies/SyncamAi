"""Enforce the Phase 1 Go statement-coverage floor."""

from __future__ import annotations

import pathlib
import sys


def coverage_percent(profile: pathlib.Path) -> float:
    covered = 0
    statements = 0
    lines = profile.read_text(encoding="utf-8").splitlines()
    if not lines or not lines[0].startswith("mode:"):
        raise ValueError("invalid Go coverage profile")
    for line in lines[1:]:
        fields = line.rsplit(" ", 2)
        if len(fields) != 3:
            raise ValueError(f"invalid Go coverage row: {line}")
        statement_count = int(fields[1])
        execution_count = int(fields[2])
        statements += statement_count
        if execution_count > 0:
            covered += statement_count
    return 100.0 if statements == 0 else covered * 100.0 / statements


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: validate_go_coverage.py PROFILE MINIMUM", file=sys.stderr)
        return 2
    profile = pathlib.Path(sys.argv[1])
    minimum = float(sys.argv[2])
    actual = coverage_percent(profile)
    if actual + 1e-9 < minimum:
        print(f"Go coverage {actual:.1f}% is below {minimum:.1f}%", file=sys.stderr)
        return 1
    print(f"Go coverage: ok ({actual:.1f}% >= {minimum:.1f}%)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
