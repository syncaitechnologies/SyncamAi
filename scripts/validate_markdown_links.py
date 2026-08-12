"""Fail when a local Markdown link points to a missing file."""

from __future__ import annotations

import pathlib
import re
import sys
from urllib.parse import unquote

ROOT = pathlib.Path(__file__).resolve().parents[1]
LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SKIP_PARTS = {".git", "node_modules", "dist", ".venv"}


def markdown_files() -> list[pathlib.Path]:
    return [
        path
        for path in ROOT.rglob("*.md")
        if not SKIP_PARTS.intersection(path.relative_to(ROOT).parts)
    ]


def main() -> int:
    errors: list[str] = []
    for document in markdown_files():
        text = document.read_text(encoding="utf-8")
        for raw_target in LINK.findall(text):
            target = raw_target.strip().strip("<>")
            if not target or target.startswith(("#", "http://", "https://", "mailto:")):
                continue
            file_part = unquote(target.split("#", 1)[0])
            candidate = (document.parent / file_part).resolve()
            if not candidate.is_relative_to(ROOT) or not candidate.exists():
                errors.append(f"{document.relative_to(ROOT)} -> {target}")
    if errors:
        print("Broken local Markdown links:", file=sys.stderr)
        print("\n".join(f"- {item}" for item in errors), file=sys.stderr)
        return 1
    print(f"markdown links: ok ({len(markdown_files())} files)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
