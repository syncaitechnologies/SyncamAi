# ADR-001: model and code licensing

- Status: Accepted for Apache-only development; legal approval required for production promotion
- Date: 2026-08-12
- Owners: CTO, Legal, AI lead
- Related: D6, T-0256, FR-101–FR-116

## Context

Some candidate detector implementations and weights carry AGPL or commercial terms. A public source repository and a commercial edge product cannot silently assume those terms are acceptable.

## Decision

SyncCam development uses only model code and artifacts whose individual, recorded
terms are Apache-2.0-compatible. The initial development person-detector adapter
targets Intel Open Model Zoo `person-detection-0200`; its XML and BIN are fetched
outside Git only after their published SHA-384 checksums are verified. Every later
model, weight file, runtime and dataset must be recorded independently: a
permissive repository licence does not automatically cover a downloaded weight or
its training data.

Commercial/AGPL alternatives, including Ultralytics YOLO code or weights, may
enter neither the repository nor the release pipeline without Legal recording the
applicable licence, distribution obligations, weight provenance and approval here.
CI enforces the allowlist in `licenses/allowlist.json`.

## Consequences

- No model weights or datasets are committed.
- Every model release requires license, provenance, model card, signature, and rollback metadata.
- The Apache-only decision permits isolated developer-machine runtime integration
  with no customer footage, persistent storage, alert emission, model promotion or
  production claim. It does not approve a production deployment.
- External model promotion remains blocked until Legal approves the exact
  artifact, dataset provenance, deployment terms and release evidence.
- Face recognition, enrolment, matching and liveness remain subject to their
  dedicated biometric approval boundary; this ADR does not relax it.

