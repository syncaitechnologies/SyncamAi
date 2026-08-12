# SyncCam AI — Engineering Execution Roadmap

**Version:** v1.0 · **Date:** 2026-08-01 · **Author:** CTO / Engineering Director
**Status:** Draft for Review

This roadmap is the single execution source of truth for engineering. It derives from, and is subordinate to, the architecture documents in this directory. Where any conflict exists, the architecture decision logs (AD-xx, OD-xx, SD-xx) prevail.

- PRD-SyncCam-AI.md (scope, personas, success metrics)
- ARCHITECTURE.md (system architecture, service catalog, SLOs)
- AI-ARCHITECTURE.md (AI modules, model registry, hardware tiers)
- BACKEND-ARCHITECTURE-SyncCam-AI.md (DB schemas, APIs, events, scaling)
- DEVOPS-MLOPS-SyncCam-AI.md (CI/CD, MLOps, cost model)
- SECURITY-PRIVACY-COMPLIANCE-AI-GOVERNANCE.md (security controls, AI governance)
- UX-DESIGN-SyncCam-AI.md (screens, flows, design acceptance)
- BUSINESS-MODEL-STRATEGY-SyncCam-AI.md (pricing, GTM, team ramp)

**Calendar reference (assumes kickoff 2026-08-03):**
- Weeks 1–12 (2026-08-03 → 2026-10-23): **MVP** → private beta
- Weeks 13–24 (2026-10-26 → 2027-01-15): **Production** → GA
- Weeks 25–52 (2027-01-18 → 2027-07-30): **Enterprise** (Phase 2/3, SOC 2, multi-region)

**Pacing:** 26 two-week sprints. Engineering capacity assumption: 6 FTE engineers (2 founders, 2 platform, 1 AI, 1 frontend) during MVP, growing per §8.

---

## §1 Repository Structure

**Decision: MONOREPO** — single repository `syncamai/`.

**Rationale (vs multi-repo):**
1. **Team scale.** 6–7 engineering FTE at GA, ~23 total at month 12. 20+ services in multi-repo would cost 20+ CI pipelines, 20+ issue trackers, and cross-repo contract churn we cannot absorb.
2. **Atomic cross-service changes.** One commit can change an event schema in `shared/`, a producer in `ai-services/`, and a consumer in `backend/`. Multi-repo forces version-pinning dance on every contract change.
3. **Single CI/CD story.** One pipeline matrix builds, tests, scans, and deploys everything; ArgoCD syncs one source of truth.
4. **Shared contracts.** OpenAPI specs, Avro schemas, and generated SDKs live in `shared/` and are consumed by all services without publish/version rituals.
5. **Path-based ownership.** CODEOWNERS + branch protection can still scope ownership per directory; multi-repo isolation is not needed at this scale.

**Countermeasures for known monorepo drawbacks:** CI caching (GitHub Actions cache + BuildKit cache mounts), path-based CI triggers, CODEOWNERS mandatory review, `Makefile` per directory for local dev.

### Folder layout

```
syncamai/
├── frontend/                      # Web PWA (React 18 + TypeScript + Vite)
│   ├── apps/web/                  #   the main PWA app
│   ├── packages/ui/               #   shared component library (design system)
│   ├── packages/api-client/       #   generated GraphQL/REST client
│   └── packages/config/           #   eslint/tsconfig/prettier presets
├── mobile-app/                    # Flutter (Phase 2 — S16+; scaffold-only placeholder now)
│   ├── app/                       #   main app (iOS/Android)
│   └── packages/                  #   shared Dart packages
├── backend/                       # Go platform services
│   ├── api-gateway/               #   Kong declarative config (routes, plugins, rate limits)
│   ├── realtime-gw/               #   WebSocket gateway (Go)
│   ├── bff/                       #   GraphQL BFF (Apollo Federation, read-only)
│   ├── services/                  #   one directory per service, each a deployable binary
│   │   ├── identity-svc/          #   auth, users, sessions
│   │   ├── tenant-svc/            #   orgs, sites, tenancy, RBAC policies
│   │   ├── config-svc/            #   camera/site/zone/mask/ROI configuration
│   │   ├── device-svc/            #   camera registry, onboarding, claims, firmware metadata
│   │   ├── audit-svc/             #   append-only audit log + daily hash chain
│   │   ├── event-svc/             #   event ingestion, persistence, dedupe
│   │   ├── alert-svc/             #   alert rules, generation, severity, escalation
│   │   ├── notify-svc/            #   email/SMS/push/webhook dispatch + preferences
│   │   ├── analytics-svc/         #   incidents, aggregation, dashboards data
│   │   ├── report-svc/            #   incident dossiers, PDF/CSV export, scheduled reports
│   │   ├── playback-svc/          #   clip locator, signed URLs, timeline
│   │   ├── search-svc/            #   OpenSearch indexing + query
│   │   ├── integration-svc/       #   webhooks, API keys, third-party integrations
│   │   ├── model-registry-svc/    #   model registry metadata + promotion workflow
│   │   ├── training-svc/          #   training orchestration (SageMaker jobs)
│   │   ├── eval-svc/              #   model evaluation gates (accuracy, bias, license, adversarial)
│   │   └── billing-svc/           #   plans A–E, metering, invoices (S7+)
│   └── shared/                    #   Go internal libs: ctx helpers, telemetry, kafka, db, idempotency
├── ai-services/                   # Python AI services (detection, face, ingestion)
│   ├── ingestion-svc/             #   frame/track ingestion from edge, sampling, ROI gating
│   ├── detection-svc/             #   shared backbone + specialist heads runtime
│   ├── face-svc/                  #   ArcFace embeddings, matching, liveness
│   ├── behavior-svc/              #   fall/fight/loitering/crowd temporal modules
│   └── shared/                    #   Python libs: model loading, tensorrt wrappers, eval utils
├── edge/                          # Edge agent (Python) + model runtime packaging
│   ├── agent/                     #   agent: RTSP ingest, store-and-forward, telemetry, OTA client
│   ├── runners/                   #   TensorRT/Triton runner packaging per device tier
│   ├── images/                    #   Jetson Orin NX/AGX + x86/RTX image definitions
│   └── scripts/                   #   flashing, onboarding, bench scripts
├── shared/                        # Cross-cutting contracts (language-agnostic)
│   ├── contracts/                 #   OpenAPI 3.1 specs (REST), GraphQL schema, Avro event schemas
│   ├── protos/                    #   (reserved for future gRPC; not used in MVP)
│   └── sdk/                       #   Generated SDKs (TypeScript, Go, Python)
├── infrastructure/                # IaC + GitOps (Terraform/Terragrunt + ArgoCD)
│   ├── terragrunt/                #   environments: dev / staging / prod-{ap-south-1,us-east-1,eu-central-1}
│   ├── modules/                   #   reusable TF modules (eks, vpc, kms, msk, s3, rds, ...)
│   ├── argo/                      #   Application manifests + App-of-Apps
│   ├── helm/                      #   charts for platform services
│   └── scripts/                   #   bootstrap, DR drills, DB migration runners
├── mlops/                         # Training & evaluation pipelines
│   ├── training/                  #   SageMaker pipelines, dataset manifests (DVC)
│   ├── eval/                      #   benchmark harness, datasets (DVC-managed), bias/adversarial suites
│   └── registry/                  #   promotion config, canary/shadow policies
├── docs/                          # ADRs, runbooks, onboarding, API portal sources
└── scripts/                       # repo-level dev tooling (bootstrap, lint-all, generate)
```

**Top-level files:** `README.md`, `AGENTS.md`, `Makefile`, `go.work`, `pnpm-workspace.yaml`, `turbo.json` (frontend builds), `.github/workflows/`, `.gitignore`, `LICENSE`, `CODEOWNERS`.

**CODEOWNERS mapping:**
- `frontend/`, `mobile-app/` → Frontend eng
- `backend/`, `shared/` → Platform eng (2)
- `ai-services/`, `mlops/`, `edge/` → AI eng
- `infrastructure/` → DevOps/MLOps eng
- `docs/` → CTO/PM

---

## §2 Development Order

The build order maximizes *usable capability at every milestone*: each phase ends in a demoable, deployable increment. It follows the dependency spine: **tenancy → devices → events → intelligence → actions**.

| Phase | Content | Sprint span | Exits with |
|---|---|---|---|
| 0. Foundations | Repo, CI, sandbox, telemetry skeleton | S1 | Engineer boots from `README`; green CI on merge |
| 1. Identity & Tenancy | identity-svc, tenant-svc, RBAC, Kong, OPA | S1–S2 | Login as org admin; create users/roles |
| 2. Devices & Events | device-svc, config-svc, audit-svc, Kinesis/MSK/SQS backbone, event-svc | S2–S3 | Camera registers; events flow end-to-end; audit trail records it |
| 3. Edge Agent v1 | RTSP ingest, store-and-forward, health telemetry, OTA client | S3–S4 | Simulated + 1 real camera streams into cloud |
| 4. Intelligence Wave 1 | ingestion pipeline, backbone + weapon/fire-smoke/intrusion/restricted-zone | S4 | Real-time alerts for P0 modules in staging |
| 5. Intelligence Wave 2 | PPE, fall, fight, loitering; face attendance + liveness; alert/notify/analytics services | S5 | Full MVP module set fires alerts |
| 6. Frontend Core | Live view, incident feed/triage, playback, search, reports | S5–S6 | Operator can run daily ops in PWA |
| 7. Integrations & Billing | webhooks, API keys, billing-svc, reports/evidence export | S6–S7 | Customer can self-serve; billing runs |
| 8. Production Hardening | SLO dashboards, load tests, retention/erasure, DR runbooks, security sweeps | S7–S11 | Passes GA checklist; pilot runs |
| 9. GA Launch | onboarding, docs, SOC-2 kickoff, region bootstrap | S12 | GA revenue launch |
| 10. Phase 2 Modules | LPR, vehicles, crowd/occupancy, face search, ReID, mobile apps, integrations | S13–S18 | Upsell modules live |
| 11. Phase 3 + Scale | predictive, autonomous response, gen-AI narratives, marketplace, SOC 2, multi-region | S19–S26 | Enterprise readiness, 10k-cam validated |

**Non-negotiable invariants enforced from Phase 0** (OD-12, P1–P5):
- Edge-first inference; cloud never blocks life-safety alerting (store-and-forward).
- 5–10 FPS sampling, ROI gating, cheap→expensive cascade.
- Privacy masks applied pre-encode at edge; embeddings-only default for faces.
- Hash-chained audit and evidence; erasure by tenant config.
- Dedupe keys on all events; at-least-once + idempotent consumers.

---

## §3 Sprint Planning

**Format:** 26 two-week sprints across three horizons: MVP (weeks 1–12), Production (weeks 13–24), Enterprise (weeks 25–52). Each sprint lists objectives, deliverables, dependencies, acceptance criteria, and top risks. Tasks referenced by ID map to §10.

---

### HORIZON 1 — MVP (Weeks 1–12) → Private Beta

#### Sprint 1 — Foundations & Identity (Weeks 1–2)

- **Objective:** Standing, secure build system and the identity/tenancy spine. No feature work without CI.
- **Deliverables:** Monorepo + CI matrix (T-0201…); identity-svc with Cognito OIDC (T-0001–T-0002); tenant-svc org/site model (T-0004); RBAC roles/permissions (T-0005, T-0101); Kong gateway with routes, JWT auth, rate limits (T-0006); sandbox + dev environments (T-0202–T-0203); ADR process seeded (T-0271); **D6 AGPL decision executed (T-0231 — Week 1, revenue blocker).**
- **Dependencies:** None (greenfield). Requires AWS sandbox account + admin access (Week 0).
- **Acceptance criteria:** New engineer can clone → `make bootstrap` → `make test` → login as org admin on sandbox with no manual steps; CI blocks merge on failed lint/test/scan; OIDC login returns scoped JWT; tenant creation provisions isolated keyspace; AGPL verdict recorded as ADR-001.
- **Key risks:** AWS account provisioning delay → parallelize with local dev via docker-compose; AGPL decision delay → legal task is P0, decision by end of Week 1 regardless of direction.

#### Sprint 2 — Tenancy, Devices & Event Spine (Weeks 3–4)

- **Objective:** Camera lifecycle + first event flowing through the backbone.
- **Deliverables:** device-svc (register, claim, activation tokens) (T-0008–T-0010); config-svc (site, camera, zone, mask config) (T-0007); audit-svc append-only + hash chain (T-0011, T-0133); event backbone: Kinesis + MSK + SQS/SNS provisioned (T-0209–T-0211), producer/consumer Go lib (T-0055–T-0056); Avro schema registry (T-0055); OPA authz policies (T-0042); preview + staging environments (T-0204–T-0205); user management API (T-0068–T-0069); frontend login + org onboarding shell (T-0124–T-0126).
- **Dependencies:** S1 (identity, Kong). Cloud infra: EKS, VPC, KMS (T-0206–T-0208).
- **Acceptance criteria:** Camera with valid claim token appears in org; config pushed to a mock edge agent; an emitted test event lands in event-svc via Kinesis→MSK consumer; audit log records all mutations; OPA denies cross-tenant reads (verified by test).
- **Key risks:** Backbone latency misconfiguration → measured in CI test with alert if p95 > 2s; scope creep on config-svc → freeze to camera/site/zone/mask/ROI only.

#### Sprint 3 — Edge Agent v1 (Weeks 5–6)

- **Objective:** Real camera streams reach the cloud; agent is self-healing.
- **Deliverables:** edge agent: RTSP ingest, H.264/H.265 decode (T-0161–T-0162, T-0176); store-and-forward with disk quota + eviction (T-0163, T-0174); health telemetry (T-0164); config sync from config-svc (T-0165); time sync + power-loss recovery (T-0168–T-0169); device certs (T-0171); event-svc persistence + dedupe (T-0012, T-0054); alert-svc skeleton (T-0013); OTel collector + Prometheus + Sentry (T-0212–T-0215); container registry with Cosign + SBOM + Trivy (T-0224–T-0226).
- **Dependencies:** S2 (device registry, config-svc, backbone).
- **Acceptance criteria:** Jetson Orin NX dev kit + 1 IP camera stream continuously for 48h with ≤2% dropped frames on LAN; disconnect 10 min → reconnect → no data loss (store-and-forward verified); agent telemetry visible in Grafana; signed image scan-gated in CI.
- **Key risks:** RTSP vendor quirks → abstract via ffmpeg-rtsp wrapper tested against 3 mock sources; flash/OTA complexity → defer OTA to S6, manual flash for beta.

