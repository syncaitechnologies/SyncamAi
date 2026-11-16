"""Public keys that sign SyncCam Box updates (Ed25519, PEM).

The private key is the SYNCAM_RELEASE_SIGNING_KEY secret of the GitHub
repository and is used only by the release workflow. To rotate it, add the new
public key here, ship a release signed with the old key, then sign with the new
one and remove the old key. An empty tuple turns automatic updates off.
"""

RELEASE_PUBLIC_KEYS: tuple[str, ...] = (
    "-----BEGIN PUBLIC KEY-----\n"
    "MCowBQYDK2VwAyEA0ag8HoE1iuW5q/mFJrEgOoWwmZso0pAwQFfkUL31AIQ=\n"
    "-----END PUBLIC KEY-----\n",
)
