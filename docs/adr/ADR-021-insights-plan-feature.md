# ADR-021: Visitor and table insights as a plan feature

## Status

Proposed on 2026-11-26.

## Context

Visitor and table insights (ADR-020) ran for every workspace. Restaurant
clients are to buy it as part of their plan, with the platform owner deciding
who has it and each client switching it on or off, like the ADR-019 features.

## Decision

1. **A new feature key, `insights`, named "Visitor and table insights".**
   - It is plan-controlled: the owner adds or removes it per workspace on the
     owner admin screen, through the audited `platform.admin_set_features`.
   - It is an **add-on**: status `available`, but no plan includes it unless
     the owner adds it. Workspaces that already had door counter or table
     zones when this shipped were given it by the migration, so no one lost
     insights they were using.
   - The client switches it on or off for all or chosen cameras on the
     Features page. The card shows visitors and the average table time over
     30 days.
2. **The server enforces it; the site box is unchanged.**
   - `POST /v1/site-ingest/analytics` drops hourly rows from zones on cameras
     where the feature is off or not in the plan, and still answers 202 so the
     box does not retry them.
   - `GET /v1/analytics/insights` answers 403 `FEATURE_NOT_INCLUDED` when the
     plan does not include it. Switched off, past numbers stay readable.
   - The box keeps counting on every door and table zone (box 0.1.7+), so the
     feature does not depend on a box capability report.

## Consequences

- New workspaces see the card under "Add to your plan" until the owner adds it.
- A box still spends a little CPU counting for switched-off cameras; its
  numbers are discarded. Gating the counter on the box is a later, optional
  optimisation.
- The grandfathering insert in the migration is not in the audit log, because
  no operator made it; the migration itself is the record.
- Real table time depends on a table zone drawn around each table; one zone
  around several tables measures them as one.
