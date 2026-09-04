# Phase 1 identity and tenancy setup

The merged identity slice implements the provider-neutral security boundary for T-0310 through T-0312. The persistence slices add T-0313 through T-0318: tenant/site and event/outbox Postgres migrations, transaction-local RLS, idempotent mutations, leased outbox dispatch, an idempotent alert projection, and append-only hash-chained audit records. Data erasure remains denied until its dual-approval workflow is implemented.

> **Temporary MVP overlay (ADR-009, T-0364/T-0365):** Supabase is the
> development Auth/Postgres/RLS platform. Its CLI migrations in
> `backend/supabase` are authoritative, `auth.users.id` is the user key, and
> authorization is issued only from `app_metadata.syncam`. Go remains the
> business-mutation and transactional outbox boundary. Cloudflare queues,
> Vercel deployment changes, AWS migration, datasets/DVC, models, streaming,
> footage, R2, and production evidence storage are all deferred.

## Local prerequisites

1. Install Go 1.25.12 or a newer supported patch release. Earlier toolchains fail the current vulnerability gate.
2. Install Docker Desktop with Linux containers for the Supabase local stack and Testcontainers integration tests.
3. Copy `.env.example` to an untracked `.env` and replace every `replace-*` placeholder locally. Use distinct administrative and application database passwords.
4. Start Supabase with `pnpm exec supabase start --workdir backend`.
5. Apply its disposable local schema with `pnpm exec supabase db reset --workdir backend`. The old Go migration command is retired.
6. Seed only local development membership records. A user's active tenant must match `identity.user_tenant_memberships`; do not seed production data through this path.
7. Configure Supabase with `SYNCAM_OIDC_PROFILE=supabase`, an exact Auth issuer and audience `authenticated`.
8. Enable `identity.syncam_custom_access_token` as the Supabase custom access-token hook. It is an invoker-rights hook callable only by `supabase_auth_admin`, and writes the trusted tenant, sites, roles, scopes and data classes under `app_metadata.syncam`. Never authorize from `user_metadata`.
9. Never place a client secret in the React application. The browser application is a public client and uses Authorization Code with PKCE.

Supabase CLI reads `backend/supabase/config.toml`; keep secrets only in the untracked root `.env` or terminal session. Export the same `SYNCAM_*` values before running Go commands; the application deliberately does not parse `.env` files itself.

The control-plane process reads:

- `SYNCAM_OIDC_ISSUER`
- `SYNCAM_OIDC_AUDIENCE`
- `SYNCAM_OIDC_PROFILE` (`supabase` for this temporary MVP)
- `SYNCAM_DATABASE_URL` (non-superuser `syncam_app` connection)
- `SYNCAM_HTTP_ADDR` (defaults to `:8080`)

Never run the HTTP service with an administrative connection. The `syncam_app` password is activated only by the documented local bootstrap step and is never placed in browser configuration.

For Supabase, `SYNCAM_OIDC_ISSUER` is `https://<project-ref>.supabase.co/auth/v1` and `SYNCAM_OIDC_AUDIENCE` is `authenticated`. Tokens must carry `role=authenticated`; `aal1` remains non-MFA and `aal2` satisfies the existing MFA boundary. The generic/Cognito-compatible profile remains available for later migration.

Every repository operation opens a transaction and derives `app.tenant_id` from the verified token claim. The application database role is `NOSUPERUSER NOBYPASSRLS`; queries without that transaction setting return no tenant rows. `POST /v1/sites` requires `tenant:manage`, `Idempotency-Key`, and an optional UUIDv4 `X-Correlation-Id`. The site, exact replay response, and audit event commit in one transaction.

The T-0066 user-management foundation is currently an internal Go boundary only: `backend/internal/usermanagement` requires a verified, MFA-authenticated Super Admin with explicit `users:manage` scope before it can invoke a provider adapter. No HTTP route, browser write path, Supabase Admin adapter, or user lifecycle mutation is wired. [ADR-010](../adr/ADR-010-initial-super-admin-bootstrap.md) defines the sole initial-Super-Admin policy; [ADR-011](../adr/ADR-011-identity-provider-lifecycle-delivery.md) requires a tenant-scoped transaction to atomically write the requested lifecycle state, audit event, and recoverable outbox message before a separately deployed provider worker delivers the change.

`POST /v1/events` requires an authenticated Super Admin or Site Admin with explicit `events:write` scope, access to the submitted site, and a UUIDv4 `X-SentinelVision-Request-ID`. New probabilistic events must be pending human review. The normalized event, one unpublished `detection-events-v1` outbox message, and its audit record commit in one transaction. Replaying identical content under the same tenant `dedupe_key` returns the original logical event and creates no additional outbox or audit row; different content returns `409`.

For local projection, set `SYNCAM_WORKER_TENANT_ID` to a provisioned tenant UUID and run `go run ./backend/cmd/outbox-worker`. The worker uses 60-second leases and `FOR UPDATE SKIP LOCKED`, then invokes an idempotent alert projector. A consumer receipt and alert commit together before the outbox row is marked published, so a crash between projection and completion safely replays without a second alert. `GET /v1/alerts` requires `alerts:read` and filters queue rows again through the verified site scope. This local projector is replaceable by the canonical Kinesis/MSK publisher when AWS is available; it is not a claim that the cloud backbone exists.

Invitation delivery is a separate, secret-bearing process: `go run ./backend/cmd/lifecycle-delivery-worker`. It requires `SYNCAM_DATABASE_URL`, `SYNCAM_WORKER_TENANT_ID`, `SYNCAM_SUPABASE_URL`, and `SYNCAM_SUPABASE_SECRET_KEY`. Inject the Supabase secret only into that worker's runtime environment; do not put it in `.env`, source control, the HTTP service, browser configuration, log output, or a command line. The worker claims only the explicit actions implemented by its provider: today that is durable invitation intent only. A delivery intent carries its existing target Auth-user UUID only where the action has one; invitation intents retain no target user. This server-only transport identity does not deliver or revoke a session. Disablement remains pending until a separately reviewed session-revocation adapter can revoke the target user's sessions without a browser credential. A timeout or any non-2xx invitation-provider result is held as `reconciliation_required`, not retried, because the provider does not document an invitation idempotency key. Deploying this binary and supplying a production secret remain explicit owner/infrastructure actions; no deployment is created by this repository.

Set `SYNCAM_RUN_INTEGRATION=1` when running `go test ./backend/internal/...` to execute the Docker-backed Postgres isolation tests. GitHub CI enables this automatically.

## Request contract

Protected requests require both:

- `Authorization: Bearer <signed-token>`
- `X-SentinelVision-Tenant-ID: <tenant_id-from-token>`

The tenant header is only a binding assertion. It never creates authentication context and must exactly match the verified claim. A mismatch returns `404 NOT_FOUND` so the API cannot be used as a cross-tenant existence oracle.

## Owner decisions still required

The owner has authorized temporary public implementation publication without an open-source license; T-0309 remains open for final legal review and the later public/private decision. Before any AI model or weights are introduced, Legal must approve ADR-001. No AWS resource is provisioned by these Phase 1 slices. Full tenant onboarding remains deferred until its AWS KMS, storage-prefix, quota, and regional controls can be created atomically rather than leaving a partially isolated tenant.
