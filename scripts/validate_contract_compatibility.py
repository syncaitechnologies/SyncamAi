"""Reject removal or incompatible mutation of previously published contracts."""

from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
BASE_REF = "origin/main"
CONTRACTS = {
    "shared/contracts/avro/detection-event-v1.avsc": "avro",
    "shared/contracts/openapi/v1.yaml": "openapi",
    "shared/contracts/proto/events/v1/events.proto": "proto",
}


def base_text(path: str) -> str | None:
    result = subprocess.run(
        ["git", "show", f"{BASE_REF}:{path}"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=False,
    )
    return result.stdout if result.returncode == 0 else None


def avro_signature(text: str) -> dict[str, object]:
    data = json.loads(text)
    return {field["name"]: field["type"] for field in data.get("fields", [])}


def proto_signature(text: str) -> dict[int, str]:
    return {
        int(number): name
        for name, number in re.findall(r"\b(?:repeated\s+)?\w+\s+(\w+)\s*=\s*(\d+);", text)
    }


def openapi_required(text: str) -> set[str]:
    required_match = re.search(r"\n\s{6}required:\s*\n(?P<body>(?:\s{8}- .+\n)+)", text)
    return set(re.findall(r"-\s+([a-zA-Z0-9_]+)", required_match.group("body"))) if required_match else set()


def main() -> int:
    errors: list[str] = []
    compared = 0
    for path, kind in CONTRACTS.items():
        old = base_text(path)
        if old is None:
            continue
        compared += 1
        new = (ROOT / path).read_text(encoding="utf-8")
        if kind == "avro":
            old_sig, new_sig = avro_signature(old), avro_signature(new)
            for name, field_type in old_sig.items():
                if name not in new_sig:
                    errors.append(f"{path} removed field {name}")
                elif new_sig[name] != field_type:
                    errors.append(f"{path} changed type of {name}")
        elif kind == "proto":
            old_sig, new_sig = proto_signature(old), proto_signature(new)
            for number, name in old_sig.items():
                if new_sig.get(number) != name:
                    errors.append(f"{path} changed field {number} ({name})")
        else:
            removed = sorted(openapi_required(old) - openapi_required(new))
            if removed:
                errors.append(f"{path} removed required fields: {', '.join(removed)}")

    if errors:
        print("Contract compatibility validation failed:", file=sys.stderr)
        print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
        return 1
    print(f"contract compatibility: ok ({compared} prior contracts compared)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