#### Sprint 4 — Intelligence Wave 1 (Weeks 7–8)

- **Objective:** P0 life-safety modules produce real alerts.
- **Deliverables:** ingestion-svc (sampling, ROI gating) (T-0121–T-0122); detection backbone training + TensorRT INT8 conversion (T-0123, T-0141, T-0147); weapon, fire/smoke, intrusion/restricted-zone models (T-0142–T-0144); temporal confirmation engine (T-0151); alert-svc rule engine + severity model (T-0013, T-0061); notify-svc email/SMS/push/webhook dispatch (T-0014–T-0015, T-0059–T-0060); mask/ROI config honored at edge (T-0155); eval datasets + DVC (T-0154, T-0156–T-0157); contract tests (T-0245); RabbitMQ task queues (T-0058).
- **Dependencies:** S3 (agent, event-svc). Requires 1 GPU training box or SageMaker allocation.
- **Acceptance criteria:** Weapon/fire/intrusion alerts on staging from recorded footage with agreed precision/recall (see AI-ARCHITECTURE §3 targets); temporal confirmation suppresses single-frame FPs; notification lands in email + webhook < 10 s p95 after detection; every module ships with eval card.
- **Key risks:** Backbone accuracy below target → gate: no module ships without eval card; GPU contention → queue training jobs overnight, 2 GPUs minimum reserved.

#### Sprint 5 — Intelligence Wave 2 + Alerting UX (Weeks 9–10)

- **Objective:** Full MVP module set; operator-facing live triage.
- **Deliverables:** PPE (4 classes), fall, fight, loitering (T-0145–T-0146, T-0148–T-0149); face stack: detection, ArcFace embeddings, matching, attendance + liveness (T-0150, T-0152, T-0153, T-0082–T-0085); ByteTrack tracker + alert object selection (T-0158–T-0159); masked frame encoding (T-0166); alert acknowledgment workflow + incident assembly (T-0039, T-0041); realtime-gw WebSocket (T-0044); frontend: live view grid, incident feed, triage screen, notification center, dashboards, mask/ROI editor, enrollment flow with consent (T-0127–T-0130, T-0137–T-0138, T-0136); camera health module (T-0119).
- **Dependencies:** S4 (ingestion, backbone, alert/notify).
- **Acceptance criteria:** All 12 MVP modules demonstrated on staging with live cameras; operator triages an incident in < 30 s (UX §7 flow test); face enrollment requires consent screen; attendance report generated for a test site; false alerts ≤ 1 per 5 cameras/day on the demo site.
- **Key risks:** Face accuracy on Indian demographics → early bias eval in this sprint; liveness bypass → adversarial test suite in S10 but basic liveness now; tracker identity swap → ReID deferred to S13 (documented).

#### Sprint 6 — Playback, Search, Reports & Beta Cut (Weeks 11–12)

- **Objective:** Private beta; evidence chain complete.
- **Deliverables:** playback-svc (clip locator, signed URLs, KVS stream metadata, timeline) (T-0019, T-0049, T-0080–T-0081); search-svc (indexing, filters, saved searches) (T-0020–T-0021, T-0062–T-0064); report-svc (dossiers, PDF, evidence package export) (T-0017–T-0018, T-0040, T-0043, T-0065–T-0067); GraphQL BFF read-only (T-0023); webhooks + API keys UI (T-0025, T-0075); OTA via IoT Jobs + Greengrass (T-0177, T-0222–T-0223); prod ap-south-1 bootstrap (T-0218); Playwright E2E core flows (T-0246); IR runbook + on-call basics (T-0239, T-0229); incident dashboard + attendance UI (T-0131–T-0133).
- **Dependencies:** S5.
- **Acceptance criteria:** Private beta (5 pilot sites) goes live; search returns incidents < 1 s p95; evidence dossier exports with hash-verifiable manifest (SD-05 verify API); OTA updates agent on 1 test device with rollback; E2E suite green on staging; GA checklist v1 reviewed with founders.
- **Key risks:** Beta scope creep → hard scope freeze list signed by founders; playback cost (KVS) → verify per-hour cost model before beta.

---

### HORIZON 2 — Production (Weeks 13–24) → GA

#### Sprint 7 — Billing, Onboarding & Pilot Ops (Weeks 13–14)

- **Objective:** Self-serve revenue path; pilot support loop.
- **Deliverables:** billing-svc (plans A–E, metering, invoices, payment gateway Razorpay/Stripe) (T-0027–T-0028, T-0073–T-0075); usage dashboards (T-0074); onboarding wizard + empty states (T-0126, T-0135); model-registry-svc MVP (T-0029); customer onboarding docs + release notes process (T-0264, T-0266); trial/pilot provisioning (T-0076).
- **Dependencies:** S6. Billing requires GST/invoice templates from founders (Week 12 input).
- **Acceptance criteria:** A pilot converts to paid Plan C with invoice generated; metering matches analytics-svc aggregates ±1%; onboarding wizard completes a new site in < 15 min.
- **Key risks:** Payment gateway approval delays → Razorpay primary (India), Stripe fallback wired in parallel; pricing final sign-off is business P0 for this sprint.

#### Sprint 8 — Multi-Tenancy Hardening & Model Ops (Weeks 15–16)

- **Objective:** Tenant isolation proven; model lifecycle automated.
- **Deliverables:** tenant isolation verification suite + OpenSearch multi-tenant indices (T-0043, T-0045); per-tenant KMS hierarchy (T-0030, T-0235); model registry promotion workflow + shadow inference A/B + canary/rollback (T-0086–T-0089); bulk camera import + camera groups (T-0035–T-0036); confidence calibration + FP reduction pass 2 (T-0124, T-0140); bias eval suite (Indian demographics) (T-0157); privacy review of biometric flows (T-0236); blue-green DB migration tooling + canary rollout automation (T-0219–T-0220); runbooks (T-0263); CPU fallback mode on edge (T-0178).
- **Dependencies:** S7.
- **Acceptance criteria:** Cross-tenant access attempts blocked (automated suite, 100% pass); model can be promoted through dev→staging→shadow→canary→prod with one click and instant rollback; DPDP-informed privacy review sign-off recorded.
- **Key risks:** Shadow inference infra cost → cap at 5% of traffic; isolation regressions → isolation suite is a merge-gate CI job from this sprint.

#### Sprint 9 — Scale to 1,000 Cameras & Data Path Maturity (Weeks 17–18)

- **Objective:** Prove the data path at 10× pilot scale.
- **Deliverables:** ClickHouse cold analytics path + TimescaleDB partitioning (T-0031, T-0033); retention/erasure jobs with manifests + verification (T-0032, T-0238); k6 + Locust load tests + synthetic edge fleet (T-0247–T-0249); Karpenter autoscaling + cost monitoring (T-0227–T-0228); backup/restore validation (T-0230); saved searches, report templates, scheduled reports, CSV export (T-0065–T-0067); credential rotation on edge (T-0172); embedding storage isolation (T-0084); dark mode, offline/cache (T-0139, T-0143); API keys UI (T-0075).
- **Dependencies:** S8.
- **Acceptance criteria:** 1,000 synthetic cameras generate ≥ 1,000 ev/s sustained for 4h with SLOs held (99.9% availability, ≤3 s detection p95); retention job erases per-tenant data and manifest verifies; restore drill from backup succeeds < RTO 60 min.
- **Key risks:** ClickHouse ingestion tuning → spike-protocol load test in sprint; cost blowout → cost dashboard reviewed weekly (OD-12 invariants enforced in tests).

#### Sprint 10 — Security Sweep, Pen Test & Compliance Packs (Weeks 19–20)

- **Objective:** Release-blocking security bar passed.
- **Deliverables:** DAST on staging + pen test round 1 (T-0233–T-0234); adversarial ML suite + eval gate on prod models (T-0159, T-0157); SAST/DAST remediation (T-0232); India DPDP compliance pack (T-0237); audit verify API + audit log viewer (T-0034, T-0079); IR drill (T-0240); accessibility pass + regression suite automation (T-0144, T-0251); API versioning policy (T-0048); invoice/usage dashboards polish (T-0074); secure boot/attestation on edge (T-0173) + edge firewall config (T-0179).
- **Dependencies:** S9.
- **Acceptance criteria:** Pen test report closed with 0 critical/high open at GA; adversarial tests pass eval gate; DPDP checklist signed by compliance lead; IR drill completed with postmortem.
- **Key risks:** Pen test findings push GA → buffer: fix window S11, criticals block GA by policy; adversarial evals reveal model weaknesses → mitigation task added to S11 backlog.

#### Sprint 11 — Observability Maturity & DR (Weeks 21–22)

- **Objective:** Operate it like production; prove recovery.
- **Deliverables:** Loki/Tempo/Thanos full stack + SLO dashboards (T-0216–T-0217); alert escalation policies (T-0040); on-call rotation + runbooks complete (T-0229, T-0263); vulnerability management cadence (T-0242); per-tenant rate limits (T-0047); DR drill 1 (failover ap-south-1→ap-southeast-1) (T-0221); incident response automation (T-0239).
- **Dependencies:** S10.
- **Acceptance criteria:** SLO dashboard shows 99.9% availability trend; DR drill restores read path < 60 min RTO, data loss ≤ 5 min RPO; on-call responds to synthetic page < 15 min.
- **Key risks:** DR drill reveals gaps → runbook fixes must land before S12; SLO burn alerts tune with 2 weeks of real traffic.

#### Sprint 12 — GA Launch (Weeks 23–24)

- **Objective:** General availability.
- **Deliverables:** GA readiness checklist sign-off (DEVOPS §11) (T-0265); GraphQL federation full rollout (T-0046); SOC 2 prep kickoff (controls inventory) (T-0241); marketing site content + training material (T-0269–T-0270); API docs portal (T-0265); multi-region IaC groundwork for us-east-1 (T-0218, T-0231); GA announcement + onboarding demo day.
- **Dependencies:** S11.
- **Acceptance criteria:** GA launch executes with 25+ pilot/prospect conversations active (business); no P0/P1 defects open; SOC 2 Type I audit scheduled; docs portal live.
- **Key risks:** Feature debt slips → freeze list enforced; launch coincides with Indian festival season (Oct–Nov) → ops staffing plan.

---

### HORIZON 3 — Enterprise (Weeks 25–52) → Phase 2/3, SOC 2, Multi-Region

#### Sprint 13 — Phase 2 Kickoff: LPR, Face Search, ReID (Weeks 25–26)

- **Objective:** First upsell modules in alpha.
- **Deliverables:** LPR module (T-0167); ReID at handoff (T-0158); face search (T-0168); i18n en-IN (T-0145); partner/MSP account scaffolding (T-0077); AGX Orin + RTX 4000 tier support (T-0175, T-0180); SSO/SAML (T-0072); third-party risk review (T-0243).
- **Dependencies:** S12; hardware procurement (founders, Week 20 order).
- **Acceptance criteria:** LPR reads 3 test plates sets (IN formats) at ≥ 92% char accuracy in staging; face search returns matches < 2 s p95; ReID improves track continuity metric ≥ 15% on benchmark.
- **Key risks:** LPR accuracy on Indian plates → dataset acquisition task added; hardware lead times → ordered at Week 20, delay buffer 4 weeks.

#### Sprint 14 — Vehicle Suite (Weeks 27–28)

- **Objective:** Vehicle intelligence modules.
- **Deliverables:** vehicle detection/classification (T-0169); speed estimation (T-0170); parking occupancy (T-0171); traffic counting (T-0172).
- **Dependencies:** S13.
- **Acceptance criteria:** Speed within ±5 km/h of ground truth on test clips; parking accuracy ≥ 90%; traffic counts within 5% of manual count.
- **Key risks:** Camera angle variance → calibration guide + acceptance clips per mounting type.

#### Sprint 15 — Crowd & Occupancy (Weeks 29–30)

- **Objective:** Density analytics for retail/hospital personas.
- **Deliverables:** crowd density (T-0173); people counting/occupancy (T-0174); occupancy dashboard widgets (T-0131).
- **Dependencies:** S14.
- **Acceptance criteria:** Occupancy counts ±10% of manual counts at ≤ 30 people/frame; density heatmap renders in dashboard.
- **Key risks:** Overcrowding accuracy → scope alerting to coarse bands initially.

#### Sprint 16 — Mobile Apps (Weeks 31–32)

- **Objective:** Native apps for responders.
- **Deliverables:** Flutter app scaffold + auth + live view + alert push (T-0146); app stores + signing (T-0146); mobile E2E (T-0252).
- **Dependencies:** S15; App Store/Play accounts created at Week 28.
- **Acceptance criteria:** Alert push → app open → live view < 5 s on Android + iOS; store review passes both.
- **Key risks:** Store review cycles → submit 1 week early in beta lane.

#### Sprint 17 — Integrations Depth (Weeks 33–34)

- **Objective:** Sell-through integrations.
- **Deliverables:** ONVIF Profile S/T depth, Hikvision/Dahua adapter pack, PTZ control API (T-0050, T-0183); generic object classification (T-0184); access control integration (T-0182).
- **Dependencies:** S16.
- **Acceptance criteria:** PTZ preset recall ≤ 1 s; 3 target vendor NVRs onboarded by integrations partner.
- **Key risks:** Vendor API churn → adapter layer isolated, contract tests per vendor.

#### Sprint 18 — Predictive & Compliance Packs (Weeks 35–36)

- **Objective:** Predictive value + market compliance.
- **Deliverables:** predictive capacity planning (T-0185); theft-risk scoring (T-0186); GDPR pack + HIPAA readiness review (T-0245, T-0246); billing for channel/integrator (T-0090).
- **Dependencies:** S17.
- **Acceptance criteria:** Capacity model forecasts ±15% on 2 pilot datasets; GDPR/HIPAA review reports issued with remediation list.
- **Key risks:** Model overpromise → all predictive outputs labeled as advisory.

#### Sprint 19 — Cross-Site & Organization Scale (Weeks 37–38)

- **Objective:** Multi-site enterprise tenancy.
- **Deliverables:** cross-site org model + consolidated dashboards (T-0078, T-0187); site hierarchy in config-svc (T-0188).
- **Dependencies:** S18.
- **Acceptance criteria:** 1 org, 5 sites, consolidated view with per-site drill-down; RBAC scopes to site.
- **Key risks:** Tenancy regression → isolation suite extended to org hierarchy.

