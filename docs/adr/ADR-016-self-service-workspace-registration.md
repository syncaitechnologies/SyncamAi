# ADR-016: Self-service workspace registration

## Status

Accepted on 2026-11-16 by the product owner. Supersedes the "no self-service
tenant claim" consequence of [ADR-010](ADR-010-initial-super-admin-bootstrap.md);
the rest of ADR-010 (the owner-approved bootstrap procedure) stays in force.

## Context

SyncCam is sold as a plug-and-play service. A prospective customer who opens the
dashboard today reaches a sign-in page they cannot pass: accounts and workspaces
are created only by hand. ADR-010 deliberately forbade any self-service tenant
claim because an application endpoint could otherwise grant itself privilege.

The risk ADR-010 guards against is a user gaining access to a workspace that
already exists. Creating a brand-new, empty workspace for a user who has no
membership at all grants no access to anyone else's data, so it can be allowed
under narrow, audited rules.

## Decision

1. A Supabase Auth user who is authenticated, not anonymous, has a confirmed
   email address and has **no row at all** in `identity.user_tenant_memberships`
   may create exactly one new workspace through `POST /v1/tenants`.
2. The Go API verifies the token with the same issuer, audience and JWKS checks
   as every other route but without requiring `app_metadata.syncam`. It requires
   the server-set `app_metadata.syncam_registration.email_verified` claim, which
   the access-token hook copies from Supabase Auth (`auth.users.email_confirmed_at`);
   user-editable `user_metadata` is never trusted. It then calls the private
   database routine `identity.register_workspace`, which in one transaction:
   - takes an advisory lock on the user and re-checks the absence of a
     membership (a repeat call returns the existing workspace);
   - creates a new tenant (`tier = smb`, `plan = trial`), its first site, an
     active `super_admin` membership with the same scopes and data classes as the
     ADR-010 bootstrap, and a site membership;
   - writes hash-chained `audit.events` rows for the tenant, site and membership.
3. The routine is `SECURITY DEFINER`, owned by a dedicated NOLOGIN, NOBYPASSRLS
   role with only the grants it needs, and executable only by `syncam_app`. It can
   never add a user to an existing tenant. `syncam_app` still has no direct write
   access to tenants or memberships.
4. The custom access-token hook issues a token without the `syncam` claim for a
   user with no membership row, so the user can reach the registration screen.
   A user whose membership is `suspended` or `revoked` is still refused a token.
   Every existing route still requires the `syncam` claim.
5. MFA enrollment remains mandatory for the new `super_admin` before privileged
   use (enforced by the dashboard MFA gate and the Go authorization boundary).
6. New workspaces start a **14-day trial limited to 1 site and 10 cameras**
   (`identity.tenants.plan`, `trial_ends_at`, `site_limit`, `camera_limit`). After
   the trial ends, user routes and site-box ingest return `402 TRIAL_ENDED` until
   the owner extends the trial or sets `plan = active` by an audited manual
   change. Existing tenants default to `plan = active` with no limits.
7. `data_region` records the real hosting region of the current backend
   (`ap-south-1`). Customers are not offered a region choice until residency
   controls exist (see `docs/privacy/cross-border-data-transfer-policy.md`).

## Consequences

- Anyone with a working email address can create an empty workspace. Abuse is
  bounded by the trial limits, one workspace per user (the membership table's
  primary key), the API rate limit, Supabase Auth rate limits and the CAPTCHA the
  owner enables in Supabase Auth.
- Sign-up requires confirmed email in the hosted Supabase project and a custom
  SMTP sender; both are owner-managed dashboard settings, not repository files.
- Terms and privacy consent is collected by an unticked checkbox at sign-up. The
  linked legal texts must be approved by the owner before public launch; the
  drafts in `docs/privacy` are not approved notices.
- Inviting teammates still follows ADR-011 and is not enabled by this decision.
- Billing, entitlements beyond the trial limits and self-serve upgrade remain
  out of scope (see `docs/development/phase-7-billing-delivery-boundary.md`).
