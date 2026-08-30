# Supabase local development

This directory is the authoritative migration boundary for SyncCam's temporary
Supabase MVP backend. It contains no remote project reference, access token,
database password, service-role key, or populated environment file.

## Local workflow

1. Install dependencies with `pnpm install --frozen-lockfile`.
2. Start Docker Desktop, then run `pnpm supabase start -- --workdir backend`.
3. Copy the root `.env.example` to an untracked `.env` and provide local-only
   values. Set `SYNCAM_OIDC_PROFILE=supabase`, the local/remote Auth issuer, and
   `SYNCAM_OIDC_AUDIENCE=authenticated`.
4. Reset a disposable local stack with `pnpm supabase:reset`.
5. Run database tests with `pnpm supabase test db -- --workdir backend --local
   supabase/tests` and database advisors with `pnpm supabase db advisors --
   --workdir backend --local --type security --fail-on warn`.

Create new migrations only with `pnpm supabase migration new <descriptive-name>
-- --workdir backend`; never invent timestamps by hand. The `legacy_00000x`
migration names record the source migration that was transferred.

## Application database role

The migration creates `syncam_app` as `NOLOGIN`, `NOSUPERUSER`, and
`NOBYPASSRLS`. Only a local administrator may activate a local password, after
the local stack is running. In PowerShell with `psql` installed:

```powershell
psql "$env:SUPABASE_DB_URL" -v app_password="$env:SYNCAM_POSTGRES_APP_PASSWORD" -c "ALTER ROLE syncam_app LOGIN PASSWORD :'app_password';"
```

Do not use that command against production, do not commit its values, and do
not grant the browser `syncam_app` credentials. Go sets `app.tenant_id` inside
each transaction; absent context fails closed under RLS.

## Remote setup boundary

This PR intentionally does not link, create or alter a hosted Supabase,
Cloudflare or Vercel resource. When the foundation is approved, a project owner
can link a development project explicitly and configure its custom access-token
hook to `identity.syncam_custom_access_token`.
