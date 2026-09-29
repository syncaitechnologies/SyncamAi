# Three-feature client walkthrough and live blockers

Task: T-0432. Inspected `origin/main` at `99dea876` on 2026-09-29 UTC.
PR 18 is merged at `4e6c0cde`. The owner's immediate presentation goal is to
open the web app from a mobile phone. The delivery follows
[ADR-013](../adr/ADR-013-mobile-three-feature-demo.md).

## Presentation

The application has an **AI walkthrough** navigation item and a direct
`?demo=vision` entry in demo mode. A deployed Vercel preview uses the same
frontend build as the app. This query never changes live-mode authentication.

1. Open the preview link with `?demo=vision` on the phone.
2. Person detection: start the scenario, then advance twice. Show a fictional
   local track entering the restricted zone and acknowledge the demo review.
   Choose the empty or outside-zone scenario to show that no review is queued.
3. Weapon detection: advance three consistent fixture observations to a review.
   Change the threshold to 90% to suppress the 84% fixture, or choose the tool
   scenario to show a negative path. Scores are scripted, not model metrics.
4. Face recognition: start with both simulated prerequisites unchecked to show
   blocking. Enable the fictional opt-in and liveness checks, then advance
   three times to a candidate for human review. Unknown and failed-liveness
   scenarios remain blocked. No real identity, consent or attendance is created.
5. Explain the visible simulation notice. The screen demonstrates the product
   workflow, not an operational CCTV detector or tested recognition accuracy.

The screen requests no camera permission, accepts no uploads, loads no models,
creates no network request of its own and stores no walkthrough state outside
component memory. Feature/scenario/threshold/prerequisite changes reset progress.
Live mode shows an unavailable state instead of simulated observations.

Local launch from the repository root:

```powershell
pnpm --dir frontend/apps/web dev --host 127.0.0.1
```

Open `http://127.0.0.1:5173/?demo=vision` on the development computer.
Use the HTTPS deployment preview for the phone; localhost on a phone points
to the phone, not the development computer.

## Why these features are not live

| Capability | Existing implementation | Missing for real processing | Owner |
|---|---|---|---|
| Person detection | Deterministic detection-to-track and track-to-zone metadata libraries | Licensed approved detector/weights; pixel preprocessing and inference runtime; masked frame handoff; executable composition and held-out camera tests | AI, Edge, Platform |
| Weapon detection, FR-101 | `WeaponReviewConfirmation` validates supplied candidate metadata across three observations | Approved knife/firearm weights; actual detector; tool/weapon negatives and distance/lighting evaluation; site tuning; runtime-to-human-review delivery | AI, Product, Legal |
| Face attendance, FR-102 | Planned registry entries and approval templates; generic biometric event ingestion is blocked | Recorded purpose/site and privacy/security approvals; opt-in/withdrawal ledger; enrollment and encrypted templates; face/recognition/liveness models and evaluation; dedicated attendance contracts, persistence, audit and correction UI | Product, Privacy/Legal, Security, AI, Platform |
| Shared live path | Go control-plane, outbox, authenticated APIs, edge frame/mask libraries and React live clients | A deployed authenticated runtime and durable delivery verified end to end; approved edge client-certificate ingress; trusted executable mask release loading; detector integration | Platform, Edge, Infrastructure |

Evidence is in the source and existing delivery boundaries:

- [`detection_tracking.py`](../../ai-services/src/syncam_ai/detection_tracking.py)
  accepts detection metadata; it does not infer from images.
- [`weapon_review.py`](../../ai-services/src/syncam_ai/weapon_review.py)
  accepts weapon candidate metadata; its temporal tests do not prove detection.
- [`model-registry-model.ts`](../../frontend/apps/web/src/model-registry-model.ts)
  labels every catalog capability blocked, in `synthetic_read_only` mode.
- [`edge-agent/main.go`](../../edge/cmd/edge-agent/main.go) composes heartbeat,
  configuration, spool and RTSP lifecycle without a promoted detector.
- [Model release boundary](phase-4-model-release-boundary.md) and
  [ADR-001](../adr/ADR-001-model-license.md) keep external-model promotion
  blocked pending recorded approval, provenance and release metadata.
- [Face attendance boundary](phase-5-consent-attendance-boundary.md) requires
  purpose, privacy/legal, security, model and evaluation approvals before
  actual biometric implementation.
- [ADR-009](../adr/ADR-009-supabase-cloudflare-mvp-backend.md) supersedes the
  AWS-first prototype topology. AWS account verification is not the only or
  primary blocker to a local AI demonstration.
- The existing public Vercel page was opened in the browser this turn and
  showed `Local demo feed`, `Synthetic workspace` and synthetic telemetry.
  This observation establishes frontend reachability, not backend health.
  Hosted backend configuration and authenticated journeys were not inspected.

## Smallest route to a real three-feature prototype

1. Select and approve the person/weapon implementations **and weights** under
   ADR-001; record exact version, checksum, license/provenance and evaluation
   plan. Keep artifacts in private local/model storage, outside this repository.
2. Run a Python 3.12 inference process on an approved local CPU/GPU host. Start
   with controlled authorized clips and no claim of always-on safety coverage.
   Connect masked frames to real detections, then existing track/zone and
   weapon-review boundaries. Measure person and weapon outcomes on held-out
   footage, including tool negatives, occlusion, lighting and small objects.
3. Connect review-required outputs through verified device authentication,
   durable event/outbox delivery and the live React queue. Verify tenant/site
   negatives, replay/reconnect, review actions and measured end-to-end results.
4. Complete the existing Phase 5 evidence checklist. Implement staff attendance
   only after its named approvals, starting with consent/withdrawal and dedicated
   isolated storage, then enrollment, matching, liveness and disputed-result review.

These are remaining implementation steps, not capabilities completed by this
walkthrough. Additional registry modules can follow the same delivery path.

## Verification

- Complete `scripts/verify.ps1`: passed with Go 1.25.13 selected through the
  existing bundled toolchain. Aggregate backend coverage: 80.8%.
- Python: 102 AI-library tests and 17 validator tests passed; one local symlink
  test is skipped by the Windows host. Frontend: 46 tests, typecheck and Vite
  production build passed. Local Python is 3.13; CI supplies canonical 3.12.
- Browser checks use a 390 × 844 phone viewport: direct entry, person review
  acknowledgement, weapon three-observation review, tool exclusion, face
  prerequisite blocking and mobile width. Hosted preview and CI checks are
  reported with the pull request after publication.
- No Go, Python-service, database, API or runtime configuration is changed.
  T-0431 is reconciled as complete because PR 169 is verified merged.