#### Sprint 20 — Autonomous Response (Weeks 39–40)

- **Objective:** Act on alerts, not just notify.
- **Deliverables:** response policies (PTZ track, siren, lights) (T-0189, T-0190); guard app actions (T-0191); behavior anomaly baseline (T-0192).
- **Dependencies:** S19. Requires hardware accessory list + safety review with legal.
- **Acceptance criteria:** Policy triggers accessory within 2 s of alert; every autonomous action logged to audit with human override path.
- **Key risks:** Liability → safety review gate, kill-switch per site, actions default-off.

#### Sprint 21 — Gen-AI Narratives & Copilot (Weeks 41–42)

- **Objective:** Gen-AI value layer (Phase 3).
- **Deliverables:** incident narrative generation (T-0193); search copilot (T-0194); narrative grounding + evaluation harness (T-0195).
- **Dependencies:** S20. LLM provider (bedrock) approved by security (T-0247).
- **Acceptance criteria:** Narratives are fact-only (verified against event data, hallucination < 2% on eval set); copilot answers scoped to tenant data only.
- **Key risks:** Hallucination/leakage → grounding eval gate is release blocker; PII redaction tests.

#### Sprint 22 — Marketplace & Plugin Framework (Weeks 43–44)

- **Objective:** Ecosystem enablement (Phase 3).
- **Deliverables:** plugin framework + marketplace storefront (T-0196–T-0197); partner onboarding flow (T-0198).
- **Dependencies:** S21.
- **Acceptance criteria:** 1 internal + 1 partner plugin published and installable per tenant.
- **Key risks:** Security boundary of plugins → sandboxed execution, review process.

#### Sprint 23 — SOC 2 Type I & Pen Test Round 2 (Weeks 45–46)

- **Objective:** Certifiable security posture.
- **Deliverables:** SOC 2 Type I audit evidence collection + audit (T-0244, T-0234); pen test round 2; vulnerability closure.
- **Dependencies:** S22.
- **Acceptance criteria:** SOC 2 Type I report issued with 0 exceptions or exceptions remediated within sprint; pen test 2 closes all high/critical.
- **Key risks:** Evidence gaps → weekly compliance sync during sprint.

#### Sprint 24 — DR Drill 2 & EU Region (Weeks 47–48)

- **Objective:** Multi-region production resilience.
- **Deliverables:** eu-central-1 rollout (T-0231); DR drill 2 cross-region failover (T-0221); region scale validation 10k cams (T-0250).
- **Dependencies:** S23.
- **Acceptance criteria:** eu-central-1 serves traffic with data pinned to region; failover drill meets RTO/RPO; 10k-cam synthetic validation passes SLOs.
- **Key risks:** Region sync latency → async replication validated; cost per region tracked.

#### Sprint 25 — Chaos & Reliability Engineering (Weeks 49–50)

- **Objective:** Antifragile operations.
- **Deliverables:** chaos test suite (network, node, DB failover) (T-0253); performance budget checks (T-0255); 10k-cam hardening fixes.
- **Dependencies:** S24.
- **Acceptance criteria:** Chaos game-days pass with SLO burn < 5%; no new P1 defects in 2 weeks.
- **Key risks:** Blast radius → chaos scoped to staging + dedicated dev pool.

#### Sprint 26 — Enterprise GA (Weeks 51–52)

- **Objective:** Enterprise feature completeness + scale readiness.
- **Deliverables:** feature freeze review vs Phase 3 scope (T-0268); training for customer success + partners (T-0269); final SLO report; enterprise launch plan.
- **Dependencies:** S25.
- **Acceptance criteria:** Enterprise GA checklist (business-owned) 100%; SOC 2 Type I in hand; 10k-cam unit economics validated vs $3.5–7/cam blended target.
- **Key risks:** Scope overhang → Phase 3 remainder moves to post-52-week backlog explicitly.

---

## §4 Git Workflow

**Decision: TRUNK-BASED DEVELOPMENT** (mandated by DEVOPS-MLOPS OD-04). Not Git Flow.

**Why not Git Flow:** Git Flow's long-lived `develop` + release branches presume infrequent, version-pinned releases. We deploy continuously via ArgoCD canaries (OD-01); a release branch model would double merge overhead per deploy, delay security fixes, and fight the trunk-first model-cadence required by MLOps.

### Rules

| Rule | Specification |
|---|---|
| Default branch | `main` — always deployable; CI green required |
| Feature branches | Short-lived (`feat/xyz`), from `main`, merged ≤ 2 days old; long-running branches banned |
| Hotfixes | `fix/xyz` off `main`; deploy immediately, then revert-first if escalation needed |
| Model releases | Models do **not** ride app releases — separate registry promotion path (OD-11) |
| Merge | Squash-merge with conventional commit message; PRs require 1 approval (2 for `backend/`, `infrastructure/`) |
| Branch protection | `main`: require CI, 1-2 approvals, CODEOWNERS review, no direct push |
| Tags | `vX.Y.Z` semver on `main` at each GA/hotfix; changelog generated from conventional commits |

**Commit message format (Conventional Commits):**
```
type(scope): summary
```
Types: `feat` `fix` `perf` `refactor` `test` `docs` `chore` `ci` `infra`. Breaking changes: `feat!: ...`. Scopes: `identity`, `device`, `event`, `alert`, `notify`, `edge-agent`, `detection`, `face`, `frontend`, `infra`, `mlops`, `contracts`, `billing`, `security`.

**CI gates on every PR (ordered, cheapest first — DEVOPS §5):** `lint → unit → build → scan (gitleaks, semgrep, Trivy) → integration (Testcontainers) → contract → deploy preview`. AI/ML changes additionally run: `dataset-validate → eval-smoke (golden set, < 10% runtime) → license check`.

**Release branch policy:** none in normal ops. Only post-GA `release/x.y` branch for a < 2-week stabilization window when a customer SLA requires it; hotfixes still merge to `main` first.

**Bot/automation:** Dependabot weekly (grouped, auto-PR), Renovate for infra modules, auto-label, stale-branch cleanup at 14 days.

---

## §5 Coding Standards

### 5.1 Language & Framework Baselines

| Stack | Version | Style enforcers | Notes |
|---|---|---|---|
| Go (platform) | 1.22+ | `golangci-lint` (govet, staticcheck, errcheck, revive), `gofmt` | Effective Go; no global state; errors wrapped with context |
| Python (AI/edge) | 3.11 | `ruff` (lint+format), `mypy --strict` | Type hints mandatory on public API; `dataclasses`/`pydantic` for config |
| TypeScript (web) | 5.x strict | ESLint (typescript-eslint, react-hooks), Prettier | `noImplicitAny`, `strict: true`; no `any` escape hatches without review |
| Dart (mobile) | Flutter 3.x | `flutter analyze`, `dart format` | Phase 2; same CI pattern |
| SQL | PostgreSQL 16 | `pg_format`, review checklist | See 5.5 |
| Terraform | 1.8+ | `tflint`, `terragrunt validate`, `checkov` | Modules pinned; no mutable resources without drift plan |

### 5.2 Repository & Code Organization

- One module per directory; `backend/services/<svc>` = one deployable; internal libs in `backend/shared/` (no cross-service imports outside shared).
- Python: package-per-service under `ai-services/<svc>`; shared in `ai-services/shared/`.
- No duplicate contracts: OpenAPI spec in `shared/contracts/` is the single source; generated SDKs committed via CI job.
- Max file length: 400 lines (Go/Python), 250 lines (TSX components) — enforced in review, flagged by lint where possible.
- No comments that restate code; docstrings/ADRs explain *why*.

### 5.3 API & Event Standards

- **REST:** OpenAPI 3.1, `/api/v1/...` under Kong; JSON `camelCase`; pagination via `cursor`; errors: RFC 7807 problem+json with `trace_id`.
- **Idempotency:** all POST/PUT accept `Idempotency-Key` header; server-side `dedupe_key` on every event (ARCHITECTURE §4.2).
- **Events:** Avro schemas in `shared/contracts/events/`, versioned `name.version`; backwards-compatible evolution only (additive fields); schema registry enforces compatibility.
- **GraphQL BFF:** read-only (no mutations through BFF); resolvers call REST services; depth limiting + per-tenant data scoping enforced.
- **Naming:** services `*-svc`; topics `sentinel.<domain>.<event>`; DLQs `sentinel.<domain>.<event>.dlq`; S3 buckets per BACKEND §11 exactly.

### 5.4 Database Standards

- `snake_case`, plural table names, `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`; `created_at/updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`; soft deletes banned (use audit + retention jobs).
- All tenant-owned rows carry `tenant_id`; composite indexes ordered `(tenant_id, ...)`.
- JSONB only for flexible metadata; every JSONB column documented in BACKEND §3.
- Migrations: numbered files in `migrations/` per service, forward-only, reviewed; destructive ops require two-step (deploy-safe) pattern.
- Partitioning: TimescaleDB hypertables per BACKEND §3; new tables ≥ 10M rows/yr must be hypertables or partitioned by month.

### 5.5 Env Vars, Config & Secrets

- 12-factor: config via env, no config files in repo for credentials; services fail-fast on missing required vars.
- All secrets: AWS Secrets Manager, synced by `external-secrets` into K8s; **never** commit keys/tokens/certs; `.env.example` committed with no values.
- `gitleaks` pre-commit hook + CI gate (mandatory).
- Per-tenant secrets: KMS hierarchy per SD-04; no tenant material in shared namespaces.

### 5.6 Logging, Metrics, Tracing

- Structured JSON logs: `trace_id`, `span_id`, `tenant_id`, `service`, `version`, `level`; no free-form strings.
- OpenTelemetry everywhere: traces for all external calls (SDK-wrapped), metrics for RED (request rate/errors/duration) + SLO signals.
- Log levels: `debug` dev, `info` default, `warn` anomalies, `error` only for actionable failures; **no sensitive PII in logs** (mask device IDs acceptable, faces/plate values never).
- Central schema enforced by a shared logging lib in `backend/shared/` and `ai-services/shared/`.

### 5.7 AI/ML Code Standards

- Model code is versioned + reproducible: fixed seed, pinned deps (uv/poetry lockfiles), dataset manifest via DVC.
- Every module ships: eval card (metrics per AI-ARCHITECTURE §3), model card (limitations, training data provenance, license), benchmark artifact.
- No model merged without eval-gate pass (eval-svc): accuracy, precision/recall, bias slice, adversarial, license check (includes AGPL D6).
- TensorRT INT8 conversion must be reproducible from a pinned source ONNX artifact + calibration dataset hash.

### 5.8 Frontend Standards

- React 18 + TS strict + Vite; component library in `packages/ui` (no duplicated components across pages).
- Accessible by default: semantic HTML, focus management, WCAG 2.2 AA target (UX §10); `axe` in CI.
- State: server cache via Apollo; local UI state via hooks; no global store without ADR.
- i18n via `react-i18next` from S13 (en-IN default); strings never hardcoded post-i18n.

### 5.9 Documentation & Process

- ADRs in `docs/adr/` for every architecture-affecting decision (AD-xx/OD-xx/SD-xx IDs referenced).
- Every service: `README.md` (run, test, deploy), `docs/runbooks/` for ops toil.
- PR description template: what/why/risk/test-evidence; linked task ID.
- Definition of Done: code reviewed, CI green (all gates incl. scans + eval), tests ≥ 80% coverage on changed lines (new code), docs touched if API/behavior changed, telemetry wired, changelog entry.

## §6 Testing Strategy

**Goal:** Ship with confidence that (a) the system meets SLOs, (b) models meet their eval cards, (c) security regressions are caught in CI — without a QA team until GA (QA embedded per §8).

### 6.1 Test Pyramid (per layer)

| Layer | Tooling | Scope | Gate |
|---|---|---|---|
| Unit | Go: `testing` + `testify`; Python: `pytest`; TS: `vitest` | Pure logic, handlers with fakes | ≥ 80% line coverage on changed code; mandatory for all services |
| Integration | Testcontainers (Postgres, Redis, Kafka/Redpanda, MinIO, OpenSearch, ClickHouse) | Cross-service contract of a service against real infra | Every PR touching backend/ai-services |
| Contract | Pact-style + OpenAPI schema validation (schema registry check) | Provider-consumer compatibility for event/API contracts | Contract CI job; schema evolution check |
| E2E | Playwright (web), integration-tested Flutter (mobile) | Critical paths: login → camera on → alert → triage → export | On staging nightly + pre-release |
| Performance | k6 (API), Locust (event ingest), synthetic edge fleet (OD-13) | SLO: 1,000 ev/s, ≤3 s detection p95, alert ≤10 s p95 | Pre-GA + every scale milestone (S9, S24) |
| Security | gitleaks (secrets), semgrep (SAST), Trivy (deps/images), OWASP ZAP (DAST), external pen tests | All branches | CI gates + quarterly + round 1 S10, round 2 S23 |
| AI/ML eval | eval-svc + benchmark suites (DVC-versioned datasets) | Accuracy, precision/recall, bias slices (Indian demographics), adversarial, license | Mandatory before any model promotion |
| Chaos | ChaosMesh / litmus + scripted drills | DB failover, network partition, node loss | Game-day quarterly; S25 suite |

### 6.2 AI Model Eval Gates (non-negotiable, per AI-ARCHITECTURE §5 + OD-02/03)

Every candidate model must pass, in order, before promotion:
1. **Data gate:** dataset manifest pinned (DVC), labeling QA sampled.
2. **Benchmark gate:** target metrics from AI-ARCHITECTURE §3 (e.g., weapon AP ≥ 0.85, face ≥ 99% TAR @ FAR 1e-5 where specified) on held-out set.
3. **Bias gate:** per-segment metrics across skin-tone/age/gender/region slices (India-first); no segment below 90% of aggregate.
4. **Adversarial gate:** occlusion, lighting, adversarial patch suite (from S10).
5. **License gate:** model + training-data license check (AGPL D6 resolved at S1; CI enforces allowlist).
6. **Shadow gate:** live shadow traffic 1–2 weeks vs incumbent; decision metric per module (e.g., FP-rate delta).
7. **Canary gate:** 5% → 50% → 100% with auto-rollback on SLO breach (OD-01, OD-11).

### 6.3 Performance & Load Validation

