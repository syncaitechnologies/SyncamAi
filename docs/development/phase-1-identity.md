# Phase 1 identity and tenancy setup

The merged identity slice implements the provider-neutral security boundary for T-0310 through T-0312. The persistence slices add T-0313 through T-0316: tenant/site and event/outbox Postgres migrations, transaction-local RLS, idempotent site creation, idempotent event ingestion, and append-only hash-chained audit records. Data erasure remains denied until its dual-approval workflow is implemented.

## Local prerequisites

1. Install Go 1.25.12 or a newer supported patch release. Earlier toolchains fail the current vulnerability gate.
2. Install Docker Desktop with Linux containers for the local Postgres stack and Testcontainers integration tests.
3. Copy `.env.example` to an untracked `.env` and replace every `replace-*` placeholder locally. Use distinct administrative and application database passwords.
4. Start Postgres with `docker compose up -d postgres`.
5. Apply the schema with `go run ./backend/cmd/migrate` from the repository root. The migration connection uses `syncam_admin`; the control plane uses the non-superuser `syncam_app` role.
6. Seed a local tenant through the administrative connection until the onboarding API is implemented. Use a UUID that exactly matches the OIDC `tenant_id` claim; do not seed production data through this path.
7. Configure an OIDC provider with an exact issuer and a public application audience. Cognito is canonical for cloud; Keycloak is the local-only alternative.
8. Configure the token customization hook to emit the canonical claims: `sub`, `email`, `tenant_id`, `site_ids`, `data_class`, `mfa_level`, `token_use=access`, `exp`, `iat`, and `jti`. Generic providers may emit `scopes` and `roles`; Cognito access tokens use `scope`, `cognito:groups`, and `client_id`, which are normalized to the same principal. Missing or non-access `token_use`, unknown roles, missing scopes/data classes/MFA state, or missing sites for a site-scoped role are rejected.
9. Never place a client secret in the React application. The browser application is a public client and uses Authorization Code with PKCE.

Docker Compose reads the root `.env` automatically. Export the same `SYNCAM_*` values into the terminal session before running Go commands; the application deliberately does not parse `.env` files itself.

The control-plane process reads:

- `SYNCAM_OIDC_ISSUER`
- `SYNCAM_OIDC_AUDIENCE`
- `SYNCAM_DATABASE_URL` (non-superuser `syncam_app` connection)
- `SYNCAM_HTTP_ADDR` (defaults to `:8080`)

The migration command separately requires `SYNCAM_MIGRATION_DATABASE_URL`. Never run the HTTP service with the migration/superuser connection.

For Cognito, `SYNCAM_OIDC_ISSUER` is `https://cognito-idp.ap-south-1.amazonaws.com/<user-pool-id>` and `SYNCAM_OIDC_AUDIENCE` is the public app client ID. Access tokens are accepted only when their exact `client_id` matches; generic OIDC access tokens may instead carry that exact value in `aud`.

Every repository operation opens a transaction and derives `app.tenant_id` from the verified token claim. The application database role is `NOSUPERUSER NOBYPASSRLS`; queries without that transaction setting return no tenant rows. `POST /v1/sites` requires `tenant:manage`, `Idempotency-Key`, and an optional UUIDv4 `X-Correlation-Id`. The site, exact replay response, and audit event commit in one transaction.

`POST /v1/events` requires an authenticated Super Admin or Site Admin with explicit `events:write` scope, access to the submitted site, and a UUIDv4 `X-SentinelVision-Request-ID`. New probabilistic events must be pending human review. The normalized event, one unpublished `detection-events-v1` outbox message, and its audit record commit in one transaction. Replaying identical content under the same tenant `dedupe_key` returns the original logical event and creates no additional outbox or audit row; different content returns `409`.

Set `SYNCAM_RUN_INTEGRATION=1` when running `go test ./backend/internal/...` to execute the Docker-backed Postgres isolation tests. GitHub CI enables this automatically.

## Request contract

Protected requests require both:

- `Authorization: Bearer <signed-token>`
- `X-SentinelVision-Tenant-ID: <tenant_id-from-token>`

The tenant header is only a binding assertion. It never creates authentication context and must exactly match the verified claim. A mismatch returns `404 NOT_FOUND` so the API cannot be used as a cross-tenant existence oracle.

## Owner decisions still required

The owner has authorized temporary public implementation publication without an open-source license; T-0309 remains open for final legal review and the later public/private decision. Before any AI model or weights are introduced, Legal must approve ADR-001. No AWS resource is provisioned by these Phase 1 slices. Full tenant onboarding remains deferred until its AWS KMS, storage-prefix, quota, and regional controls can be created atomically rather than leaving a partially isolated tenant.
