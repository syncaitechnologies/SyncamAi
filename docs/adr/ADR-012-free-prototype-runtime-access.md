# ADR-012: Restricted runtime access for the free Go prototype

## Status

Engineering decision, 2026-09-09, implementing the owner's explicit approval
to use Render Free with the existing Supabase project. This is not a security,
privacy, biometric or GA approval.

## Decision

Keep ADR-008 package boundaries and ADR-009's portable Go business-mutation
boundary. Run the control-plane prototype on one Render Free Go web instance;
keep Vercel as the frontend and Supabase as Auth/Postgres. Do not create a paid
worker, persistent disk, database or hosting upgrade. There is no continuous
outbox-delivery or always-on CCTV claim on this topology.

Create `syncam_render` as a distinct database runtime role inheriting only the
reviewed `syncam_app` permissions. It cannot administer that role, switch to it,
create roles/databases, replicate, act as superuser or bypass RLS. Neither role
owns application objects. Set a 20-connection ceiling to accommodate the
control plane's ten-connection pool during a single deployment overlap.

The migration creates the role with login disabled and no password. Activating
login and assigning a unique cryptographically generated password are separate
secret-provisioning operations, never literals in migrations or Git. Preserve
an already provisioned safe role on migration replay; reject privilege drift
instead of silently accepting a privileged existing role.

Use the verified Supabase session-pooler endpoint with `sslmode=verify-full`.
The database URL and persistent random device-claim key belong only in Render's
runtime secret configuration and the owner's private local credential store.
Account-management tokens are local deployment credentials, not application
environment variables. Do not expose runtime secrets through `VITE_` settings.

## Consequences

- Browser origins use T-0410's exact allowlist; preview wildcards are forbidden.
- Runtime permission and tenant-RLS checks must pass before frontend live mode.
- Initial Super Admin bootstrap still follows ADR-010 and its separate owner
  target/change approval; this role cannot invoke the bootstrap routine.
- Supabase password provisioning and committed migration deployment use
  authorized administrative tooling. Do not deploy with the `postgres` login
  or store a password in migration history as a tooling workaround.
- Render TLS termination does not constitute verified edge client identity;
  edge certificate verification stays fail-closed.
- AWS deployment later replaces hosting/secret adapters, not domain contracts.
- Revocation disables this login and terminates its sessions through an
  authorized operator procedure; disabling login alone does not end sessions.
