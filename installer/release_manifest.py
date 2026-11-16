"""Create and sign the update manifest for a SyncCam Box release.

    # once: create the release key (keep the private file out of Git)
    python installer/release_manifest.py generate-key --out D:/private/syncam-release-signing-key.pem

    # in the release workflow (key from the SYNCAM_RELEASE_SIGNING_KEY secret)
    python installer/release_manifest.py sign --installer dist/SyncCamSetup-0.1.0.exe \
        --version 0.1.0 --tag box-v0.1.0 --out dist

Writes syncam-box-manifest.json and syncam-box-manifest.json.sig (Ed25519,
base64). Boxes verify the signature with the public keys in
ai-services/src/syncam_ai/site_worker/release_keys.py.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

REPOSITORY = "https://github.com/syncaitechnologies/SyncamAi"


def generate_key(out: Path) -> int:
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    if out.exists():
        print(f"{out} already exists; refusing to overwrite a release key.", file=sys.stderr)
        return 1
    key = Ed25519PrivateKey.generate()
    private = key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption())
    out.parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "wb") as handle:
        handle.write(private)
    public = key.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo)
    print(f"Private key written to {out}. Store it as the SYNCAM_RELEASE_SIGNING_KEY repository secret.")
    print("Public key for release_keys.py:")
    print(public.decode())
    return 0


def sign(installer: Path, version: str, tag: str, out: Path, notes: str) -> int:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import load_pem_private_key

    pem = os.environ.get("SYNCAM_RELEASE_SIGNING_KEY", "")
    if not pem.strip():
        print("SYNCAM_RELEASE_SIGNING_KEY is not set.", file=sys.stderr)
        return 1
    key = load_pem_private_key(pem.encode(), password=None)
    if not isinstance(key, Ed25519PrivateKey):
        print("The release key must be Ed25519.", file=sys.stderr)
        return 1
    if tag != f"box-v{version}":
        print(f"Tag {tag} does not match version {version}.", file=sys.stderr)
        return 1
    manifest = {
        "version": version,
        "url": f"{REPOSITORY}/releases/download/{tag}/{installer.name}",
        "sha256": hashlib.sha256(installer.read_bytes()).hexdigest(),
        "size": installer.stat().st_size,
        "published_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "notes": notes,
    }
    body = (json.dumps(manifest, indent=2) + "\n").encode()
    out.mkdir(parents=True, exist_ok=True)
    (out / "syncam-box-manifest.json").write_bytes(body)
    (out / "syncam-box-manifest.json.sig").write_text(base64.b64encode(key.sign(body)).decode() + "\n", encoding="ascii")
    print(f"Signed manifest for {installer.name} ({manifest['sha256']}).")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True)
    generate = commands.add_parser("generate-key")
    generate.add_argument("--out", type=Path, required=True)
    signing = commands.add_parser("sign")
    signing.add_argument("--installer", type=Path, required=True)
    signing.add_argument("--version", required=True)
    signing.add_argument("--tag", required=True)
    signing.add_argument("--out", type=Path, required=True)
    signing.add_argument("--notes", default="")
    args = parser.parse_args(argv)
    if args.command == "generate-key":
        return generate_key(args.out)
    return sign(args.installer, args.version, args.tag, args.out, args.notes)


if __name__ == "__main__":
    raise SystemExit(main())
