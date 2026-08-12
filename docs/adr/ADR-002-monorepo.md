# ADR-002: monorepo

- Status: Accepted
- Date: 2026-08-12
- Owners: CTO, engineering

## Decision

Keep web, Go platform packages, Go edge software, Python AI services, contracts, infrastructure, tests, and operating documentation in one repository. Language-specific modules remain independently buildable.

## Consequences

- Contract and traceability checks run before language builds.
- Path-scoped CI may optimize later but `make verify` remains the complete gate.
- Customer footage, evidence, biometric data, model weights, datasets, credentials, and generated build output are prohibited.
