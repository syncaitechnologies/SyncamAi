# Privacy exposure fixes — T-0406

This follows the [2026-09-08 main audit](2026-09-08-current-state-audit.md) and [draft privacy traceability](../privacy/privacy-implementation-traceability.md). The reviewed main baseline is `06626a1`; these changes are proposed source controls, not production or legal approval.

## Behavior

- Generic HTTP event intake rejects the reserved `attendance_review` type with the existing 422 validation envelope. Both Postgres and memory repositories independently reject it before persistence/replay state. Case and surrounding whitespace cannot bypass this gate. The enum remains for wire compatibility; this is an intentional restriction of formerly accepted behavior, not a claim of behavioral compatibility. Approved non-biometric events still ingest normally. No feature flag enables attendance.
- Control-plane, outbox-worker and lifecycle-delivery-worker entrypoints log fixed failure categories instead of raw database, identity-provider or configuration errors. Operator-visible failure categories and aggregate worker counts remain; raw diagnostic detail is intentionally lost. Any future richer diagnostics need an approved allowlist of safe fields. This does not claim whole-system log redaction.
- The browser stylesheet no longer imports Google Fonts. Existing system font fallbacks render the interface without that font-provider request. Identity/session behavior and other provider requests still require their own inventory review.

## Verification

`scripts/verify.ps1` passed locally with Go 1.25.13, Python 3.14.6 and Node 24.19.0: documentation/task/license/secret/contract/compatibility/traceability validators, Python AI and validator tests, backend coverage threshold, backend command and edge tests, frontend type checks, 28 frontend tests and production build. Local Python/Node differ from CI's pinned 3.12/22; canonical CI is required on this PR.

Regression tests prove attendance does not reach the HTTP repository, no Postgres transaction is attempted, no memory dedupe state is reserved, ordinary intake still succeeds, and authentication/cross-tenant denial remains. Subprocess tests feed malformed synthetic configuration into each service entrypoint and assert only the fixed failure category appears. A frontend regression checks the stylesheet and HTML for external font resources.

The in-app browser loaded the local synthetic demo, showed readable Overview and Alert Center layouts, navigated between them, and reported no captured warning/error console entries. This was a desktop visual/navigation smoke check, not a full accessibility, mobile, authenticated E2E, or network-egress audit. No real account, camera or tenant was used. Docker is absent locally, so database/Testcontainers/RLS integration evidence must come from CI; no local retention/deletion/consent tests or AI promotion evaluation are claimed.

## Remaining gates

The [biometric approval checklist](../governance/biometric-attendance-approval-evidence-checklist.md), [session revocation checklist](../governance/session-revocation-approval-checklist.md), [T-0397 data-path boundary](phase-9-data-path-delivery-boundary.md), [GA boundary](sprint-12-ga-readiness-delivery-boundary.md), and T-0309 remain in force. These fixes add no enrollment, consent capture, erasure, external session revocation, customer rollout or release approval.