- **Synthetic edge fleet (OD-13):** emulated agents generating realistic RTSP/event streams; used for 1,000-cam (S9) and 10,000-cam (S24) validation.
- **k6:** API SLO suites (p95 latency budgets from ARCHITECTURE §15) run in CI on preview env + nightly on staging.
- **Locust:** event ingest at 2× peak (2,000 ev/s) with spike protocol; validates Kinesis→MSK→consumers and ClickHouse backpressure.
- **Cost invariants (OD-12) asserted in tests:** sampling rate caps, ROI gating, cascade ordering — regression tests fail if a code path skips gating.

### 6.4 Security Testing Cadence

- CI (every PR): gitleaks, semgrep (critical/high only), Trivy (CRITICAL/HIGH fail), license check.
- Nightly staging: ZAP DAST against staging + OpenSearch/Redis auth checks.
- Quarterly: external pen test (round 1 S10, round 2 S23); IR drill (S10); adversarial ML suite re-run.
- Data plane: encryption-at-rest/transit assertions (TLS 1.3, KMS per-tenant) as integration tests (SD-01…04).

### 6.5 Test Data & Environments

- **Test data:** DVC-managed curated clips (real + synthetic), tagged by scenario; no production customer data in dev/test; synthetic PII generator for enrollment flows.
- **Environments:** dev (per-engineer, docker-compose), preview (per-PR ephemeral), staging (nightly full-suite target), prod (canary).
- **Flakiness policy:** flaky test quarantined in < 24 h; CI must be green-or-blocked; test time budget per PR < 15 min.

---

## §7 Deployment Roadmap

### 7.1 Environments

| Env | Provisioning | Purpose | Data |
|---|---|---|---|
| `dev` | docker-compose + localstack | engineer iteration | Synthetic |
| `preview` | ephemeral per-PR via GitHub Actions + ArgoCD | contract/E2E smoke | Synthetic |
| `staging` | Terragrunt `staging` | nightly full suite, evals, pen test | Synthetic + recorded footage |
| `prod-ap-south-1` | Terragrunt prod | India GA (S12) | Customer (pinned) |
| `prod-us-east-1` | Terragrunt prod | US GTM (post-GA, S12 groundwork → S24+) | Customer (pinned) |
| `prod-eu-central-1` | Terragrunt prod | EU compliance (S24) | Customer (pinned) |

### 7.2 Deploy Mechanics (GitOps, OD-01)

1. Merge to `main` → CI builds multi-arch distroless images, signs with Cosign, generates SBOM, scans with Trivy; pushes to ECR.
2. ArgoCD detects new manifest set (Helm values bumped by CI) → syncs to target env.
3. **App deploy:** canary 5% → 50% → 100% with auto-rollback on SLO/error-budget burn; manual approval at 50% for prod (first 3 months, then auto with runbook).
4. **DB migrations:** two-step (expand → deploy → contract) via blue-green migration runner (S8); never a single destructive step; lock tables windowed off-peak.
5. **Model deploy (decoupled — OD-11):** model-registry-svc promotion → edge/cloud runner config via IoT Jobs + Triton model repo; independent rollback per model; never coupled to app deploys.

### 7.3 Rollout & Rollback

- **Rollback policy:** app = revert manifest (ArgoCD sync-back), < 5 min; model = switch registry pointer to incumbent, < 2 min; data = no rollback, forward-fix + replay from event stream (audit/evidence chain preserved).
- **Feature flags:** LaunchDarkly-style internal flag service (MVP: env-var + config-svc flags) — kill-switch for new modules per tenant.
- **Freeze windows:** no deploys during beta wave days; GA windows Tue–Thu 06:00–14:00 IST.

### 7.4 Multi-Region & DR (AD-02)

- Day 1 regions: `ap-south-1` (primary), `us-east-1` (secondary GTM), `eu-central-1` (S24).
- DR pairs: ap-south-1 ↔ ap-southeast-1 (bootstrap S11 drill), us-east-1 ↔ us-west-2, eu-central-1 ↔ eu-west-2.
- Targets: RTO ≤ 60 min, RPO ≤ 5 min; async replication for event/analytics; synchronous within region for truth stores.
- DR drills: S11 (region failover), S24 (cross-region); runbook-driven, chaos-injected.

### 7.5 Edge Fleet Deployment

- Greengrass v2 + IoT Jobs for OTA (S6 pilot, S7 fleet); signed firmware, staged rollout (5% → 25% → 100% per device group), auto-rollback on health metric regression (OD-11).
- Device certs via IoT Core; credential rotation (S9); secure boot/attestation (S10); agent pinning to config-svc revision.

### 7.6 Production Readiness Gate (DEVOPS §11 checklist)

All items must be evidenced before each milestone: GA (S12), Phase-2 modules (S18), Enterprise GA (S26). Evidence = runbooks, dashboards, drill reports, eval cards, pen test closure.

---

## §8 Team Structure

Sourced from BUSINESS-MODEL-STRATEGY §11. Headcount = founders + direct hires. **Early roles intentionally combined** to keep 6 engineering FTE through MVP; splitting happens at defined triggers (not dates).

### 8.1 Team by Phase

| Role | MVP (W1–12) | Production (W13–24) | Enterprise (W25–52) | Trigger to add |
|---|---|---|---|---|
| Founder/CEO (product) | 1 | 1 | 1 | — |
| Founder/CTO (architecture, MLOps oversight) | 1 | 1 | 1 | — |
| AI Engineer (vision, training) | 2 | 3 | 4 | ≥ 3 model modules in parallel |
| Platform Engineer (Go, backend) | 2 | 2 | 4 | GA scale ops |
| DevOps/MLOps (IaC, CI/CD, edge OTA) | 1 | 1 | 2 | 2nd region bootstrap |
| Frontend Engineer (web, then mobile) | 1 | 1 | 2 | mobile app build (S16) |
| Designer (shared w/ GTM) | 0.5 | 0.5 | 1 | UX backlog density |
| QA Engineer (embedded w/ platform) | 0 | 1 | 2 | GA release quality |
| Security/Compliance Lead (shared) | 0.5 | 1 | 1 | pen test round 1 (S10) |
| Solutions Engineer (customer) | 1 (shares platform) | 1 | 2 | pilot deployments |
| SDRs (sales) | 2 | 2 | 3 | pipeline > 3× close rate |
| Customer Success | 0 | 0.5 | 2 | GA churn monitoring |
| **Total (incl. founders)** | **≈ 12–14** | **≈ 14–16** | **≈ 22–24** | |

### 8.2 Early Role Combinations (explicit, documented)

- **QA** is embedded in platform + AI engineers until S7 (founders run acceptance demos weekly); dedicated QA added at S7 gate.
- **DevOps/MLOps** is one person until 2nd region (S24); CTO covers model CI until then.
- **Solutions Engineer** doubles as support + onboarding; CSM added post-GA.
- **Security/Compliance** is founder-shared until S10; hire before pen test round 1.
- **Designer** is fractional until UX backlog > 20 tickets.

### 8.3 RACI (key artifacts)

| Artifact | R | A | C | I |
|---|---|---|---|---|
| SLOs & error budgets | DevOps/MLOps | CTO | Platform, AI | All |
| Release to prod (canary) | DevOps/MLOps | CTO | QA | All |
| Model promotion | AI Engineer | CTO | eval-svc owner, DevOps | GTM |
| Incident response | On-call | Security Lead | All | CEO |
| Customer data erasure | Platform | Security Lead | Compliance | CSM |
| Pen test remediation | Platform/AI | Security Lead | DevOps | CEO |
| Pricing/billing config | Platform | Founder/CEO | Solutions, CSM | — |

### 8.4 Working Agreements

- Standups daily 10:00 IST; sprint review/retro per sprint; **weekly SLO + cost review** (Thursdays, 30 min) — OD-12 cost invariants reviewed here.
- On-call: 1 primary + 1 shadow (from S7), rotation weekly; page via PagerDuty-class tool; escalation ≤ 15 min.
- Hiring bar: every candidate writes a short code exercise + takes a design interview; first 5 hires owned personally by founders (per business doc).

---

## §9 Risk Register

Consolidated from PRD §13–14, AI-ARCHITECTURE §9, SECURITY §1.4/§6, DEVOPS §9, BUSINESS §12. `P` = probability, `I` = impact (1–5). Owner = accountable role per §8.3.

### 9.1 Technical Risks

| # | Risk | P | I | Mitigation | Owner | First review |
|---|---|---|---|---|---|---|
| T-1 | **Ultralytics YOLO AGPL-3.0 (D6)** — license contaminates SaaS offering | 5 | 5 | Week-1 decision: enterprise license (~$3–5k/yr) OR Apache-2.0 swap (RT-DETR/D-FINE); license allowlist enforced in CI; ADR-001 | CTO | S1 |
| T-2 | Model accuracy below targets on Indian demographics | 4 | 4 | Bias eval suite from S5; early customer footage collection; eval gates block promotion | AI Eng | S5 |
| T-3 | Backbone/head coupling slows module iteration (D1) | 3 | 3 | Shared backbone frozen at major versions; heads own full per-module eval cards | AI Eng | S8 |
| T-4 | 1,000→10,000-cam scale breaks ingest SLOs | 3 | 4 | Synthetic fleet tests at S9/S24; OD-12 invariants regression-tested; Karpenter autoscaling | DevOps | S9 |
| T-5 | Edge OTA bricks fleet / config drift | 3 | 4 | Staged rollout 5→25→100, health-regression auto-rollback, attestation | DevOps | S7 |
| T-6 | Event duplication/loss in at-least-once backbone | 3 | 3 | `dedupe_key` enforcement + idempotent consumers + replay tooling; tested at 2× peak | Platform | S5 |
| T-7 | KVS/playback cost overruns (unit economics §12.1) | 3 | 4 | Per-hour cost model before beta; configurable bitrate/retention; ClickHouse cold path | CTO | S6 |
| T-8 | Camera/vendor RTSP incompatibilities | 3 | 2 | ffmpeg wrapper abstraction; vendor compatibility matrix tested pre-sale | AI Eng | S3 |
| T-9 | TensorRT INT8 precision regression | 2 | 3 | INT8 calibration reproducibility gate; per-device perf benchmark in eval card | AI Eng | S4 |

### 9.2 Business Risks

| # | Risk | P | I | Mitigation | Owner | First review |
|---|---|---|---|---|---|---|
| B-1 | **70% gross margin covenant** (Year 2) | 3 | 5 | OD-12 cost invariants in tests; cost dashboard weekly; pricing floor per plan | Founder/CEO | S7 |
| B-2 | India pricing pressure ($5–12/cam/mo) vs hardware + cloud cost | 4 | 4 | Edge-first inference; blended cost tracked per customer cohort; upsell ladder (LPR, face) | Founder/CEO | S8 |
| B-3 | Slow GTM → $1M ARR/18-mo miss | 3 | 5 | Private beta S6 with 5 pilots; GA S12; sales motion via integrator channel from S13 | Founder/CEO | S6 |
| B-4 | Churn from alert fatigue (≤1 FA/5 cams/day SLO) | 3 | 4 | FP-reduction passes S5/S8; per-tenant threshold tuning; alert quieting UX | AI Eng | S5 |
| B-5 | AGPL legal exposure (beyond T-1) — dataset licenses | 2 | 5 | Dataset provenance registry; license check in eval gate | Security | S4 |
| B-6 | Key-customer overconcentration (1 customer > 40% ARR) | 2 | 3 | Channel diversification; enterprise seat caps | CEO | S13 |

### 9.3 Security & Compliance Risks

| # | Risk | P | I | Mitigation | Owner | First review |
|---|---|---|---|---|---|---|
| S-1 | **Biometric data breach / face embedding leak** | 2 | 5 | Embeddings-only default, field-level AES-256-GCM + per-tenant KMS, no raw face retention (SD-02/04); pen test round 1 | Security | S8 |
| S-2 | Adversarial attack on models (patches, occlusion) | 3 | 4 | Adversarial eval suite S10 + re-runs; dual-detector on high-severity zones | AI Eng | S10 |
| S-3 | Tenant isolation regression | 3 | 5 | Isolation suite as CI merge gate from S8; OPA policy tests; quarterly red-team | Platform | S8 |
| S-4 | Insider misuse of surveillance data | 3 | 4 | Hash-chained audit + WORM evidence, role-scoped access, anomaly alerts on bulk export | Security | S6 |
| S-5 | Regulatory gap (DPDP/GDPR/HIPAA per market) | 3 | 4 | Compliance packs S10/S18; SOC 2 Type I S23; legal review per pack | Security | S10 |
| S-6 | Supply-chain compromise (images/deps) | 2 | 4 | Cosign + SBOM + Trivy gates; dependency allowlist; vendor review S13 | DevOps | S3 |

### 9.4 Operational Risks

| # | Risk | P | I | Mitigation | Owner | First review |
|---|---|---|---|---|---|---|
| O-1 | Region outage exceeds RTO 60 min | 2 | 5 | DR drills S11/S24; multi-region IaC; cross-region failover runbook | DevOps | S11 |
| O-2 | Data loss beyond RPO 5 min | 2 | 5 | Async replication + daily backup restore validation; event replay path | Platform | S9 |
| O-3 | On-call burnout / solo DevOps | 3 | 3 | Shadow on-call from S7; runbook discipline; hire at S24 trigger | CTO | S7 |
| O-4 | Hardware lead times (Jetson/RTX) | 3 | 3 | Orders at Week 20 for S13 tiers; cloud-fallback deployment option (GPU nodes) | CTO | S13 |
| O-5 | Model promotion human-error rollback | 2 | 3 | One-click promote + instant rollback; shadow/canary gates mandatory | DevOps | S8 |

**Top 5 watch-list (reported to founders weekly):** T-1 (until resolved), T-4, B-1, B-2, S-3.

---

## §10 Top 300 Development Tasks

**Legend:** `ID` · Title — `Area` `Priority` `Sprint` `Effort (days)` `Deps` `AC:` acceptance criteria.
- **Areas:** A=Backend/Platform · B=AI/ML · C=Edge · D=Frontend · E=DevOps/Infra · F=Security/Compliance · G=QA/Testing · H=Docs/Ops
- **Priorities:** P0 = blocks private beta · P1 = blocks GA · P2 = enterprise/Phase 2–3
- **Deps:** `—` = none. Effort = calendar days for one engineer at mid-senior level.

### 10.A Backend / Platform (T-0001–T-0090)

