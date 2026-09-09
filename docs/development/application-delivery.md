# Application delivery

The owner requested an integrated application on 2026-09-09, including PR
maintenance, deployment and live verification, with a zero-cost hosting limit
until customers justify an AWS migration. Existing biometric and legal gates
remain in force. This is an engineering backlog, not a GA approval.

## Verified starting point

- Main `84609a5` includes merged PRs 131–135; T-0404 through T-0407 are complete.
- The existing Vercel frontend is reachable in synthetic demo mode. Its presence
  does not establish working authenticated API, camera or evidence journeys.
- The connected Supabase project has migrations through the trigger search-path
  hardening change. Five later committed migrations need deployment review.
- No running Go backend or runtime secret store was identified in the linked
  deployment configuration. Authentication configuration, tenant context and MFA
  must work before privileged live journeys can be validated.

## Delivery order

1. T-0408: consolidate the pending React/React DOM, matching types, Vite/plugin
   and TypeScript upgrades. Verify them together before superseding PRs 3–7.
2. Complete the browser sign-in journey, including MFA, safe failure recovery,
   trusted tenant context and accessible site selection. Preserve server-side
   authorization as the enforcement boundary.
3. Package and configure the existing Go control plane and durable outbox
   delivery for an approved free prototype host; verify same-origin routing or
   explicit CORS, TLS, health, reconnect behavior and deployment secrets.
4. Connect camera management and versioned zones to existing Go contracts;
   test persistence, stale versions, unauthorized access and empty states.
5. Verify event ingestion, alert projection/acknowledgement and reconnects
   through the deployed application with isolated test data. Keep evidence
   and incident capabilities visibly unavailable where their backend is absent.
6. Implement non-biometric rights intake after the existing required owner and
   privacy reviews. Consent, retention/erasure, footage and model processing
   keep their separate prerequisites from the privacy implementation audit.

Each implementation slice receives a task ID, focused PR, canonical verification
and live checks where infrastructure permits. Record the actual outcome and
remaining limitation; never count synthetic journeys as real service evidence.

## T-0408 compatibility

The separate React PRs upgraded React and React DOM to different major versions.
The consolidated change upgrades both to 19.2.8 with matching type packages,
TypeScript 7.0.2, Vite 8.2.2 and its React plugin 6.1.1. React 19 requires an
explicit initial value for the alert client's ref. Existing tests and the modern
JSX transform are preserved.

Migration references: [React 19](https://react.dev/blog/2024/04/25/react-19-upgrade-guide)
and [Vite 8](https://vite.dev/guide/migration).

PR 136 merged as `4c05fbc` after all six GitHub checks passed, full local
verification and a deployed synthetic alert acknowledgement smoke check. The
individual dependency PRs 3–7 are no longer open. This does not validate live APIs.

## T-0409 browser session security

The live sign-in screen verifies the exact access token with Supabase
`getClaims`, checks its subject against the current session, and reads only
trusted `app_metadata.syncam` for UI routing. It does not authorize business
requests: the Go JWT, tenant, role, scope, data-class and MFA checks remain the
enforcement boundary. Missing or unsupported membership fails closed.

Super Admin and Auditor must enroll a TOTP factor or challenge an existing
verified factor before the app mounts. Other users with verified MFA also
challenge at AAL1. Existing unsupported factor types block access rather than
offering a bypass. Standard-role tenant-policy enforcement and action-specific
step-up remain separate server requirements; this slice does not introduce a
new tenant-policy editor or enable export, deletion or biometric workflows.

Enrollment starts only on an explicit button click. The setup secret and QR
remain in component memory, disappear on verification or sign-out, and are not
logged or placed in app storage. Interrupted enrollment can leave an unverified
provider factor; this screen never silently deletes factors or bypasses
identity-verified administrator recovery. Factor limits surface a recoverable
error. The web runtime accepts only `sb_publishable_` keys and bare HTTPS
Supabase project origins; legacy anon JWT keys need replacement with a
publishable key, never with a secret key.

Session restoration ignores stale results after auth events/unmount. Provider
failures present retry/sign-out controls and do not claim remote revocation.
The legacy short-lived API-token bridge is populated only after successful
session checks and cleared while checking, signing out or leaving the gate.
One Supabase client is shared across StrictMode remounts.

Verification covers role/assurance routing, editable-metadata rejection,
invalid claims, unsupported factors, provider failures and restoration races.
Real enrollment and authenticated API verification still require a provisioned
test account and a configured backend; synthetic tests are not that evidence.
Trusted workspace/site selection follows in a separate focused slice.

Local canonical verification passed: 41 web tests, Go/edge tests (80.3% backend
coverage), 49 AI fixture tests and 17 validator cases (the host skipped one
symlink case). The dev-only [browser fixture](../../frontend/apps/web/tests/mfa.html)
exercised enrollment, wrong-code recovery, synthetic AAL2 transition, challenge,
provider outage, unsupported factor and missing membership states with no
console errors. To repeat, run the web dev server and open `/tests/mfa.html`;
use only the displayed synthetic code. The fixture is not a production build
entry, makes no Supabase requests, and is not real MFA or provider evidence.

References: [Supabase TOTP](https://supabase.com/docs/guides/auth/auth-mfa/totp),
[verified claims](https://supabase.com/docs/reference/javascript/auth-getclaims),
security governance §1.7 and [ADR-010](../adr/ADR-010-initial-super-admin-bootstrap.md).

## Hosting decision awaiting account access

The owner explicitly approved Render Free for the portable Go backend on
2026-09-09. Account sign-in is being completed by the owner. No paid resource,
backend deployment or database credential has been provisioned by this step.
Free-tier idle shutdown makes this a prototype, not an always-on CCTV service.
