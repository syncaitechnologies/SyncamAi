# Phase 3 bounded privacy-release artifact loader

T-0427 defines the model-free local parsing boundary that must run before a
provisioned privacy-mask release may be presented to the existing controlled
release gate. It handles metadata only and performs no activation.

## Artifact contract

The loader accepts one JSON object with schema version `1`, an exact hardware
profile ID and one existing privacy-mask release manifest. Input is capped at
128 KiB. Empty or malformed input, duplicate keys at any nesting level, unknown
fields and additional trailing JSON fail closed.

The caller configures the expected tenant, site, camera and hardware profile.
The profile binds a device UUID and physical-HIL harness ID. All six scope
values must match the artifact exactly. Neither scope nor trust roots are
learned from the artifact.

The loader then:

1. validates the release UUID and positive version;
2. revalidates the approved two-reviewer privacy-mask candidate;
3. requires the exact `decode -> mask -> encode` pipeline;
4. recomputes and compares the deterministic candidate hash; and
5. verifies the physical-HIL signature with the injected Ed25519 public key.

Success returns a defensive copy of the manifest, the matched profile ID, and
the deterministic candidate and evidence hashes. The caller may next present
that manifest to the existing controlled release gate, which independently
enforces acceptance, ordering and application semantics.

## Deliberate non-capabilities

This loader has no file path API, trust-store API, network transport, private
key, credential, hardware executor, stream, frame or pixel interface. It does
not persist state, approve a candidate, manufacture HIL evidence, accept a
release, activate masking or enable analytics.

Production remains blocked on an approved atomic artifact-provisioning and
OS-backed trust design, executable composition, a real allowlisted hardware
executor, physical signed-mask HIL evidence, and explicit review. Synthetic
tests exercise only parsing, scope and cryptographic verification boundaries.
