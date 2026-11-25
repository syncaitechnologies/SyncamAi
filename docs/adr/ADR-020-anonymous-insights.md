# ADR-020: Anonymous visitor and table insights

## Status

Accepted on 2026-11-25 by the product owner.

## Context

A restaurant client asked SyncCam to recognise customers by face and report how
often they come back: weekly, monthly and in total. Recognising people by
face without their consent is illegal or tightly restricted where our clients
are:

- **US:** biometric laws in Illinois (BIPA), Texas and Washington, Portland's
  ban on face recognition in places such as restaurants, and New York City's
  signage rule.
- **Canada:** PIPEDA and the provincial privacy laws.
- **India:** the Digital Personal Data Protection Act 2023.

The owner chose to start with anonymous analytics: how many people come,
when, and how the tables are used, with no faces and no identity.

## Decision

1. **Two zone kinds feed the insights, and neither ever raises an alert:**
   - `door_line`: a line across an entrance. Inside is on the right of the
     arrow from the first point to the second, as drawn on the camera
     picture. The dashboard shows a tick towards the inside and has a Flip
     button.
   - `table`: a polygon around a table and its chairs.
2. **The site box counts people only, from its existing tracks.**
   - **Door:** a crossing is an entry or an exit by direction. A track must
     be at least 0.02 (in frame units) past the line on each side, and it is
     counted at most once per direction every 10 s.
   - **Table:** a session starts after 60 s with someone seated and ends
     120 s after the last person leaves. Shorter gaps, such as a washroom
     trip, stay in the session.
3. **Only hourly numbers leave the building.**
   - Per zone and hour: entries, exits, occupied seconds, finished sessions,
     their total length, and the most people seen at once.
   - Each box start has a random run id. The box sends running totals for
     its run every 5 minutes (`POST /v1/site-ingest/analytics`), and the
     server replaces that row (`platform.zone_hours`, keyed by zone, hour
     and run). A retry is harmless and runs add up.
   - No pictures, track ids or positions are sent.
4. **`GET /v1/analytics/insights` returns the summary for a range of
   site-local days** (up to 92):
   - visitors per day;
   - visitors by hour of day;
   - per-zone totals.

   The Insights page shows visitors, the busiest hour, the average dining
   time, table turnover, and charts.
5. **Repeat-visitor recognition is not built.** An opt-in version, where only
   customers who sign up are recognised, can reuse the staff attendance face
   system later, with its own consent flow.

## Consequences

- Restaurants learn footfall, busy hours and table use without any
  biometric data, which works under the laws above.
- Counts are estimates:
  - a person hidden behind someone else may get a new track and be counted
    twice, and groups walking close together may be undercounted;
  - staff using the door are counted;
  - visits shorter than a minute at a table are not sessions.
- Unsent counts live only in the site box's memory, so a restart mid-hour
  loses them (a `shortcut:` in `analytics.py`).
- On sites with a half-hour time zone offset (India), hours are UTC hours
  shown in local time, so the hour-of-day chart rounds :30 boundaries down.
- Counting needs site box 0.1.7 or later; older boxes ignore the new zone
  kinds.
