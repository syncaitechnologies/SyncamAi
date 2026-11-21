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
| `SYNCAM_OWNER_TELEGRAM_CHAT_ID` | optional: your own Telegram chat ID, to be told when any client's site box goes offline |

**Site box offline alerts.** When a site box sends no heartbeat for 10 minutes,
its site's linked Telegram chats get "🔴 Site box offline", and "🟢 Site box
back online" when it reports again (each once). With
`SYNCAM_OWNER_TELEGRAM_CHAT_ID` set, you get the same notices for every client.
To find your chat ID, link your Telegram to any site in the dashboard, then in
the Supabase SQL editor run
`select chat_id, chat_title from notify.telegram_targets;`. The first check
runs 10 minutes after the API starts, so a restart never looks like an outage.

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

## 5a. Self-service sign-up (ADR-016)

Clients can create their own account and workspace at the dashboard URL
(`?signup` opens the sign-up form). Before sharing it:

1. Apply `20261117090000_self_service_workspace.sql`. It changes the access-token
   hook (users with no workspace now get a token without workspace claims) and
   adds `identity.register_workspace` plus trial columns on `identity.tenants`.
2. **Email sender.** Supabase's built-in email reaches only your own team and is
   heavily rate-limited. Set up custom SMTP (for example Resend, free tier):
   verify a sender domain, then Authentication → Emails → SMTP settings.
3. Authentication → Sign In / Providers → Email: turn on **Confirm email**,
   minimum password length **15**, leave sign-ups on.
4. Authentication → Emails → templates "Confirm signup" and "Reset password":
   include the 6-digit code `{{ .Token }}`, for example
   "Your SyncCam code is {{ .Token }}. It expires in one hour."
5. Authentication → URL configuration: Site URL = the Vercel URL.
6. Recommended: Authentication → Attack protection → CAPTCHA with Cloudflare
   Turnstile (free). The dashboard does not send a CAPTCHA token yet, so turn
   this on only after that small change ships.
7. Optional Vercel variables: `VITE_SYNCAM_TERMS_URL`, `VITE_SYNCAM_PRIVACY_URL`
   and `VITE_SYNCAM_CONTACT_EMAIL` (shown on the trial banner).
   Draft pages ship with the dashboard at `/terms.html` and `/privacy.html`
   (source: `frontend/apps/web/public/`). They are marked "Draft – not yet in
   effect", have highlighted blanks for your company details, and ask search
   engines not to index them. Once a lawyer has approved the text: fill the
   blanks, remove the draft notice and `noindex` line, then set
   `VITE_SYNCAM_TERMS_URL=https://syncam-ai-alert-center.vercel.app/terms.html`
   and `VITE_SYNCAM_PRIVACY_URL=https://syncam-ai-alert-center.vercel.app/privacy.html`
   and redeploy. Until then the sign-up checkbox shows the names without links.

New workspaces get a 14-day trial with 1 site and 10 cameras. Change a client's
plan, trial end, limits and live-view minutes on the dashboard's **Owner admin**
page (Manage menu; ADR-018). Every change is written to that client's audit log.
The page appears only for owners listed in `platform.operators`, signed in with
the authenticator app. Add yourself once (SQL editor, as the project owner),
then sign out and in:

```sql
insert into platform.operators (user_id, note)
select id, 'SyncCam owner' from auth.users where email = '<your sign-in email>';
```

Fallback without the dashboard (SQL editor, as the project owner):

```sql
update identity.tenants set trial_ends_at = now() + interval '14 days' where slug = '<slug>';
update identity.tenants set plan = 'active', site_limit = null, camera_limit = null where slug = '<slug>';
```

Users see the change after their next token refresh (within an hour, or at once
after signing out and in).

## 5b. Live-view minutes (ADR-017)

Every workspace gets 10 live-view minutes a month; the owner's own workspace
is unlimited. To sell more minutes to a client, use **Owner admin** on the
dashboard (empty = unlimited, 0 = off; applies within a minute). The SQL below
does the same without the audit record:

```sql
update identity.tenants set live_view_minutes = 300 where slug = '<slug>';   -- 5 hours a month
update identity.tenants set live_view_minutes = null where slug = '<slug>';  -- unlimited
update identity.tenants set live_view_minutes = 0 where slug = '<slug>';     -- live view off
-- This month's use, in minutes, for every workspace:
select t.slug, t.live_view_minutes as allowance, round(coalesce(u.seconds_used, 0) / 60.0, 1) as used_minutes
from identity.tenants t
left join platform.live_view_usage u on u.tenant_id = t.id and u.month = date_trunc('month', now())::date
order by used_minutes desc;
```

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
- Attendance: sell it as a convenience record, not anti-fraud. The passive
  liveness check can be fooled by a phone photo (see
  [site-box-setup](site-box-setup.md#liveness-anti-spoofing)); for clients who
  need fraud-resistant attendance, quote an infrared/3D face terminal or an
  active kiosk check. Tune `face_liveness_threshold` at each entrance camera,
  and get a legal review of the consent wording and retention.
- Move to paid plans: Vercel Pro (Hobby is non-commercial), Render Starter,
  Supabase Pro (about $52 a month in total).