- **T-0001** identity-svc scaffold (Go) — A · P0 · S1 · 3–5 d · Deps: — · AC: OIDC login via Cognito returns JWT; refresh rotation; logout revocation; unit tests ≥80%.
- **T-0002** Cognito user pools + app clients (web, edge) — A · P0 · S1 · 2–3 d · Deps: — · AC: Web PKCE + edge device auth flows work in sandbox.
- **T-0003** tenant-svc: org/site model + CRUD — A · P0 · S1 · 3–5 d · Deps: T-0001 · AC: Org→site→camera hierarchy persisted; tenant_id on all rows; soft-delete banned.
- **T-0004** RBAC: roles, permissions, assignments — A · P0 · S1 · 5–7 d · Deps: T-0003 · AC: 5 seed roles (PRD §4 personas); permission checks enforced in middleware; custom roles.
- **T-0005** Kong gateway: routes, JWT validation, rate limits — A · P0 · S1 · 3–5 d · Deps: T-0001 · AC: All services behind gateway; unauthorized 401; tenant rate limits configurable.
- **T-0006** config-svc: camera/site/zone/mask/ROI API — A · P0 · S2 · 5–7 d · Deps: T-0003 · AC: CRUD for all config types; versioned revisions for edge sync; OPA-enforced tenant scoping.
- **T-0007** device-svc: camera registry CRUD — A · P0 · S2 · 3–5 d · Deps: T-0003 · AC: Camera lifecycle (pending/active/offline/retired); serial-bound uniqueness per tenant.
- **T-0008** device claim + activation tokens — A · P0 · S2 · 3–5 d · Deps: T-0007 · AC: Claim token one-time use; activation binds device to tenant; expiry 24 h.
- **T-0009** device status state machine + heartbeat API — A · P0 · S2 · 2–3 d · Deps: T-0007 · AC: Heartbeat updates status; stale-device detection flags offline in < 90 s.
- **T-0010** audit-svc: append-only audit log — A · P0 · S2 · 3–5 d · Deps: T-0003 · AC: All admin/config/auth mutations logged; immutability test (no UPDATE path).
- **T-0011** audit daily hash chain + public verify — A · P0 · S3 · 3–5 d · Deps: T-0010 · AC: Chain root published daily; verify API confirms tamper (SD-05); test rejects modified row.
- **T-0012** event-svc: ingestion, persistence, dedupe — A · P0 · S3 · 5–7 d · Deps: T-0005 · AC: ≥1,000 ev/s sustained; `dedupe_key` deduped; event schema from registry; retention configurable.
- **T-0013** alert-svc: rule engine + severity model — A · P0 · S4 · 5–7 d · Deps: T-0012 · AC: Rules per module per tenant; severity P0–P2; suppression windows; alert payload ≤ 3 s after event.
- **T-0014** notify-svc: email/SMS/push/webhook dispatch — A · P0 · S4 · 5–7 d · Deps: T-0013 · AC: Delivery p95 ≤ 10 s; per-channel templates; failure retries with backoff; dead-letter visible.
- **T-0015** notification preferences + quiet hours — A · P0 · S4 · 2–3 d · Deps: T-0014 · AC: Per-user per-module opt-in/out; quiet hours honored in dispatch.
- **T-0016** analytics-svc: incident aggregation + dashboard data — A · P0 · S5 · 5–7 d · Deps: T-0013 · AC: Incidents rolled up per site/module/severity; hourly + daily aggregates; API for dashboards.
- **T-0017** report-svc: dossier assembly — A · P0 · S5 · 3–5 d · Deps: T-0016 · AC: Dossier = incident timeline + evidence refs + actions; template-driven.
- **T-0018** report-svc: PDF + evidence package export — A · P0 · S6 · 3–5 d · Deps: T-0017, T-0040 · AC: PDF and ZIP export; package hash manifest; download via signed URL.
- **T-0019** playback-svc: clip locator + signed URLs — A · P0 · S6 · 3–5 d · Deps: T-0006 · AC: Clip resolved from timeline query; URLs expire in 60 s; tenant-scoped.
- **T-0020** search-svc: indexing pipeline — A · P0 · S6 · 3–5 d · Deps: T-0012 · AC: Events/incidents indexed to OpenSearch; index aliases; reindex runbook.
- **T-0021** search-svc: query API (filters, date range) — A · P0 · S6 · 3–5 d · Deps: T-0020 · AC: Query p95 < 1 s; filters by module/site/severity/actor; paginated; multi-tenant safe.
- **T-0022** integration-svc: webhooks — A · P0 · S6 · 3–5 d · Deps: T-0014 · AC: HMAC signature; retries with backoff; delivery logs; disable on repeated failure.
- **T-0023** GraphQL BFF: read-only + federation base — A · P0 · S6 · 5–7 d · Deps: T-0021 · AC: Read-only enforcement (no mutations); depth limiting; per-tenant data scoping.
- **T-0024** realtime-gw WebSocket gateway — A · P0 · S5 · 5–7 d · Deps: T-0013 · AC: Alert/event push to clients < 1 s; reconnect with resubscribe; per-tenant channel auth.
- **T-0025** API keys (scoped, revocable) — A · P0 · S6 · 2–3 d · Deps: T-0005 · AC: Create/revoke/rotate; per-key scopes; last-used audit.
- **T-0026** billing-svc: plans A–E + usage metering — A · P1 · S7 · 5–7 d · Deps: T-0016 · AC: Metered camera-days per plan; plan limits enforced at API layer; upgrade/downgrade path.
- **T-0027** billing-svc: invoices + payment gateway — A · P1 · S7 · 5–7 d · Deps: T-0026 · AC: Razorpay (IN) + Stripe fallback; GST invoice PDF; dunning emails.
- **T-0028** model-registry-svc: metadata + promotion — A · P1 · S8 · 5–7 d · Deps: T-0012 · AC: Model versions, artifacts, licenses; promotion states dev→staging→shadow→canary→prod.
- **T-0029** model rollback API (registry pointer) — A · P1 · S8 · 2–3 d · Deps: T-0028 · AC: One-click rollback to incumbent < 2 min; audited.
- **T-0030** KMS per-tenant key hierarchy + envelope encryption — A · P0 · S8 · 5–7 d · Deps: — · AC: Per-tenant data keys; field-level encryption for biometrics (SD-04); rotation drill.
- **T-0031** ClickHouse cold analytics path — A · P1 · S9 · 5–7 d · Deps: T-0016 · AC: Analytics queries served from ClickHouse; hot/cold split configurable; backfill job.
- **T-0032** retention/erasure jobs + manifests — A · P0 · S9 · 5–7 d · Deps: T-0031 · AC: 30/90/365-day tenant config; erasure across all stores with verified manifest; audit trail.
- **T-0033** TimescaleDB partitioning (events, telemetry) — A · P0 · S9 · 3–5 d · Deps: T-0012 · AC: Month-partitioned hypertables; retention drop policy; no full-table scans on hot path.
- **T-0034** audit verify public API — A · P1 · S10 · 2–3 d · Deps: T-0011 · AC: Public verify endpoint (SD-05); documented; load-tested.
- **T-0035** bulk camera import (CSV) — A · P1 · S8 · 2–3 d · Deps: T-0007 · AC: 1,000-row import with validation report; idempotent.
- **T-0036** camera groups + bulk config — A · P1 · S8 · 2–3 d · Deps: T-0006 · AC: Group CRUD; config apply to group; override per camera.
- **T-0037** alert acknowledgment + workflow states — A · P0 · S5 · 3–5 d · Deps: T-0013 · AC: Acknowledge/resolve/assign; state transitions audited; SLA timer per severity.
- **T-0038** alert escalation policies — A · P1 · S11 · 3–5 d · Deps: T-0037 · AC: Escalation matrix per tenant; on-call rotation hook; escalation audit.
- **T-0039** incident assembly (multi-event rollup) — A · P0 · S5 · 5–7 d · Deps: T-0013 · AC: Related events rolled into one incident; open/closed lifecycle; dedupe of duplicates.
- **T-0040** evidence package manifest + hash chain export — A · P0 · S6 · 3–5 d · Deps: T-0018 · AC: Export includes artifact hashes + chain root link; verify API returns match.
- **T-0041** OPA policies (authz) — A · P0 · S2 · 3–5 d · Deps: T-0005 · AC: Policy-as-code for tenant/site/role scoping; policy tests in CI; deny-by-default.
- **T-0042** tenant isolation suite (automated) — A · P0 · S8 · 3–5 d · Deps: T-0041 · AC: Cross-tenant read/write attempts fail; suite runs as CI merge gate; 100% pass.
- **T-0043** OpenSearch multi-tenant index strategy — A · P0 · S8 · 3–5 d · Deps: T-0020 · AC: Tenant partitioning verified; cross-tenant query impossible; per-tenant quotas.
- **T-0044** GraphQL federation full rollout — A · P1 · S12 · 3–5 d · Deps: T-0023 · AC: All read paths via federation; gateway health; no authz bypass.
- **T-0045** API versioning policy + deprecation handling — A · P1 · S10 · 2–3 d · Deps: — · AC: Version header/URL scheme documented; deprecated endpoints return warnings; sunset schedule.
- **T-0046** playback timeline API — A · P0 · S6 · 3–5 d · Deps: T-0019 · AC: Timeline query returns clip segments with gaps; marker support for incidents.
- **T-0047** per-tenant rate limits + quota enforcement — A · P1 · S11 · 2–3 d · Deps: T-0005 · AC: Limits per plan; 429 responses with retry-after; quota metrics.
- **T-0048** PTZ control API — A · P2 · S17 · 3–5 d · Deps: T-0007 · AC: Pan/tilt/zoom/preset via ONVIF; per-role authorization; movement audit.
- **T-0049** mask/ROI config plumbing to edge — A · P0 · S5 · 2–3 d · Deps: T-0006 · AC: Mask polygons and ROI delivered with config revision; masking verified pre-encode.
- **T-0050** event enrichment pipeline (camera, zone, meta) — A · P0 · S5 · 3–5 d · Deps: T-0012 · AC: Events enriched with camera/site/zone/floor-plan context before persistence.
- **T-0051** dedupe_key enforcement in consumers — A · P0 · S5 · 2–3 d · Deps: T-0012 · AC: Consumer idempotency verified under duplicate-injection tests.
- **T-0052** Avro schema registry + compatibility CI — A · P0 · S3 · 3–5 d · Deps: — · AC: Schemas versioned; additive-only evolution enforced in CI; consumers pin compatible versions.
- **T-0053** idempotent consumer library — A · P0 · S3 · 3–5 d · Deps: T-0052 · AC: Library with dedupe + outbox pattern; unit-tested; used by all consumers.
- **T-0054** Kinesis producer/consumer Go lib — A · P0 · S3 · 3–5 d · Deps: — · AC: Producer batch + retry; consumer checkpointing; backpressure handling.
- **T-0055** MSK topics + retention config — A · P0 · S3 · 2–3 d · Deps: — · AC: Topics per domain; retention sized for replay window; ACLs per service.
- **T-0056** SQS/SNS routing rules — A · P0 · S3 · 2–3 d · Deps: T-0055 · AC: Routing matrix documented; DLQ configured for every queue.
- **T-0057** RabbitMQ task queue infra + lib — A · P0 · S4 · 3–5 d · Deps: — · AC: Task queues for async jobs (notify, report); prefetch limits; dead-letter policy.
- **T-0058** notification templates per channel — A · P0 · S4 · 2–3 d · Deps: T-0014 · AC: Email/SMS/push/webhook templates; localization-ready; preview endpoint.
- **T-0059** SMS gateway integration (India) — A · P0 · S4 · 2–3 d · Deps: T-0058 · AC: 2 providers configured (failover); delivery receipts; rate limits.
- **T-0060** push notifications via realtime-gw (FCM/APNs) — A · P0 · S5 · 3–5 d · Deps: T-0024 · AC: Alert push delivered < 10 s; token lifecycle; badge handling.
- **T-0061** search filters + saved searches API — A · P1 · S6 · 2–3 d · Deps: T-0021 · AC: Saved search CRUD per user; filter presets.
- **T-0062** incident timeline API — A · P0 · S6 · 2–3 d · Deps: T-0039 · AC: Timeline with events, actions, evidence; pagination.
- **T-0063** report templates + scheduled reports — A · P1 · S9 · 3–5 d · Deps: T-0018 · AC: Template library (daily/weekly); scheduler; delivery to email/S3.
- **T-0064** CSV export for analytics — A · P1 · S9 · 2–3 d · Deps: T-0031 · AC: Aggregate export; row caps with async job; audit logged.
- **T-0065** org settings API — A · P0 · S2 · 2–3 d · Deps: T-0003 · AC: Org profile, timezone, retention default, branding fields.
- **T-0066** user management API — A · P0 · S2 · 3–5 d · Deps: T-0004 · AC: Invite/disable/reassign; password policy; MFA enforcement flag.
- **T-0067** role assignment + custom roles — A · P0 · S2 · 3–5 d · Deps: T-0004 · AC: Assign roles at org/site scope; custom role builder with permission matrix.
- **T-0068** SSO/SAML — A · P2 · S13 · 5–7 d · Deps: T-0066 · AC: SAML 2.0 for enterprise tenants; Just-in-time provisioning.
- **T-0069** usage metering pipeline (billing) — A · P1 · S7 · 3–5 d · Deps: T-0026 · AC: Camera-days + alerts + storage metered; 5-min aggregation; reconciliation report.
- **T-0070** usage dashboards API — A · P1 · S7 · 2–3 d · Deps: T-0069 · AC: Usage by site/plan; exportable; refresh ≤ 1 h.
- **T-0071** trial/pilot provisioning workflow — A · P1 · S7 · 3–5 d · Deps: T-0026 · AC: 14-day trial with plan limits; conversion path to paid; expiry notifications.
- **T-0072** partner/MSP account model — A · P2 · S13 · 5–7 d · Deps: T-0003 · AC: Partner org with sub-tenant management; reseller metadata; revenue share tracking fields.
- **T-0073** cross-site tenancy + consolidated API — A · P2 · S19 · 5–7 d · Deps: T-0042 · AC: Multi-site org aggregates; per-site drill-down; RBAC scopes to site.
- **T-0074** KVS stream metadata + chunk manifest — A · P0 · S6 · 5–7 d · Deps: T-0019 · AC: Stream index; chunk manifests for playback; gap detection.
- **T-0075** face enrollment API + consent records — A · P0 · S5 · 3–5 d · Deps: T-0003 · AC: Consent timestamp required; consent revocation triggers embedding erasure; audit trail.
- **T-0076** embedding storage isolation — A · P0 · S8 · 3–5 d · Deps: T-0030 · AC: Embeddings encrypted per-tenant; no cross-tenant matching; erasure path verified.
- **T-0077** liveness integration API — A · P0 · S5 · 3–5 d · Deps: T-0075 · AC: Liveness challenge initiation; result persisted with enrollment.
- **T-0078** model promotion workflow API — A · P1 · S8 · 2–3 d · Deps: T-0028 · AC: Approval gates; eval-card attachment required; audit trail.
- **T-0079** shadow inference A/B routing — A · P1 · S8 · 5–7 d · Deps: T-0078 · AC: % traffic routing to candidate; metrics comparison dashboard; auto-abort on regression.
- **T-0080** camera health aggregation service — A · P0 · S5 · 3–5 d · Deps: T-0009 · AC: Health score from telemetry + AI module (FR-116); alerts on degradation; dashboard API.
- **T-0081** webhook retry/backoff + dead-letter — A · P0 · S6 · 2–3 d · Deps: T-0022 · AC: Exponential backoff; max 10 attempts; DLQ with replay UI.
- **T-0082** GraphQL depth limiting + tenant scoping — A · P0 · S6 · 2–3 d · Deps: T-0023 · AC: Query depth/cost limits; scoping tests; no N+1 via resolver review.
- **T-0083** API gateway per-tenant rate limits — A · P1 · S11 · 2–3 d · Deps: T-0005 · AC: Plan-based limits enforced at edge; metrics per tenant.
- **T-0084** event replay tooling (from MSK) — A · P1 · S10 · 3–5 d · Deps: T-0055 · AC: Replay events from offset/timestamp; dry-run mode; used in DR drill.
- **T-0085** audit log viewer API — A · P1 · S9 · 2–3 d · Deps: T-0010 · AC: Filterable viewer; export; role-scoped.
- **T-0086** playback cost control (bitrate/retention config) — A · P0 · S6 · 2–3 d · Deps: T-0019 · AC: Per-camera bitrate/retention; cost estimate API; KVS budget guard.
- **T-0087** ONVIF Profile S/T adapter — A · P2 · S17 · 5–7 d · Deps: T-0048 · AC: Discovery, streaming, events from ONVIF devices; contract-tested.
- **T-0088** Hikvision/Dahua adapter pack — A · P2 · S17 · 5–7 d · Deps: T-0087 · AC: Two vendor NVRs onboarded end-to-end; vendor isolation layer.
- **T-0089** access-control integration — A · P2 · S17 · 5–7 d · Deps: T-0022 · AC: Door/access events ingested; correlation with incidents.
- **T-0090** channel/integrator billing (reseller) — A · P2 · S18 · 5–7 d · Deps: T-0072 · AC: Reseller markup; white-label invoice; margin reports.

