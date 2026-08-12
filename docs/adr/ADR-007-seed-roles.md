# ADR-007: five seed roles

- Status: Accepted
- Date: 2026-08-12
- Owners: Security, Product

## Decision

Seed Super Admin, Site Admin, Operator, Auditor, and Viewer with deny-by-default permissions. Supervisor, Guard, HR Manager, and Reception are later least-privilege presets built from the same permission model.

## Consequences

- Authorization is enforced server-side and in data access, not only in UI navigation.
- Biometric, raw-video, export, masking, and administrative capabilities are separately scoped and audited.
- Cross-tenant negative tests are merge-blocking.
