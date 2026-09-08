"""Check draft document integrity; this validator cannot grant legal approval."""

from __future__ import annotations

import datetime
import hashlib
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
INVENTORY = "docs/privacy/policy-inventory.json"
TRACEABILITY = "docs/privacy/privacy-implementation-traceability.md"
REQUIRED = {
    f"docs/privacy/{name}.md" for name in (
        "global-privacy-policy", "india-privacy-supplement",
        "canada-privacy-supplement", "us-privacy-supplement",
        "biometric-and-face-recognition-notice", "employee-attendance-privacy-notice",
        "camera-video-surveillance-notice", "data-retention-and-deletion-policy",
        "data-subject-request-procedure", "privacy-incident-and-breach-procedure",
        "subprocessor-governance", "cross-border-data-transfer-policy",
        "privacy-jurisdiction-matrix", "privacy-implementation-traceability",
    )
}
WARNING = (
    "> Draft operational privacy policy — requires review and approval by qualified "
    "legal/privacy counsel before production use in the applicable jurisdiction."
)
VERSION = re.compile(r"\d{4}-\d{2}-\d{2}\.draft-[1-9]\d*")


def unique_object(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def validate(root: pathlib.Path) -> list[str]:
    root = root.resolve()
    errors = []
    try:
        payload = json.loads((root / INVENTORY).read_text(encoding="utf-8"),
                             object_pairs_hook=unique_object)
    except (OSError, UnicodeError, ValueError):
        return ["privacy inventory is missing or invalid JSON"]
    if not isinstance(payload, dict) or set(payload) != {
        "schema_version", "status", "traceability", "policies"
    }:
        return ["privacy inventory has invalid fields"]
    if type(payload["schema_version"]) is not int or payload["schema_version"] != 1:
        errors.append("privacy inventory schema_version must be 1")
    if payload["status"] != "draft":
        errors.append("privacy inventory must remain draft; validation is not approval")
    if payload["traceability"] != TRACEABILITY:
        errors.append("privacy inventory must reference canonical local traceability")
    if not isinstance(payload["policies"], list):
        return errors + ["privacy inventory policies must be a list"]

    seen = set()
    for index, policy in enumerate(payload["policies"]):
        label = f"privacy policy entry {index + 1}"
        if not isinstance(policy, dict) or set(policy) != {"path", "version", "sha256"}:
            errors.append(f"{label} has invalid fields")
            continue
        reference = policy["path"]
        # Exact canonical paths reject URLs, traversal, Windows drives and alternate separators.
        if not isinstance(reference, str) or reference not in REQUIRED:
            errors.append(f"{label} has an unsafe or noncanonical path")
            continue
        if reference in seen:
            errors.append(f"duplicate privacy policy: {reference}")
        seen.add(reference)
        target = root / reference
        try:
            if not target.resolve().is_relative_to(root / "docs/privacy"):
                errors.append(f"{reference} resolves outside the privacy directory")
                continue
            raw = target.read_bytes()
            document = raw.decode("utf-8")
        except (OSError, UnicodeError, RuntimeError):
            errors.append(f"{reference} is missing or unreadable")
            continue
        version = policy["version"]
        if not isinstance(version, str) or not VERSION.fullmatch(version):
            errors.append(f"{reference} needs a dated draft version")
        else:
            try:
                datetime.date.fromisoformat(version[:10])
            except ValueError:
                errors.append(f"{reference} has an invalid version date")
            headers = re.findall(r"^Version: `([^`]+)`", document, re.MULTILINE)
            if headers != [version]:
                errors.append(f"{reference} version differs from inventory")
        if WARNING not in document.splitlines()[:6]:
            errors.append(f"{reference} lacks the prominent draft warning")
        digest = policy["sha256"]
        if not isinstance(digest, str) or not re.fullmatch(r"[a-f0-9]{64}", digest):
            errors.append(f"{reference} has an invalid content digest")
        elif hashlib.sha256(raw).hexdigest() != digest:
            errors.append(f"{reference} content differs from inventory; review version and digest")

    if seen != REQUIRED:
        errors.append("privacy inventory must contain all 14 required documents exactly once")
    actual = {p.relative_to(root).as_posix() for p in (root / "docs/privacy").rglob("*.md")
              if p != root / "docs/privacy/README.md"}
    if actual != REQUIRED:
        errors.append("privacy Markdown files differ from the canonical inventory scope")
    return errors


def main() -> int:
    errors = validate(ROOT)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print("privacy policies: ok (14 versioned drafts; no legal approval inferred)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