---

### 10.B AI/ML (T-0091–T-0150)

- **T-0091** ingestion-svc: frame/track ingestion + sampling — B · P0 · S4 · 5–7 d · Deps: T-0054 · AC: 5–10 FPS sampling per OD-12; tenant/device quotas; backpressure to edge.
- **T-0092** ingestion ROI gating (cheap→expensive cascade) — B · P0 · S4 · 3–5 d · Deps: T-0091 · AC: Motion/foreground gating before full inference; cascade order regression-tested.
- **T-0093** detection backbone training pipeline (SageMaker) — B · P0 · S4 · 5–7 d · Deps: T-0112 · AC: Reproducible training job; artifact pinned (seed, data hash, deps lockfile).
- **T-0094** backbone→TensorRT INT8 conversion pipeline — B · P0 · S4 · 3–5 d · Deps: T-0093 · AC: INT8 artifact reproducible from pinned ONNX + calibration set hash; per-tier build.
- **T-0095** weapon detection model + eval card — B · P0 · S4 · 5–7 d · Deps: T-0093 · AC: Meets AI-ARCHITECTURE §3 target; eval card with AP/PR, per-brightness slices.
- **T-0096** fire/smoke detection model + eval card — B · P0 · S4 · 5–7 d · Deps: T-0093 · AC: Target accuracy met on held-out set; false-negative review by safety lead.
- **T-0097** intrusion/restricted-zone model + eval card — B · P0 · S4 · 5–7 d · Deps: T-0093 · AC: Zone-line crossing detected; configurable zones honored; eval card published.
- **T-0098** temporal confirmation engine — B · P0 · S4 · 3–5 d · Deps: T-0093 · AC: N-frame/m-second confirmation suppresses single-frame FPs; config per module.
- **T-0099** PPE model (helmet, vest, gloves, mask) + eval card — B · P0 · S5 · 5–7 d · Deps: T-0093 · AC: 4 classes meet §3 targets on Indian-site footage; eval card with per-class metrics.
- **T-0100** fall detection model + eval card — B · P0 · S5 · 5–7 d · Deps: T-0093 · AC: Fall vs sit/lie confusion ≤ target; pose-based or temporal model justified in ADR.
- **T-0101** fight detection model + eval card — B · P0 · S5 · 5–7 d · Deps: T-0093 · AC: Meeting §3 target; crowd-scene FP tested; eval card published.
- **T-0102** loitering model + eval card — B · P0 · S5 · 5–7 d · Deps: T-0098 · AC: Dwell-time configurable; zone-scoped; FP rate on busy sites ≤ target.
- **T-0103** face detection + ArcFace embeddings service — B · P0 · S5 · 5–7 d · Deps: T-0093 · AC: ≥99% TAR @ FAR 1e-5 (target); embeddings-only output; no raw face persistence.
- **T-0104** face matching index (per-tenant) — B · P0 · S6 · 3–5 d · Deps: T-0103 · AC: Match query < 2 s at 10k enrollments; per-tenant index isolation.
- **T-0105** liveness module (single-camera basic) — B · P0 · S5 · 5–7 d · Deps: T-0103 · AC: Blink/motion challenge; spoof (photo/video) rejection ≥ target rate in test set.
- **T-0106** attendance logic (arrival/departure, shifts) — B · P0 · S5 · 3–5 d · Deps: T-0104 · AC: Attendance records with shift mapping; duplicate-check-in rule; report-ready.
- **T-0107** ByteTrack tracker integration — B · P0 · S5 · 3–5 d · Deps: T-0091 · AC: Track IDs stable within view; handoff gap documented (ReID in S13); MOT metrics recorded.
- **T-0108** alert object selection (best frame) — B · P0 · S5 · 2–3 d · Deps: T-0107 · AC: Best-evidence frame/snippet chosen; bounding box + confidence attached to alert.
- **T-0109** snapshot/thumbnail extraction — B · P0 · S5 · 2–3 d · Deps: T-0108 · AC: Thumbnails generated at alert; < 500 ms extra latency; S3 stored with hash.
- **T-0110** clip extraction at edge (evidence clips) — B · P0 · S4 · 3–5 d · Deps: C-edge agent · AC: Pre/post-roll configurable; clip hash for evidence chain.
- **T-0111** model benchmark harness (per device tier) — B · P0 · S4 · 5–7 d · Deps: T-0094 · AC: FPS/latency/accuracy matrix for Edge S/M/L; results committed per model release.
- **T-0112** eval datasets v1 (DVC) — B · P0 · S4 · 5–7 d · Deps: — · AC: Held-out sets per module; DVC-managed; labeling QA sampled ≥5%.
- **T-0113** labeling ops + annotation tooling — B · P0 · S4 · 3–5 d · Deps: — · AC: Annotation workflow; inter-annotator agreement ≥ target; export to training format.
- **T-0114** confidence calibration + threshold tuning backend — B · P0 · S5 · 3–5 d · Deps: T-0095 · AC: Per-module threshold config per tenant; calibration curve dashboard.
- **T-0115** false-positive reduction pass 1 — B · P0 · S5 · 5–7 d · Deps: T-0098 · AC: Pilot-site FP ≤ 1 per 5 cams/day; top FP classes documented.
- **T-0116** false-positive reduction pass 2 — B · P1 · S8 · 5–7 d · Deps: T-0115 · AC: FP SLO held at 5 pilot sites × 7 days; regression suite for FP scenarios.
- **T-0117** bias eval suite (Indian demographics) — B · P0 · S5 · 3–5 d · Deps: T-0112 · AC: Per-slice metrics; no slice below 90% of aggregate; report in model card.
- **T-0118** adversarial eval suite — B · P1 · S10 · 5–7 d · Deps: T-0117 · AC: Occlusion/lighting/patch attacks tested; failure modes documented; fixes gated.
- **T-0119** camera health module (analytics) — B · P0 · S5 · 3–5 d · Deps: T-0091 · AC: Blur/noise/blockage/offline detection; health score feeds T-0080 (FR-116).
- **T-0120** ONNX export validation — B · P0 · S4 · 2–3 d · Deps: T-0093 · AC: Export reproduces PyTorch metrics within tolerance; CI check.
- **T-0121** model quantization QA — B · P0 · S4 · 2–3 d · Deps: T-0094 · AC: INT8 vs FP16 metric delta ≤ threshold; per-tier report.
- **T-0122** per-device model performance bench — B · P1 · S8 · 3–5 d · Deps: T-0111 · AC: Edge S/M/L benchmark results; regression detection on upgrade.
- **T-0123** auto model-card generation — B · P0 · S4 · 2–3 d · Deps: T-0112 · AC: Card generated from eval runs; includes license + provenance + limitations.
- **T-0124** synthetic data generation (rare events) — B · P1 · S8 · 5–7 d · Deps: T-0112 · AC: Synthetic pipeline for weapon/fire/fall; human-in-loop QA; augmentation catalog.
- **T-0125** multi-class head tuning — B · P0 · S5 · 3–5 d · Deps: T-0093 · AC: Specialist heads share backbone (D1); head swap without backbone retrain verified.
- **T-0126** LPR model (Indian plates) — B · P2 · S13 · 5–7 d · Deps: T-0093 · AC: ≥92% char accuracy on IN plate formats (3 test sets); low-light slice report.
- **T-0127** ReID at handoff — B · P2 · S13 · 5–7 d · Deps: T-0107 · AC: Track continuity metric +15% vs no-ReID; embeddings-only storage.
- **T-0128** face search — B · P2 · S13 · 5–7 d · Deps: T-0104 · AC: Search by photo < 2 s p95 at 10k enrollments; consent-gated; audit logged.
- **T-0129** vehicle detection/classification — B · P2 · S13 · 5–7 d · Deps: T-0093 · AC: Vehicle class AP ≥ target; color estimation; eval card.
- **T-0130** speed estimation — B · P2 · S14 · 3–5 d · Deps: T-0129 · AC: ±5 km/h of ground truth; calibration procedure documented.
- **T-0131** parking occupancy — B · P2 · S14 · 3–5 d · Deps: T-0129 · AC: ≥90% accuracy; spot-level mapping configurable.
- **T-0132** traffic counting — B · P2 · S14 · 3–5 d · Deps: T-0129 · AC: Counts within 5% of manual; direction split; hourly aggregates.
- **T-0133** crowd density — B · P2 · S15 · 5–7 d · Deps: T-0093 · AC: Bands (low/med/high/critical) accurate ±10% at ≤30 people/frame; heatmap data.
- **T-0134** people counting/occupancy — B · P2 · S15 · 5–7 d · Deps: T-0107 · AC: ±10% vs manual counts; zone-based counting; dashboard integration.
- **T-0135** generic object classification (expanded classes) — B · P2 · S17 · 5–7 d · Deps: T-0093 · AC: Additional classes meet §3 targets; eval card published.
- **T-0136** predictive capacity planning — B · P2 · S18 · 5–7 d · Deps: T-0134 · AC: Forecast ±15% on 2 pilot datasets; labeled advisory in UI.
- **T-0137** theft-risk scoring — B · P2 · S18 · 5–7 d · Deps: T-0136 · AC: Score + drivers explainable; false-positive rate target; advisory labeling.
- **T-0138** gen-AI incident narratives (grounded) — B · P2 · S21 · 5–7 d · Deps: T-0039 · AC: Fact-only narratives from event data; hallucination < 2% on eval set; PII redaction.
- **T-0139** gen-AI search copilot — B · P2 · S21 · 5–7 d · Deps: T-0138 · AC: Answers scoped to tenant data; grounding citations; refuse out-of-scope.
- **T-0140** narrative grounding + hallucination harness — B · P2 · S21 · 3–5 d · Deps: T-0138 · AC: Automated eval set; release-blocking gate on hallucination rate.
- **T-0141** response-policy engine (PTZ/siren/light) — B · P2 · S20 · 5–7 d · Deps: T-0048 · AC: Policy→action mapping; default-off; kill switch per site; every action audited.
- **T-0142** PTZ auto-track — B · P2 · S20 · 5–7 d · Deps: T-0141 · AC: Maintains subject in frame ≥ target%; fails safe to preset on loss.
- **T-0143** behavior anomaly baseline — B · P2 · S20 · 5–7 d · Deps: T-0107 · AC: Deviation alert with configurable sensitivity; FP target documented.
- **T-0144** embedding model versioning path — B · P2 · S13 · 3–5 d · Deps: T-0103 · AC: New embedding versions re-index tenants incrementally; rollback supported.
- **T-0145** dataset provenance registry — B · P1 · S8 · 2–3 d · Deps: T-0112 · AC: Every dataset recorded (source, license, capture date); license check in eval gate.
- **T-0146** eval gate automation in CI (eval-svc wired) — B · P0 · S5 · 3–5 d · Deps: T-0111 · AC: All 7 gates run automatically; results attached to model version; CI blocks promotion.
- **T-0147** training reproducibility (pinned seeds/deps) — B · P0 · S4 · 2–3 d · Deps: T-0093 · AC: Re-run reproduces metrics within tolerance; artifact hash stable.
- **T-0148** shadow traffic collection pipeline — B · P1 · S8 · 3–5 d · Deps: T-0079 · AC: Shadow predictions stored; label sampling; comparison dashboard.
- **T-0149** cloud GPU inference fallback — B · P1 · S9 · 5–7 d · Deps: T-0094 · AC: Burst/edge-down fallback with latency SLA; cost cap per tenant; auto-failback.
- **T-0150** module registry manifest (23 modules) — B · P0 · S4 · 2–3 d · Deps: — · AC: Registry lists module, owner, model, hardware tier, eval card link (AI-ARCHITECTURE §2–3).

### 10.C Edge (T-0151–T-0180)

