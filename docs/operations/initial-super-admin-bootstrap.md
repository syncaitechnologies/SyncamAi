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
