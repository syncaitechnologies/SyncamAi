# ADR-010: Bootstrap the initial Super Admin through an approved manual procedure

## Status

Accepted on 2026-09-02 by the product owner.

## Context

Supabase Auth can authenticate a user before SyncCam has granted that user a
tenant membership. The custom access-token hook correctly rejects that
unprovisioned user, so an application endpoint cannot safely create the first
administrator without introducing a privilege bypass.

The user-management boundary added for T-0066 is intentionally not wired to a
Supabase Admin adapter. A future adapter must execute membership changes within
the Go transaction, audit, and outbox boundary, but it cannot itself bootstrap
the first privileged actor.

## Decision

The sole initial Super Admin for a tenant is provisioned only by a documented,
owner-approved manual procedure. The target must already exist in `auth.users`;
the procedure receives that immutable Auth user UUID and an existing tenant
UUID. It creates one active `identity.user_tenant_memberships` record with the
`super_admin` seed role and the matching least-privilege scopes, including
`users:manage`. A Super Admin is tenant-wide and therefore does not receive
site memberships merely to bootstrap access.

The procedure must refuse to run when that tenant already has an active
`super_admin` membership. It is a one-time bootstrap operation, not a general
user-management substitute.

The procedure must run as a separately authorized administrative change, never
from the browser, the Go application role, a client-side Supabase key, or an
unprovisioned access token. It must atomically set the transaction tenant
context, write the membership and a hash-chained audit event, and record the
owner approval/change reference. No generic SQL command, bootstrap password,
service-role key, user UUID, or tenant UUID is committed to this repository.

The new administrator must complete MFA enrollment before they can use a
privileged SyncCam API. The existing Go authorization boundary rejects a Super
Admin token without an MFA level. After the transaction commits, the operator
must verify that the custom access-token hook issues `app_metadata.syncam`
claims for the selected user and must retain the audit/change reference.

## Consequences

- There is no automatic first-user promotion, self-service tenant claim, or
  fallback administrator account.
- `syncam_app` remains non-superuser and does not gain membership-write access
  through this decision.
- Browser-side writes to membership tables remain denied by RLS and grants.
- A runnable administrative procedure requires a subsequent reviewed slice;
  it must preserve atomic membership, audit, and outbox semantics and include
  a recovery procedure for a failed identity-provider delivery.
- After bootstrap, future invite, disable, and reassignment operations must
  enter through the server-side user-management boundary and may not use the
  manual procedure as a general lifecycle path.
