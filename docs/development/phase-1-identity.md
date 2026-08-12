# Phase 1 identity and tenancy setup

This slice implements the provider-neutral security boundary for T-0310 through T-0312. It verifies signed OIDC tokens, binds every request to the verified `tenant_id`, applies the five seed roles plus explicit scopes and data classes, enforces MFA on privileged paths, and filters site data again after repository access. Data erasure remains denied until its dual-approval workflow is implemented.

## Local prerequisites

1. Install Go 1.25.12 or a newer supported patch release. Earlier toolchains fail the current vulnerability gate.
2. Copy `.env.example` to an untracked `.env` and replace every `replace-*` placeholder locally.
3. Configure an OIDC provider with an exact issuer and a public application audience. Cognito is canonical for cloud; Keycloak is the local-only alternative.
4. Configure the token customization hook to emit the canonical claims: `sub`, `email`, `tenant_id`, `site_ids`, `scopes`, `roles`, `data_class`, `mfa_level`, `token_use=access`, `exp`, `iat`, and `jti`. Tokens with an explicit non-access `token_use`, unknown roles, missing scopes/data classes/MFA state, or missing sites for a site-scoped role are rejected.
5. Never place a client secret in the React application. The browser application is a public client and uses Authorization Code with PKCE.

The control-plane process reads:

- `SYNCAM_OIDC_ISSUER`
- `SYNCAM_OIDC_AUDIENCE`
- `SYNCAM_HTTP_ADDR` (defaults to `:8080`)
- optional local-only bootstrap values `SYNCAM_BOOTSTRAP_TENANT_ID`, `SYNCAM_BOOTSTRAP_SITE_ID`, and `SYNCAM_BOOTSTRAP_SITE_NAME`

The bootstrap site repository is deliberately in-memory and is not an approval to persist production data without Postgres row-level security. The next backend slice adds tenant/site migrations, an RLS-backed repository, idempotent mutations, and append-only audit records.

## Request contract

Protected requests require both:

- `Authorization: Bearer <signed-token>`
- `X-SentinelVision-Tenant-ID: <tenant_id-from-token>`

The tenant header is only a binding assertion. It never creates authentication context and must exactly match the verified claim. A mismatch returns `404 NOT_FOUND` so the API cannot be used as a cross-tenant existence oracle.

## Owner decisions still required

Before implementation code is treated as approved for continued public publication, Owner and Legal must close T-0309 by selecting a repository license and recording the public-source policy. Before any AI model or weights are introduced, Legal must approve ADR-001. Neither decision is required to review this identity-only slice, and no AWS resource is provisioned by it.
