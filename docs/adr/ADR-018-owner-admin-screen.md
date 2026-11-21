# ADR-018: Owner admin screen for client plans and limits

## Status

Accepted on 2026-11-20 by the product owner.

## Context

ADR-016 and ADR-017 left plan, trial and live-view changes as manual SQL run
by the project owner. That is slow, easy to get wrong (a typo in a slug or a
missing `where`), and it bypasses the audit log that every other change to a
workspace writes. The owner needs to change a client's plan, trial end, site
and camera limits and live-view minutes from the dashboard, without giving
that power to any client.

Workspace roles (`super_admin` and below) are scoped to one workspace, so none
of them can be reused for a cross-workspace owner.

## Decision

1. A new table `platform.operators (user_id)` lists the people who run the
   SyncCam service. It is not reachable from the browser (FORCE RLS; only the
   token hook may read it). Rows are added by hand in the SQL editor; nothing
   in the app can add an operator.
2. The token hook adds `app_metadata.syncam_operator = true` for those users
   and strips the flag for everyone else. The API reads it into
   `Principal.Operator`; `user_metadata` never grants it.
3. `GET /v1/admin/workspaces` and `PATCH /v1/admin/workspaces/{id}` require
   the operator flag **and** an authenticator-verified session (`aal2`). The
   database functions check the operator table again, so a forged token alone
   cannot change a workspace.
4. Only `plan`, `trial_ends_at`, `site_limit`, `camera_limit` and
   `live_view_minutes` can be changed. Each change writes a hash-chained
   `tenant.plan.changed` row to that workspace's `audit.events`, with the
   values before and after and `operator:<user id>` as the actor.
5. The dashboard shows an "Owner admin" page under Manage only for operators.

## Consequences

- Plan and minute changes no longer need SQL; the checklist SQL remains as a
  fallback.
- A client's own auditors can see when the owner changed their plan, and what
  changed.
- Users see a plan change after their next token refresh (within an hour);
  live-view minutes apply within about a minute.
- The listing reads across all workspaces through a SECURITY DEFINER function;
  it returns only names, slugs, plan fields and counts, never alerts, people
  or footage.
- Losing control of an operator account would expose every client's plan
  settings. Operators must use an authenticator app, and the table should hold
  as few people as possible.
- Card billing and self-service upgrades are still out of scope.
