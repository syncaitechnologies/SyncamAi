# ADR-001: model and code licensing

- Status: Proposed — legal approval required
- Date: 2026-08-12
- Owners: CTO, Legal, AI lead
- Related: D6, T-0256, FR-101–FR-116

## Context

Some candidate detector implementations and weights carry AGPL or commercial terms. A public source repository and a commercial edge product cannot silently assume those terms are acceptable.

## Decision

Use an Apache-2.0-compatible detector implementation and weights by default. A commercial/AGPL alternative may enter the repository or release pipeline only after Legal records the applicable license, distribution obligations, weight provenance, and approval here. CI enforces the allowlist in `licenses/allowlist.json`.

## Consequences

- No model weights or datasets are committed.
- Every model release requires license, provenance, model card, signature, and rollback metadata.
- Until Legal changes this ADR to Accepted, external model promotion is blocked; synthetic stubs may be used for platform development.
