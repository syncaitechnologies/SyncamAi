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
