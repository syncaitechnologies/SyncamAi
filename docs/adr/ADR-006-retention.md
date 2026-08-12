# ADR-006: retention range and presets

- Status: Accepted
- Date: 2026-08-12
- Owners: Product, Security, Data

## Decision

Tenant-configurable retention is 7–365 days. User-interface presets are 7, 15, 30, 90, and 365 days. Thirty days is the default unless a data class or approved jurisdiction policy requires a different value.

## Consequences

- Retention is enforced in schema policies, object lifecycle, indexes, and erasure jobs.
- Legal hold is explicit, access-blocked, audited, and listed as an erasure-manifest exception.
- Plan-specific retention values are presets, not incompatible storage classes.
