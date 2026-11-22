# ADR-019: Feature switches for the owner and each client

## Status

Accepted on 2026-11-22 by the product owner.

## Context

Until now an AI feature could only be stopped by editing the site box's JSON
file and restarting it. Clients had no say in which features watched their
cameras, and there was nothing to bill per feature later. Weapon detection
runs on a development model with no stated licence, so it must stay off for
clients until a licensed model replaces it.

The product owner wants both sides in control:

- the owner decides which features each client's plan includes, and will bill
  from that later;
- each client switches included features on or off, for all cameras or
  chosen ones.

## Decision

1. **Two layers decide whether a feature runs.**
   - *Included*: the owner's choice per workspace, stored in
     `identity.tenant_features` and written only through
     `platform.admin_set_features`. That routine re-checks
     `platform.operators` and writes a hash-chained `tenant.features.changed`
     audit event.
   - *On*: the workspace's own switch, with an optional camera list, stored in
     `config.feature_settings`. Every change writes a hash-chained
     `feature.settings.changed` audit event.
   A feature runs where it is included **and** on **and** the site box has it
   loaded.
2. **The catalog lives in code** (`backend/internal/features/catalog.go`):
   zone alerts, loitering, face attendance and live view are *available* and
   included in every plan by default, trials too. Weapon detection is
   *preview* and included in none until the owner adds it. Camera offline
   alerts are *always on*. Fire and smoke, falls, fights, PPE and number plates
   are shown as *coming soon*. Missing rows mean the default, so existing
   workspaces see no change.
3. **Live view keeps its minute allowance** (ADR-017) as its plan setting: it is
   included when `live_view_minutes` is not 0. Clients can also switch it off,
   and the API then refuses sessions with `403 LIVE_VIEW_OFF`.
4. **Only a workspace's super admins switch features**, signed in with their
   authenticator (`tenant:manage`), because switching one off stops security
   alerts. Everyone in the workspace can see the Features page.
5. **The site box applies switches within a minute without a restart.** The
   effective switches are sent in `GET /v1/site-ingest/config` (already polled
   every 60 s) as `features: {key: {on, cameras}}`, limited to that site's
   cameras. Box 0.1.5 skips switched-off zone rules and processors per camera.
   Older boxes ignore the field.
6. **The API drops events from switched-off features** (`202` with
   `ignored: feature_off`), so older boxes are covered and nothing becomes an
   alert, a Telegram message or evidence. If feature settings cannot be read,
   events are let through: a missed intrusion is worse than an unwanted alert.
   Camera offline alerts are never dropped.
7. **Boxes report what they have loaded** in the heartbeat (`capabilities`),
   so the Features page can say "Needs site box setup" when a feature is on
   but the box cannot run it (for example, weapon detection without the
   model).

## Consequences

- Clients see every feature, what it caught in the last 30 days, and what they
  could add. "Ask to add" and "I'm interested" send the owner an email, which
  shows what to build or license next.
- `identity.tenant_features` is the future billing source. Card billing and
  per-feature prices are still out of scope.
- Settings are cached for up to a minute per API instance. A change made on
  this instance applies at once, and the box picks it up on its next config
  refresh.
- The owner must tick **Weapon** for any workspace whose box should keep
  sending weapon alerts. The owner's own workspace needs this before the
  change goes live, or the ingest guard drops its laptop box's weapon alerts.
- Turning attendance off stops new sightings but keeps the records. Erasing
  them stays on the Attendance page.
