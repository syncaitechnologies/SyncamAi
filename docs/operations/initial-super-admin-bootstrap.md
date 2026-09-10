# Initial Super Admin bootstrap procedure

This is a one-time, owner-authorized database change for a tenant without an
active `super_admin`. It is not a user-management workflow and must not be
performed from the browser, the SyncCam application role, a client key, or a
service-role key.

## Preconditions

- An owner has approved a unique change reference in the organization’s change
  record.
- The intended person already has an immutable UUID in `auth.users` and the
  target tenant already exists. Record those values only in the approved change
  record, not source control or chat.
- The operator has access to the restricted administrative database console;
  they are not using a runtime database role.

## Procedure

1. From the restricted console, invoke the reviewed
   `identity.bootstrap_initial_super_admin` routine with the tenant UUID, Auth
   user UUID, a newly generated request UUID, and the owner change reference.
   The routine has no grant to browser or application roles.
2. Treat any error as a failed change. Do not alter membership tables manually
   and do not retry with a different user or tenant until the owner has reviewed
   the failure.
3. After it commits, have the new administrator enroll MFA and sign in again.
   Confirm that the custom access-token hook issues their trusted
   `app_metadata.syncam` claims and that privileged APIs reject the account
   before MFA is present.
4. Retain the approval reference, request UUID, and resulting append-only audit
   event in the change record. The audit event stores exact canonical payload
   bytes so its hash can be verified.

The routine refuses an absent Auth user or tenant, any existing membership for
the user, and a tenant that already has an active Super Admin. It creates no
site memberships.

## Hosted Auth integrity (T-0413)

The membership insert uses the validated, non-deferrable foreign key from
`identity.user_tenant_memberships.user_id` to `auth.users.id` to enforce the
existing-Auth-user requirement. A missing or concurrently deleted user fails
with SQLSTATE `23503`; the membership and audit remain atomic. The bootstrap
executor does not need direct Auth-table read access or additional permissions
on Supabase's protected `auth` schema. Do not grant inherited browser or service
roles to work around a schema-permission error.

The migration verifies this foreign key before replacing the function and
retains its restricted owner, empty search path and private execute permissions.
CI executes positive, absent-user/tenant, repeated/foreign membership and
audit-failure rollback tests using disposable synthetic fixtures only. MFA
enrollment and real signed-in API verification remain separate release checks.
