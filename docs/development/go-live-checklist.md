# Go-live checklist (live MVP)

Everything runs on the existing services: Vercel (dashboard), Render (Go API),
Supabase (database, login, snapshot storage) and a site box at each client.
Total cost stays $0 on free tiers until the first paying client.

## 1. Database (Supabase)

1. Apply the new migrations in `backend/supabase/migrations/`:
   `20261112120000_site_ingest_notifications.sql` and
   `20261113090000_staff_attendance.sql` (`pnpm exec supabase db push --workdir backend`).
2. Storage → create a **private** bucket named `snapshots`. Do not make it public.
3. Authentication → Hooks → enable **Custom Access Token** with
   `identity.syncam_custom_access_token`, so signed-in users receive their
   tenant, sites and roles.
4. Project settings → API keys: copy the project URL, the **publishable** key
   (for Vercel) and a **secret** key (for Render only — never for Vercel).

## 2. Telegram bot

1. In Telegram, message **@BotFather**, send `/newbot`, choose a name such as
   "SyncCam Alerts" and a username ending in `bot`.
2. Keep the bot token secret; it goes only into Render.

## 3. API (Render, service `syncam-api`)

Add these environment variables, then redeploy from the branch that contains
this work:

| Variable | Value |
|---|---|
| `SYNCAM_INPROCESS_OUTBOX` | `true` (turns events into alerts for every tenant) |
| `SYNCAM_SUPABASE_URL` | `https://<project-ref>.supabase.co` |
| `SYNCAM_SUPABASE_SECRET_KEY` | the Supabase secret key |
| `SYNCAM_EVIDENCE_BUCKET` | `snapshots` |
| `SYNCAM_TELEGRAM_BOT_TOKEN` | the BotFather token |
| `SYNCAM_TELEGRAM_BOT_HANDLE` | the bot username without `@` |
| `SYNCAM_BROWSER_ORIGINS` | must include `https://syncam-ai-alert-center.vercel.app` |

The free instance sleeps after 15 idle minutes; the site box heartbeat every
two minutes keeps it awake while a site is connected.

## 4. Dashboard (Vercel, project `syncam-ai-alert-center`)

| Variable | Value |
|---|---|
| `VITE_SYNCAM_DATA_MODE` | `live` |
| `VITE_SYNCAM_API_BASE_URL` | `https://syncam-api.onrender.com` |
| `VITE_SUPABASE_URL` | `https://<project-ref>.supabase.co` |
| `VITE_SUPABASE_PUBLISHABLE_KEY` | the publishable key |

`VITE_SYNCAM_TENANT_ID` and `VITE_SYNCAM_SITE_ID` are no longer needed: the
tenant and sites come from each user's login. Add the Vercel URL to Supabase
Authentication → URL configuration → Redirect URLs.

## 5. First client site

1. Sign in, then **Configuration**: add the site, add each camera, create a
   site key, and link Telegram (tap "Open in Telegram").
2. On the site box follow [site-box-setup.md](site-box-setup.md).
3. **Zones & rules**: draw zones on the camera pictures and set schedules.

## 6. End-to-end test

- Walk into an armed zone: a Telegram photo arrives within seconds and the
  alert appears in the Alert Center with the snapshot; acknowledge it.
- Stand near a parked car longer than the dwell time: "Person loitering near
  vehicles".
- Unplug a camera: "Camera offline" after about a minute.
- Optional (development models): toy gun → "Possible weapon"; an enrolled,
  consenting staff member at the entrance → attendance row.

## Before charging clients

- Replace the development weapon model with a licence-clean model.
- Add face liveness and a legal review of consent wording and retention.
- Move to paid plans: Vercel Pro (Hobby is non-commercial), Render Starter,
  Supabase Pro (about $52 a month in total).
