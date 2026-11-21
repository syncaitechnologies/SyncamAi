# ADR-017: Metered live view per workspace

## Status

Accepted on 2026-11-20 by the product owner.

## Context

Near-live view relays about two JPEG frames a second per watched camera
through the Render API (estimated at roughly 360 MB per camera-hour; not yet
measured). Render charges for outbound bandwidth beyond a small monthly
allowance, so a camera wall left open on a monitor could cost hundreds of
dollars a month. Clients already have their own recorder and its app for
viewing and playback, and SyncCam's value is its alerts, so live view is a
convenience that should be offered in proportion to what a client pays.

## Decision

1. Each workspace has a monthly live-view allowance in minutes
   (`identity.tenants.live_view_minutes`): default **10**, `NULL` = unlimited,
   `0` = off. Workspaces already active when this was introduced (the owner's
   own workspace) are unlimited. The owner raises the allowance for paying
   clients by an audited manual change, as with plans in ADR-016.
2. The API counts viewing time per viewer per camera (each viewer's download
   costs bandwidth) per calendar month in UTC, and stores only the total
   seconds per workspace per month in `platform.live_view_usage`. No frames,
   images or viewer identities are stored there. Usage is kept in memory and
   saved every 30 seconds and at shutdown.
3. Starting live view is refused with `403 LIVE_VIEW_LIMIT` when the allowance
   is used up or zero, before any audit record or frame request. A running
   session is stopped with the same error when the allowance runs out.
4. One session pauses after **5 minutes** (`409 LIVE_VIEW_PAUSED`); continuing
   starts a new, audited session.
5. The Camera Wall shows each camera's reference still (uploaded by the box
   every 5 minutes, stored like today for drawing zones) and plays live video
   only for the one camera opened in the spotlight.
6. If the allowance cannot be read, live view does not start (fails closed).

## Consequences

- All existing live-view promises stay: Operator+ with the `raw_video` data
  class only; every session audited before the first frame; frames held in the
  API's memory and never stored; the box sends frames only for watched
  cameras; `"live_view": false` keeps a site's video on the premises.
- `GET /v1/live-view/usage` reports the allowance and use so the dashboard can
  show "3 of 10 min used this month".
- The meter, like the frame hub, is per process and fits the single Render
  instance; several API instances would need a shared counter.
- Selling extra minutes is manual (owner SQL in the go-live checklist) until
  card billing exists. Paid credits are out of scope here.
- Reference stills are refreshed every 5 minutes instead of 30, adding a
  little upload from each site box (box version 0.1.2).