- **T-0151** edge agent scaffold (Python, service infra) — C · P0 · S3 · 3–5 d · Deps: — · AC: Agent boots, logs JSON, registers with device-svc, telemetry skeleton.
- **T-0152** RTSP ingest (ffmpeg wrapper) — C · P0 · S3 · 3–5 d · Deps: T-0151 · AC: 3+ vendor streams verified; reconnect handling; sub-2% dropped frames on LAN.
- **T-0153** H.264/H.265 decode — C · P0 · S3 · 2–3 d · Deps: T-0152 · AC: Both codecs decode on NX; GPU decode path where available.
- **T-0154** store-and-forward (disk quota, eviction) — C · P0 · S3 · 3–5 d · Deps: T-0152 · AC: 10-min offline → no loss; quota eviction oldest-first; metrics exported.
- **T-0155** health telemetry (CPU/GPU/temp/latency) — C · P0 · S3 · 2–3 d · Deps: T-0151 · AC: Telemetry to cloud ≤ 30 s interval; temp/thermal throttling warnings.
- **T-0156** config sync from config-svc — C · P0 · S3 · 2–3 d · Deps: T-0006 · AC: Pull-on-interval + push; atomic apply; revision tracked; rollback on apply failure.
- **T-0157** time sync + power-loss recovery — C · P0 · S3 · 2–3 d · Deps: T-0154 · AC: NTP + PTP best-effort; on reboot, store-and-forward replays cleanly.
- **T-0158** device certificates (IoT Core) — C · P0 · S3 · 2–3 d · Deps: T-0151 · AC: Cert issuance + rotation; mutual TLS; attestation hook reserved.
- **T-0159** masked frame encoding (pre-encode) — C · P0 · S5 · 3–5 d · Deps: T-0156 · AC: Mask applied before encode; no unmasked frame leaves device (verified by test).
- **T-0160** frame sampling + dedupe (OD-12) — C · P0 · S4 · 2–3 d · Deps: T-0152 · AC: Sampling caps enforced; duplicate-frame suppression; metrics for invariants.
- **T-0161** TensorRT runtime + Triton client — C · P0 · S4 · 3–5 d · Deps: T-0094 · AC: Model repo loading; dynamic batch; latency budget per tier.
- **T-0162** Jetson Orin NX image — C · P0 · S4 · 3–5 d · Deps: T-0161 · AC: Flashable image; agent auto-start; power profile validated (T-0180).
- **T-0163** x86 + RTX 4000 Ada image — C · P0 · S4 · 3–5 d · Deps: T-0161 · AC: GPU + CPU fallback paths; same agent codebase (no fork).
- **T-0164** edge dashboard (device-local status) — C · P0 · S6 · 2–3 d · Deps: T-0155 · AC: Local UI shows health, config revision, queue depth; technician-facing.
- **T-0165** OTA client + IoT Jobs integration — C · P0 · S6 · 5–7 d · Deps: T-0158 · AC: Job-based update; staged rollout support; rollback on boot failure (2-boot rule).
- **T-0166** firmware signing + staged rollout — C · P0 · S7 · 3–5 d · Deps: T-0165 · AC: Signed bundles verified pre-install; 5→25→100% groups; health-regression abort.
- **T-0167** edge credential rotation — C · P1 · S9 · 2–3 d · Deps: T-0158 · AC: Automated rotation < 5 min; no service interruption.
- **T-0168** secure boot/attestation — C · P1 · S10 · 5–7 d · Deps: T-0166 · AC: Boot chain verified; device attestation report to cloud; failed attestation quarantined.
- **T-0169** edge firewall config — C · P1 · S10 · 2–3 d · Deps: T-0151 · AC: Egress allowlist only; documented ports; verified by scan.
- **T-0170** CPU fallback mode — C · P1 · S8 · 3–5 d · Deps: T-0163 · AC: Degraded FPS mode on GPU failure; alert to cloud; auto-recovery.
- **T-0171** AGX Orin support (Edge M) — C · P2 · S13 · 5–7 d · Deps: T-0162 · AC: Image + benchmark for Edge M tier; documented model capacity.
- **T-0172** RTX 4000 Ada support (Edge L) — C · P2 · S13 · 5–7 d · Deps: T-0163 · AC: Image + benchmark; multi-model concurrency validated.
- **T-0173** edge cache/eviction tuning — C · P1 · S8 · 2–3 d · Deps: T-0154 · AC: Cache hit-rate targets; eviction priority (evidence > telemetry); metrics.
- **T-0174** network resilience (buffering, backoff) — C · P0 · S3 · 3–5 d · Deps: T-0152 · AC: Jitter buffering; exponential backoff; adaptive bitrate on constrained links.
- **T-0175** on-edge attendance cache — C · P0 · S5 · 2–3 d · Deps: T-0159 · AC: Attendance events cached offline; sync on reconnect with dedupe.
- **T-0176** fleet status aggregation — C · P0 · S6 · 3–5 d · Deps: T-0155 · AC: Fleet view per site: version, health, storage; upgrade eligibility flags.
- **T-0177** Greengrass v2 deployment — C · P0 · S7 · 5–7 d · Deps: T-0165 · AC: Greengrass manages agent lifecycle; component versions; fleet status in cloud.
- **T-0178** local storage management — C · P0 · S3 · 2–3 d · Deps: T-0154 · AC: Quota per category; retention enforcement; space alerts.
- **T-0179** RTSP vendor compatibility matrix + tests — C · P0 · S3 · 3–5 d · Deps: T-0152 · AC: Matrix of 10+ cameras; automated smoke per firmware update.
- **T-0180** edge power profiling (Jetson) — C · P0 · S4 · 2–3 d · Deps: T-0162 · AC: Power modes documented; thermal headroom for India climate; sustained-load test 48 h.

---

### 10.D Frontend (T-0181–T-0220)

- **T-0181** web app scaffold (Vite + React 18 + TS) — D · P0 · S1 · 2–3 d · Deps: — · AC: App boots; router, lint, CI build green; base theme tokens.
- **T-0182** design system (packages/ui per UX doc) — D · P0 · S1 · 5–7 d · Deps: T-0181 · AC: Core components (buttons, tables, modals, forms, toasts) with a11y; storybook-style gallery.
- **T-0183** login + OIDC flow (PKCE) — D · P0 · S1 · 3–5 d · Deps: T-0001 · AC: Login/logout/refresh; redirect after login; session expiry handling; SSO-ready.
- **T-0184** org onboarding shell — D · P0 · S2 · 3–5 d · Deps: T-0183 · AC: Org creation, first site, invite-first-user flow (UX §7 onboarding).
- **T-0185** user management UI — D · P0 · S2 · 3–5 d · Deps: T-0066 · AC: List/invite/disable users; role display; search.
- **T-0186** role editor UI — D · P0 · S2 · 3–5 d · Deps: T-0067 · AC: Custom role builder with permission matrix; validation.
- **T-0187** camera management UI — D · P0 · S2 · 3–5 d · Deps: T-0007 · AC: Register/claim camera; status badges; detail panel with health.
- **T-0188** settings UI (org, site) — D · P0 · S2 · 2–3 d · Deps: T-0065 · AC: Org/site settings screens; retention default; branding.
- **T-0189** live view grid (player) — D · P0 · S5 · 5–7 d · Deps: T-0024 · AC: 1–16 camera grid; stream start < 2 s; low-latency indicator; error states.
- **T-0190** incident feed + triage screen — D · P0 · S5 · 5–7 d · Deps: T-0037 · AC: Live feed of incidents; filter by severity/module/site; acknowledge/resolve from list (UX §7 triage).
- **T-0191** incident detail/dossier view — D · P0 · S6 · 3–5 d · Deps: T-0017 · AC: Timeline, evidence thumbnails, actions, linked playback; prints cleanly.
- **T-0192** evidence package download UI — D · P0 · S6 · 2–3 d · Deps: T-0040 · AC: One-click package download; hash verification hint shown.
- **T-0193** playback view + timeline scrubber — D · P0 · S6 · 5–7 d · Deps: T-0046 · AC: Seek to event marker; gap display; keyboard shortcuts.
- **T-0194** search UI — D · P0 · S6 · 3–5 d · Deps: T-0021 · AC: Filters, facets, results < 1 s; saved searches; export.
- **T-0195** dashboards (live + analytics) — D · P0 · S5 · 5–7 d · Deps: T-0016 · AC: Incident trends, camera health, module distribution; drill-down to list.
- **T-0196** attendance UI — D · P0 · S6 · 3–5 d · Deps: T-0106 · AC: Daily view, shift filter, exceptions; export (per HR persona).
- **T-0197** enrollment UI with consent flow — D · P0 · S5 · 3–5 d · Deps: T-0075 · AC: Consent first (uncheckable-until-accepted), photo preview, liveness prompt; erasure request path (UX §7 enrollment).
- **T-0198** notification settings UI — D · P0 · S4 · 2–3 d · Deps: T-0015 · AC: Per-module toggles, channels, quiet hours; live preview.
- **T-0199** mask/ROI editor — D · P0 · S5 · 3–5 d · Deps: T-0049 · AC: Polygon drawing on frame; save to config; applied mask preview.
- **T-0200** camera health view — D · P0 · S6 · 2–3 d · Deps: T-0080 · AC: Health scorecards; degradation timeline; per-camera drill-down.
- **T-0201** reports UI — D · P0 · S6 · 3–5 d · Deps: T-0018 · AC: Dossier list, filter, PDF download; scheduled report management (S9 extension).
- **T-0202** webhook config UI — D · P0 · S6 · 2–3 d · Deps: T-0022 · AC: Create/test (ping)/disable webhook; delivery log viewer.
- **T-0203** API keys UI — D · P1 · S9 · 2–3 d · Deps: T-0025 · AC: Create/rotate/revoke; scopes picker; last-used.
- **T-0204** billing/plan UI — D · P1 · S7 · 2–3 d · Deps: T-0027 · AC: Plan comparison, upgrade/downgrade, invoice list; payment flow (Razorpay/Stripe).
- **T-0205** audit log viewer UI — D · P1 · S9 · 2–3 d · Deps: T-0085 · AC: Filterable, exportable; role-gated (auditor persona).
- **T-0206** real-time alert surface (WebSocket) — D · P0 · S5 · 3–5 d · Deps: T-0024 · AC: Alerts appear < 1 s; reconnect resubscribe; badge counts.
- **T-0207** notification center UI — D · P0 · S5 · 2–3 d · Deps: T-0206 · AC: Read/unread, filters, mark-all-read; link to incident.
- **T-0208** PWA shell (offline, install, cache) — D · P1 · S9 · 3–5 d · Deps: T-0189 · AC: Installable; app shell cached; stale-while-revalidate for lists.
- **T-0209** accessibility pass (WCAG 2.2 AA) — D · P1 · S10 · 3–5 d · Deps: T-0182 · AC: axe-clean on all screens; keyboard navigation; focus management (UX §10 gate).
- **T-0210** dark mode + theme tokens — D · P1 · S9 · 2–3 d · Deps: T-0182 · AC: System-preference + manual toggle; contrast verified.
- **T-0211** i18n en-IN completion — D · P2 · S13 · 2–3 d · Deps: T-0181 · AC: All strings externalized; en-IN locale; no hardcoded text.
- **T-0212** onboarding wizard — D · P1 · S7 · 3–5 d · Deps: T-0187 · AC: New site setup < 15 min (UX §7); progress persistence; support handoff.
- **T-0213** empty states + guidance — D · P1 · S7 · 2–3 d · Deps: T-0190 · AC: Every empty state has next-action CTA; contextual help links.
- **T-0214** zone builder — D · P0 · S5 · 5–7 d · Deps: T-0199 · AC: Draw zones (intrusion, restricted, loitering); per-zone module config (UX §7 zone builder).
- **T-0215** responsive grid breakpoints — D · P1 · S7 · 2–3 d · Deps: T-0189 · AC: Usable at 1280/1440/1920; tablet pass; mobile read-only.
- **T-0216** saved searches UI — D · P1 · S9 · 2–3 d · Deps: T-0194 · AC: Save/load/delete searches; share within org.
- **T-0217** scheduled reports UI — D · P1 · S9 · 2–3 d · Deps: T-0063 · AC: Create/edit/deliver recipients; delivery status visible.
- **T-0218** multi-site dashboard — D · P2 · S19 · 3–5 d · Deps: T-0073 · AC: Consolidated KPIs with site drill-down; org switcher.
- **T-0219** Flutter app scaffold + auth + live view — D · P2 · S16 · 5–7 d · Deps: T-0183 · AC: iOS/Android build; login; live view player; alert list.
- **T-0220** mobile alert push UX — D · P2 · S16 · 3–5 d · Deps: T-0060 · AC: Push → tap → incident in < 5 s; deep links; offline banner.

### 10.E DevOps / Infrastructure (T-0221–T-0255)

- **T-0221** monorepo + CI matrix + caching — E · P0 · S1 · 3–5 d · Deps: — · AC: Path-based triggers; cache hits ≥ 80%; full pipeline < 15 min.
- **T-0222** sandbox environment bootstrap — E · P0 · S1 · 3–5 d · Deps: — · AC: Terragrunt init; AWS account ready; engineers can deploy.
- **T-0223** dev env (docker-compose + localstack) — E · P0 · S1 · 2–3 d · Deps: — · AC: One command dev stack; parity of core stores.
- **T-0224** preview env per PR — E · P0 · S2 · 3–5 d · Deps: T-0221 · AC: Ephemeral namespace per PR; URL + teardown automation.
- **T-0225** staging environment — E · P0 · S2 · 3–5 d · Deps: T-0230 · AC: Full-stack staging; nightly suite target; data isolation.
- **T-0226** EKS + VPC + networking — E · P0 · S2 · 5–7 d · Deps: T-0222 · AC: Clusters per env; private subnets; egress controls; Karpenter-ready.
- **T-0227** IAM least-privilege + OIDC IRSA — E · P0 · S3 · 3–5 d · Deps: T-0226 · AC: Per-service roles; no static keys; audit of unused roles.
- **T-0228** KMS setup + key rotation policy — E · P0 · S3 · 3–5 d · Deps: T-0226 · AC: Envelope encryption; auto-rotation; key policy reviews.
- **T-0229** secrets management (external-secrets) — E · P0 · S3 · 2–3 d · Deps: T-0228 · AC: Secrets synced to cluster; no plaintext in manifests; gitleaks gate active.
- **T-0230** ArgoCD App-of-Apps — E · P0 · S2 · 3–5 d · Deps: T-0226 · AC: All services in GitOps; sync policy (automated prune off initially); drift visible.
- **T-0231** prod ap-south-1 bootstrap — E · P0 · S6 · 5–7 d · Deps: T-0230 · AC: Prod cluster + stores; backups enabled; alerting wired before beta.
- **T-0232** Kinesis + MSK + SQS/SNS provisioning — E · P0 · S3 · 3–5 d · Deps: T-0226 · AC: Streams/topics/queues per BACKEND §8; IAM locked; retention set.
- **T-0233** RabbitMQ cluster — E · P0 · S4 · 3–5 d · Deps: T-0232 · AC: HA cluster; queue mirrors; monitoring dashboards.
- **T-0234** container registry + Cosign + SBOM — E · P0 · S3 · 2–3 d · Deps: T-0221 · AC: Multi-arch images signed; SBOM attached; registry policy blocks unsigned.
- **T-0235** Trivy scanning in CI — E · P0 · S3 · 1–2 d · Deps: T-0221 · AC: CRITICAL/HIGH blocks merge; base image updates automated.
- **T-0236** OTel collector + Prometheus + Thanos — E · P0 · S3 · 3–5 d · Deps: T-0226 · AC: All services exporting metrics/traces; thanos for long-term; storage sizing.
- **T-0237** Loki + Tempo — E · P1 · S11 · 3–5 d · Deps: T-0236 · AC: Logs/traces searchable; correlation via trace_id; retention per compliance.
- **T-0238** Grafana SLO dashboards + burn alerts — E · P1 · S11 · 3–5 d · Deps: T-0237 · AC: SLO widgets live; burn-rate alerts page; weekly review export.
- **T-0239** canary rollout automation (Argo Rollouts) — E · P1 · S8 · 5–7 d · Deps: T-0230 · AC: 5→50→100 with auto-rollback; manual gate at 50% for prod (first 3 months).
- **T-0240** blue-green DB migration runner — E · P1 · S8 · 3–5 d · Deps: T-0233 · AC: Expand/contract runner; lock window config; rollback-safe; rehearsed on staging.
- **T-0241** IoT Jobs + Greengrass OTA pipeline — E · P0 · S7 · 5–7 d · Deps: T-0165 · AC: Fleet OTA end-to-end; staged groups; rollback trigger verified.
- **T-0242** Karpenter autoscaling + budgets — E · P1 · S9 · 3–5 d · Deps: T-0226 · AC: Workload auto-scale; spot mix; node budget guardrails.
- **T-0243** cost monitoring dashboard — E · P1 · S9 · 3–5 d · Deps: T-0236 · AC: Per-service + per-tenant cost view; weekly report; anomaly alerts (OD-12).
- **T-0244** backup/restore validation — E · P1 · S9 · 3–5 d · Deps: T-0231 · AC: All stores backed up; restore drill passes RTO ≤ 60 min/RPO ≤ 5 min; report filed.
- **T-0245** DR drill 1 (region failover) — E · P1 · S11 · 3–5 d · Deps: T-0244 · AC: ap-south-1 → ap-southeast-1 read path < 60 min; data loss ≤ 5 min; postmortem.
- **T-0246** multi-region IaC groundwork (us-east-1) — E · P1 · S12 · 5–7 d · Deps: T-0231 · AC: Region-parameterized modules; state isolation; cross-region replication design.
- **T-0247** eu-central-1 rollout — E · P2 · S24 · 5–7 d · Deps: T-0246 · AC: EU region live; data pinned in-region; compliance checks pass.
- **T-0248** DR drill 2 (cross-region) — E · P2 · S24 · 3–5 d · Deps: T-0247 · AC: Full failover drill; SLOs met; playbook updated.
- **T-0249** 10k-cam scale validation environment — E · P2 · S24 · 5–7 d · Deps: T-0251 · AC: Fleet emulator at 10k; SLO pass at sustained load; cost report within OD-12.
- **T-0250** ClickHouse cluster + ingestion tuning — E · P1 · S9 · 5–7 d · Deps: T-0031 · AC: Cluster sized; insert spike-protected; query p95 targets met.
- **T-0251** synthetic edge fleet (OD-13 emulator) — E · P1 · S9 · 5–7 d · Deps: T-0232 · AC: Emulated agents generate realistic streams; used by load tests + evals.
- **T-0252** Sentry + alert routing — E · P0 · S3 · 2–3 d · Deps: T-0236 · AC: Error tracking wired for web + services; alert routing to on-call.
- **T-0253** K8s hardening baseline + upgrade cadence — E · P1 · S10 · 3–5 d · Deps: T-0226 · AC: CIS-benchmark scan clean; upgrade runbook; EKS minor version policy.
- **T-0254** IaC drift detection + plan gating — E · P1 · S9 · 2–3 d · Deps: T-0230 · AC: Drift reports weekly; CI plan check blocks unplanned changes.
- **T-0255** chaos suite (network/node/DB failover) — E · P2 · S25 · 5–7 d · Deps: T-0253 · AC: Game-day scenarios automated; SLO burn < 5%; blast radius scoped to staging.

---

### 10.F Security & Compliance (T-0256–T-0275)

- **T-0256** D6 AGPL decision + execution — F · P0 · S1 · 2–3 d · Deps: — · AC: Verdict recorded (ADR-001): enterprise license purchased OR Apache-2.0 swap (RT-DETR/D-FINE) selected; CI license allowlist enforced.
- **T-0257** gitleaks pre-commit + CI gate — F · P0 · S1 · 1–2 d · Deps: — · AC: Secrets blocked on commit + merge; breach scan of history.
- **T-0258** SAST (semgrep) in CI — F · P0 · S2 · 2–3 d · Deps: T-0257 · AC: Critical/high findings block merge; rule set maintained; false-positive policy.
- **T-0259** DAST (ZAP) on staging — F · P1 · S8 · 3–5 d · Deps: T-0230 · AC: Nightly scan; high+ findings triaged < 48 h; authentication flow included.
- **T-0260** pen test round 1 + remediation — F · P0 · S10 · 5–7 d · Deps: T-0259 · AC: Report with 0 critical/high open at GA; findings tracked to closure; retest evidence.
- **T-0261** pen test round 2 — F · P2 · S23 · 5–7 d · Deps: T-0260 · AC: All high/critical closed; report in compliance package.
- **T-0262** adversarial ML testing + fixes — F · P1 · S10 · 5–7 d · Deps: T-0118 · AC: Attack classes tested; mitigations or documented residual risk; eval gate re-run.
- **T-0263** biometric privacy review (DPDP) — F · P0 · S8 · 3–5 d · Deps: T-0076 · AC: Embeddings-only default verified; consent + erasure flows pass review; sign-off memo.
- **T-0264** per-tenant KMS audit + rotation drill — F · P1 · S8 · 2–3 d · Deps: T-0030 · AC: Key inventory; rotation exercised; no tenant material in shared keyspaces.
- **T-0265** compliance pack: India DPDP — F · P1 · S10 · 3–5 d · Deps: T-0263 · AC: Controls mapped; documentation + evidence; legal sign-off.
- **T-0266** compliance pack: GDPR — F · P2 · S18 · 3–5 d · Deps: T-0265 · AC: DPA-ready; data-subject request workflow; EU region readiness.
- **T-0267** HIPAA readiness review — F · P2 · S18 · 3–5 d · Deps: T-0265 · AC: Gap report + remediation list for healthcare vertical (persona Dr. Anand).
- **T-0268** SOC 2 Type I prep (controls inventory) — F · P1 · S12 · 3–5 d · Deps: T-0265 · AC: Control list mapped to evidence; gap list; audit scheduled.
- **T-0269** SOC 2 Type I audit — F · P2 · S23 · 5–7 d · Deps: T-0268 · AC: Report issued; exceptions remediated within sprint; evidence archive maintained.
- **T-0270** IR runbook + on-call rotation setup — F · P0 · S6 · 2–3 d · Deps: — · AC: IR roles, severity levels, comms plan; on-call roster; page testing.
- **T-0271** IR drill + postmortem — F · P1 · S10 · 2–3 d · Deps: T-0270 · AC: Simulated incident executed; postmortem within 48 h; action items tracked.
- **T-0272** data erasure verification tooling — F · P1 · S9 · 2–3 d · Deps: T-0032 · AC: Erasure confirmed across stores (including backups); certificate generated per tenant.
- **T-0273** SBOM/vendor review cadence — F · P1 · S10 · 2–3 d · Deps: T-0234 · AC: Quarterly SBOM review; critical CVE SLA; vendor risk register.
- **T-0274** vulnerability management cadence — F · P1 · S11 · 2–3 d · Deps: T-0273 · AC: Weekly triage; SLA by severity; exception process with expiry.
- **T-0275** third-party risk review (LLM, vendors) — F · P2 · S13 · 2–3 d · Deps: — · AC: LLM provider approved (data handling, residency); vendor contracts reviewed; register updated.

### 10.G QA / Testing (T-0276–T-0290)

- **T-0276** test framework + coverage gates — G · P0 · S1 · 2–3 d · Deps: T-0221 · AC: Coverage reporting; ≥80% changed-lines gate; flake quarantine tracker.
- **T-0277** Testcontainers integration suite — G · P0 · S2 · 5–7 d · Deps: T-0276 · AC: Per-service integration tests vs real stores; CI job; < 10 min total.
- **T-0278** contract tests (schema registry) — G · P0 · S4 · 3–5 d · Deps: T-0052 · AC: Provider/consumer compatibility enforced; schema evolution tests; CI gate.
- **T-0279** E2E Playwright core flows — G · P0 · S6 · 5–7 d · Deps: T-0190 · AC: Login→camera→alert→triage→export path green on staging; nightly + pre-release.
- **T-0280** k6 API SLO suites — G · P1 · S9 · 3–5 d · Deps: T-0239 · AC: p95 latency budgets asserted; run in CI preview + nightly staging.
- **T-0281** Locust event-ingest load tests — G · P1 · S9 · 3–5 d · Deps: T-0251 · AC: 2,000 ev/s spike protocol passes; backpressure verified; report committed.
- **T-0282** test data management (DVC clips, synthetic PII) — G · P0 · S3 · 2–3 d · Deps: T-0112 · AC: Versioned test sets; no customer data in dev; synthetic PII generator.
- **T-0283** regression suite automation — G · P1 · S10 · 3–5 d · Deps: T-0279 · AC: Automated regression on staging; failure triage SLA 24 h.
- **T-0284** mobile E2E (Flutter integration tests) — G · P2 · S16 · 3–5 d · Deps: T-0219 · AC: Login→alert→live-view path on both platforms; CI job.
- **T-0285** accessibility automated checks — G · P1 · S10 · 2–3 d · Deps: T-0209 · AC: axe runs in CI on core flows; zero critical violations.
- **T-0286** performance budget checks in CI — G · P1 · S9 · 2–3 d · Deps: T-0276 · AC: Bundle size/LCP budgets; fail on regression > 10%.
- **T-0287** flaky-test quarantine process — G · P0 · S3 · 1–2 d · Deps: T-0276 · AC: Quarantine < 24 h; retry policy; tracker dashboard.
- **T-0288** chaos game-day execution (quarterly) — G · P2 · S25 · 2–3 d · Deps: T-0255 · AC: Scenario run + postmortem; actions tracked to closure.
- **T-0289** SLO burn-alert validation tests — G · P1 · S11 · 2–3 d · Deps: T-0238 · AC: Burn alerts fire correctly on synthetic degradation; no alert storms.
- **T-0290** usability test sessions (UX §7 flows) — G · P0 · S5 · 2–3 d · Deps: T-0190 · AC: 5-person test (personas Priya/Rajan); findings filed; P0 UX fixes scheduled.

### 10.H Docs / Operations (T-0291–T-0300)

- **T-0291** ADR process + template + seed ADRs — H · P0 · S1 · 2–3 d · Deps: — · AC: ADR-001 (AGPL verdict) + ADR-002 (monorepo) + ADR-003 (event backbone) recorded; review cadence defined.
- **T-0292** service READMEs — H · P0 · S3 · 2–3 d · Deps: — · AC: Every service: run/test/deploy instructions; verified by fresh checkout.
- **T-0293** runbooks library — H · P1 · S8 · 3–5 d · Deps: T-0292 · AC: Top 20 ops toils documented (deploys, rollbacks, replay, erasure, failover); drilled.
- **T-0294** customer onboarding docs — H · P1 · S7 · 2–3 d · Deps: — · AC: Site setup guide, camera checklist, FAQ; used by pilots.
- **T-0295** API docs portal (OpenAPI rendered) — H · P1 · S12 · 3–5 d · Deps: T-0025 · AC: Public + authenticated docs; examples; deprecation notices.
- **T-0296** release notes process + changelog — H · P1 · S7 · 2–3 d · Deps: T-0221 · AC: Auto-generated from conventional commits; customer-visible summaries; cadence.
- **T-0297** internal wiki + decision log — H · P0 · S1 · 2–3 d · Deps: — · AC: Wiki home, onboarding, decision log linked; ownership assigned.
- **T-0298** training material (CSM/partners) — H · P1 · S12 · 3–5 d · Deps: T-0294 · AC: Demo script, admin guide, troubleshooting guide; recorded walkthrough.
- **T-0299** GA readiness checklist (DEVOPS §11) tracked — H · P0 · S6 · 1–2 d · Deps: — · AC: Checklist live; owners per item; status reviewed weekly from S6.
- **T-0300** 52-week roadmap review + backlog grooming cadence — H · P1 · S26 · 1–2 d · Deps: — · AC: Quarterly review ritual; backlog re-prioritized with business; this document updated.

---

## Appendix — Roadmap Hygiene

- **Task IDs are stable.** New work enters the backlog with new IDs (T-0301+); completed tasks are never renumbered.
- **Priority drift control:** P0/P1/P2 definitions are fixed (beta/GA/enterprise). Re-prioritization requires the CTO + founder review (Sprint 26 cadence and quarterly reviews).
- **Effort confidence:** All efforts are mid-senior engineer, single-threaded days. Load factor: at 6 FTE with 80% utilization ≈ 38 d/sprint team capacity — sprint plans above assume that; recalibrate at each sprint review.
- **SLO regressions** (ARCHITECTURE §15) are sprint blockers; do not ship sprints with open SLO regressions.
- **Cross-document alignment:** every section references the governing decision (AD-xx/OD-xx/SD-xx) so an engineering manager can trace rationale back to the architecture docs.

*End of roadmap. Next revision trigger: any architecture decision log change or GA postmortem.*
