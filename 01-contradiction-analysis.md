# Deliverable 01 — Contradiction Analysis
**Project:** SyncCam AI — Edge-First AI Video Intelligence Platform
**Author:** Principal Software Architect · **Date:** 2026-08-01
**Inputs:** All 9 source documents (v1.0, 2026-07-30 → 2026-08-01)
**Status:** Complete — 18 findings (1 blocker, 3 major, 6 minor, 8 resolved-consistent), 1 registry gap

---

## 1. Purpose and Method

Every statement in the 9 source documents was cross-referenced against its sibling statements in the other 8 documents. Findings were verified against the actual file text with line-level citations. This document is the **decision register** for how the unified SRS (Deliverable 03) resolves each conflict.

Findings are graded:

| Severity | Meaning | Gate |
|---|---|---|
| **Blocker** | Contradicts staffing, repo layout, or a revenue decision; must be fixed in source docs before implementation tasks begin | Blocks Sprint 1 task assignment |
| **Major** | Affects scope, contracts, or test acceptance criteria | Must be resolved in the SRS; fix source doc at next edit |
| **Minor** | Wording/envelope discrepancy; no execution impact if canonical value is documented | Resolve in SRS only |
| **Info / Consistent** | Suspected conflict, verified as consistent after analysis | Record to prevent future churn |

---

## 2. Precedence Rules (used to resolve every conflict)

The source documents themselves establish the hierarchy (roadmap preamble + decision logs):

1. **Decision logs win over prose:** AD-01…AD-17, OD-01…OD-16, SD-01…SD-08, D1–D6.
2. **Later documents win over earlier:** a doc dated 08-01 supersedes a 07-30 statement (docs were written as extensions, not revisions).
3. **Operational documents win over planning documents:** DEVOPS (how it runs) and BACKEND (how it's built) prevail over roadmap prose and PRD intent where the difference is a factual number.
4. **Where docs are silent, the SRS (Deliverable 03) becomes the canonical statement** and this register records the decision.

---

## 3. Confirmed Contradictions

### C-01 [BLOCKER] Edge agent runtime: Go vs Python
| | |
|---|---|
| **A** | `ARCHITECTURE.md` — §7.1/§3 (service table): "Edge Agent (Go)"; §22.3: "single-binary edge agent" (Go rationale); `DEVOPS-MLOPS` §3.3: "edge-agent (Go)", §3.4 process model |
| **B** | `ENGINEERING-EXECUTION-ROADMAP` §1 repo layout (line 80): `edge/  # Edge agent (Python) + model runtime packaging`; T-0151 (line 838): "edge agent scaffold (Python, service infra)"; §7 standards (line 407): "Python (AI/edge) 3.11" |
| **Resolution** | **Go.** Canonical: `edge-agent` = Go single-binary; `vision-engine`/`face-engine` = Python+Triton (separate processes per DEVOPS §3.4). |
| **Rationale** | AD/OD decision logs and the authoritative edge process model (DEVOPS §3.4) define the agent as Go. Go is required for the agent's concurrency profile (8–32 streams/box), small memory footprint, and single-binary OTA distribution (ARCHITECTURE §22.3). The roadmap repo layout, T-0151, and the standards table are stale. |
| **Impact** | Repo layout line 80, T-0151 (task rewrite: Go scaffold), standards table row "Python (AI/edge)", staffing plan (edge-agent = Go engineer, not Python). Hiring bar and T-0151 ACs must change. |

### C-02 [MAJOR] Python runtime version: 3.11 vs 3.12
| | |
|---|---|
| **A** | Roadmap §7 standards (line 407): "Python (AI/edge) 3.11" |
| **B** | DEVOPS §5.2 base images (line 277): `python:3.12-slim`; BACKEND §13.2 (line 1283): `python:3.12-slim` |
| **Resolution** | **3.12** for all AI-plane services. |
| **Rationale** | The base-image list is the operational truth (two independent docs agree); roadmap standards table is the outlier. 3.12-slim is also the distroless-adjacent default for Python services. |
| **Impact** | Roadmap §7 table edit; CI image pins must be 3.12; Triton client images built on 3.12. |

### C-03 [MAJOR] MVP module scope: three different lists
| | |
|---|---|
| **A** | PRD §10 (lines 200–206): MVP models = **9 families** (weapon, fire/smoke, intrusion, restricted-zone, PPE, fall, fight, loitering, face-attendance); out of MVP: LPR, vehicle tracking, crowd, ReID |
| **B** | AI-ARCHITECTURE §7.6 (line 482): MVP = **12 engines** (shared detector incl. vehicle class, pose, face stack, fire/smoke classifiers, **all logic modules incl. abandoned object**, camera health) |
| **C** | BUSINESS §9 (line 545): private beta = **9 modules** (matches A) |
| **D** | Roadmap S5 deliverables (line 185): PPE, fall, fight, loitering, face stack, ByteTrack, camera health — **no abandoned object** |
| **Resolution** | Canonical: **12 MVP engines per AI-ARCHITECTURE §7.6** — the engine count (not model-family count) drives engineering. PRD §10 and BUSINESS §9 lists are model-**family** groupings and are updated to add **Abandoned Object** and the vehicle class note (see C-04). |
| **Rationale** | AI-ARCHITECTURE is the authoritative module engineering document (D1–D6 live there). "9 model families = 12 engines" is a pure counting-model mismatch, except the abandoned-object omission, which is a genuine scope difference: AI-ARCH includes it, roadmap/PRD/BUSINESS omit it. |
| **Impact** | PRD §10 and BUSINESS §9 edited; roadmap S5 add T-ref for abandoned object (or ADR notes it as MVP-optional; recommended: keep in MVP — it's a zero-model logic module, ~2–4 ms on trigger, AI-ARCH §3.12). |

### C-04 [MAJOR] Vehicle Detection: in or out of MVP?
| | |
|---|---|
| **A** | PRD §10: vehicle detection **not listed** in MVP models; FR-103 (vehicle detection) exists in the FR-100 series |
| **B** | AI-ARCHITECTURE §3.4: shared backbone includes vehicle classes, "Edge Deployment: **Everywhere**" (zero extra cost); §7.6 MVP = shared detector "person/weapon/PPE/**vehicle**/fire-hotspot" |
| **Resolution** | **In MVP as event-only:** vehicle class ships as part of the shared detector; detections are logged as events. Full FR-103 (class/color/speed) and FR-104 (vehicle tracking/ReID) remain out of MVP (FR-104 is Phase 2 everywhere). |
| **Rationale** | The vehicle class is free inside the shared backbone (D1); excluding the event stream while running the model would waste capability. But FR-103's full feature set (speed estimation via homography calibration) is not MVP scope. |
| **Impact** | SRS FR-103 split: FR-103a (event-only, MVP) / FR-103b (full, GA). PRD §10 gains a "vehicle class (event-only)" line. |

### C-05 [MINOR] Plan B retention tier (15 days) vs canonical retention tiers (7/30/90/365)
| | |
|---|---|
| **A** | BUSINESS §4 Plan B (line 205): "15-day retention" |
| **B** | DEVOPS §8.4 OD-08 lifecycle (line 916): "tenant-configurable retention (7/30/90/365d)"; roadmap T-0032 (line 711): "30/90/365-day tenant config"; SECURITY §3.5: per-tenant retention |
| **Resolution** | Retention is **tenant-configurable over 7–365 days**; UI presets = 7/15/30/90/365. Plan B's 15d is a preset, not a new tier class. |
| **Rationale** | The OD-08 lifecycle list is illustrative, not exhaustive; pricing plans may reference any preset. No schema impact (retention is a per-tenant setting). |
| **Impact** | DEVOPS line 916 wording generalized; SRS states the preset list explicitly. |

### C-06 [MINOR] Archive bitrate: 1 Mbps vs 1–2 Mbps
| | |
|---|---|
| **A** | ARCHITECTURE §6.3 (line 402): cloud archive "~1–2 Mbps when enabled" |
| **B** | BACKEND §12.1 (line 1241): "720p H.265 ≈ 1 Mbps ≈ 10.8 GB/cam/day"; DEVOPS §8.4 (line 797): "10.8 GB/cam/day" |
| **Resolution** | Canonical: **≤1 Mbps nominal = 10.8 GB/cam/day**; 1–2 Mbps is the burst envelope (motion scenes), never a planning number. |
| **Rationale** | Two independent docs compute the same number (1 Mbps × 86400 s = 10.8 GB — internally consistent); the ARCHITECTURE range is a design envelope and would double the cost model if used for planning. |
| **Impact** | ARCHITECTURE §6.3 reworded to "≤1 Mbps nominal (10.8 GB/cam/day), 1–2 Mbps burst". |

### C-07 [MINOR] MVP dashboards: "3" vs the FR-207 suite
| | |
|---|---|
| **A** | PRD §10 (line 202): "3 dashboards (Ops, Safety, Attendance)" |
| **B** | UX §5.1: role-aware 12-column grid + per-vertical templates (FR-207); ARCHITECTURE service table: analytics-svc dashboards per persona |
| **Resolution** | MVP ships **3 dashboards built on the FR-207 template infrastructure**; additional personas/verticals are template configurations, not new builds. |
| **Rationale** | FR-207 explicitly requires per-vertical templates; "3 dashboards" is the MVP content count. No conflict in execution. |
| **Impact** | SRS states: FR-207 infrastructure in MVP; 3 seeded dashboard configs (Ops, Safety, Attendance); CSO/SOC/report dashboards at GA. |

### C-08 [MINOR] Seed roles: 4 vs 5 vs 7+
| | |
|---|---|
| **A** | PRD FR-204 (line 95): 4 roles (Super Admin, Operator, Auditor, Viewer) |
| **B** | Roadmap T-0004 (line 683): "**5** seed roles (PRD §4 personas)" |
| **C** | ARCHITECTURE §12.2 (line 762): 5-column matrix (+ Site Admin) |
| **D** | SECURITY §2.3 (lines 198–200, 244): 7 system roles (+ Supervisor, Guard, HR Manager); UX §5.16 (line 770): 8 presets (+ Reception) |
| **Resolution** | Canonical: **5 MVP seed roles** = Super Admin, Site Admin, Operator, Auditor, Viewer (ARCHITECTURE §12.2 + T-0004). Supervisor, Guard, HR Manager, Reception ship as **presets (UX §5.16 gallery)** before GA. PRD FR-204 edited to the 5-role seed list. |
| **Rationale** | Role matrices disagree only on seed-set size; least-privilege presets cover the rest. T-0004's citation "PRD §4 personas" is wrong twice over: §4 is personas (people), roles are in FR-204. |
| **Impact** | PRD FR-204 edited; T-0004 citation fixed; SECURITY/UX remain canonical for the full preset gallery. |

### C-09 [MINOR] Fight-detection precision: ≥90% target vs 0.85–0.92 band
| | |
|---|---|
| **A** | PRD §9: ≥90% precision for weapon/fall/fight |
| **B** | AI-ARCHITECTURE §2 (line 60): fight prec/rec 0.85–0.92 (RWF-2000) |
| **Resolution** | PRD §9 is the **eval gate**; the AI-ARCH band is the pre-tuning expectation. Fight must be tuned to ≥0.90 precision before registry promotion; documented as an accepted risk with retrain trigger. |
| **Rationale** | Consistent with the precision-gate mechanism (SECURITY §4.6: FP-rate gates; roadmap §7.5 eval gate). |
| **Impact** | TDD gains a fight-precision regression gate; eval card target set to prec ≥0.90. |

### C-10 [MINOR] Fire/smoke alert latency: ≤5s vs ≤3s p95
| | |
|---|---|
| **A** | PRD FR-113: "≤5s alert" for fire/smoke; FR-111 fall: "escalate within 5s" |
| **B** | Global SLO (DEVOPS §7.1 line 661, ARCHITECTURE G1/§15.2): "Detection→alert latency ≤3s p95" |
| **Resolution** | **≤3s p95 is the SLO**; FR-113/FR-111's ≤5s is the absolute ceiling (p99), never a target. |
| **Rationale** | The docs distinguish "target" (SLO) from "ceiling" (FR text) inconsistently; the SLO table is the measured commitment. |
| **Impact** | SRS records both values with explicit target/ceiling semantics. |

### C-11 [INFO — CONSISTENT] Kinesis shard ladder: "4 nominal" vs 1–2 / 2–4
| | |
|---|---|
| **A** | BACKEND §12.1 (line 1234): "4 shards nominal, scale to 20 (10× headroom)" @ 1,000 ev/s (10k cams) |
| **B** | DEVOPS §8.1 (line 723): 1–2 shards @ 10 ev/s (100 cams); §8.2 (line 736): 2–4 shards @ 100 ev/s (1,000 cams); §8.3: 4→20 @ 1,000 ev/s |
| **Resolution** | Consistent tier ladder; "4 nominal" = the 10k-camera production default. SRS adds an explicit **ev/s → shards mapping table** to remove ambiguity. |

### C-12 [INFO — CONSISTENT] Heartbeat cadence vs 90s offline detection
| | |
|---|---|
| **A** | ARCHITECTURE §4.2 (line 243) + DEVOPS §3.2 (line 264): heartbeat every 10–30s |
| **B** | Roadmap T-0009 (line 688): "stale-device detection flags offline in **< 90 s**" |
| **Resolution** | Consistent: heartbeat every 10–30s; a device is flagged offline when `last_heartbeat` age exceeds **90s** (3–9 missed beats). SRS records the staleness rule explicitly. |

### C-13 [INFO — CONSISTENT] SQS/SNS vs RabbitMQ (dual backbone paths)
| | |
|---|---|
| **A** | ARCHITECTURE AD-03: "Kinesis (hot) + MSK (replay) + SQS/SNS backbone" |
| **B** | BACKEND AD-11 (line 1082): RabbitMQ for **task queues**; "SQS/SNS from ARCHITECTURE §4.2 remain for the Kinesis→alert fast path and channel fan-out" |
| **Resolution** | Extension, not contradiction. Canonical taxonomy: **event stream = Kinesis/MSK; task queues = RabbitMQ; alert fast-path + fan-out = SQS/SNS**. SRS states the single taxonomy; provisioning task T-0232 already references BACKEND §8. |

### C-14 [INFO — CONSISTENT] ByteTrack MOTA: "78–83 / IDF1 75" vs "~80%"
| | |
|---|---|
| **A** | AI-ARCH §2 (line 44) + §3.2 (line 96): MOTA 78–83, IDF1 75 (MOT17) |
| **B** | AI-ARCH §7.1 (line 443): "MOT17 MOTA ~80%" |
| **Resolution** | Same band (78–83 ⊇ ~80). No action; SRS cites the band, IDF1 75, and "handoff accuracy ≥0.80" (vehicle). |

### C-15 [INFO — CONSISTENT] Module count: 23 modules + camera-health bonus
| | |
|---|---|
| **A** | AI-ARCH D1 (line 28) + roadmap T-0150 (line 834): "23 modules" |
| **B** | AI-ARCH §2 (line 67): "**Bonus** (in MVP but not in the module list): Camera Health (FR-116)" |
| **Resolution** | Consistent: registry = **23 modules + camera-health bonus = 24 capabilities**. The module-registry manifest task (T-0150) counts 23 + bonus entry. No change. |

### C-16 [GAP — not a contradiction] FR-115 Occupancy has no module row in the AI registry
| | |
|---|---|
| **Finding** | FR-115 (Occupancy Analytics — real-time + historical occupancy, heatmaps, capacity alerts) is a PRD core requirement; UX §5.12/§5.13 and BACKEND (Timescale aggregates, `analytics.occupancy` topic) reference it; **AI-ARCHITECTURE §2 has no Occupancy module row** (Crowd Detection exists; occupancy is derivable from person tracks). |
| **Resolution** | Not a contradiction — a registry completeness gap. Carried to Deliverable 11 (Missing Features): add an Occupancy module spec (logic: zone person-count from tracks; CSRNet-lite optional for density) with eval gates. The SRS includes FR-115 with the track-derived design. |

### C-17 [INFO — CONSISTENT] Edge heartbeat SLO coverage window (60s) vs cadence (10–30s)
| | |
|---|---|
| **A** | DEVOPS §7.1 (line 665): "Edge heartbeat coverage ≥99.5% of fleet within 60s" |
| **B** | ARCHITECTURE §4.2: heartbeat every 10–30s |
| **Resolution** | Consistent: the 60s coverage window ≥ max cadence (30s) with margin for processing. SRS states the coverage definition: % of fleet with a heartbeat age ≤60s at any sample point. |

### C-18 [MINOR] T-0004 cites "PRD §4 personas" for the role seed set
| | |
|---|---|
| **Finding** | Roadmap T-0004 (line 683) says "5 seed roles (**PRD §4 personas**)". PRD §4 defines personas (people: Ravi, Priya, Rajan, Kavya, Arun…); roles are FR-204. Also "5" ≠ PRD FR-204's 4. |
| **Resolution** | Citation fixed to "PRD FR-204 + ARCHITECTURE §12.2"; seed set = 5 (see C-08). |

---

## 4. Verified-Consistent Findings (no action required)

Checked and confirmed consistent across all 9 docs. Recorded to prevent future re-litigation:

| # | Subject | Values verified |
|---|---|---|
| V-1 | DR targets | RTO ≤60 min, RPO ≤5 min everywhere (ARCHITECTURE §19/AD-08, DEVOPS §9, SECURITY §6.7, roadmap T-0244/O-1/O-2); Aurora Global DB RPO <1s in-region promotion |
| V-2 | Availability | 99.9% platform (PRD §9, ARCHITECTURE §15.2, DEVOPS §7.1, BACKEND §15.2, BUSINESS plans A–D); Plan E = custom SLA |
| V-3 | Region pairs | ap-south-1↔ap-southeast-1, us-east-1↔us-west-2, eu-central-1↔eu-west-2 (ARCHITECTURE §19, DEVOPS §9, roadmap S24) |
| V-4 | Pricing | Plans A–E, India $3.99/5.99/8.99/12.99, US $18/26/39, E = 15–25% premium (BUSINESS §4/§9/§10; consistent in PRD NFR refs) |
| V-5 | AGPL / D6 | Consistent: risk T-1 (likelihood 5 × impact 5), Week-1 decision, ADR-001, Ultralytics enterprise license (~$3–5k/yr, [ASSUMPTION] A23) OR Apache-2.0 swap (RT-DETR/D-FINE); CI license allowlist; eval-svc license gate (AI-ARCH D6, roadmap T-0256/T-1/B-5, BUSINESS §12.6, DEVOPS §7.4, SECURITY model table) |
| V-6 | FR numbering | FR-101…FR-119 = detection engines; FR-201…FR-207 = platform; all cross-doc references (BACKEND store table, ARCHITECTURE §5 mapping, UX screens) resolve correctly |
| V-7 | Personas | Ravi, Priya, Rajan, Kavya, Arun, Vikram, Dr. Anand — consistent across PRD §4, UX §2, BUSINESS §5, roadmap (T-0267, T-0290) |
| V-8 | Edge hardware tiers | S/M/L (BUSINESS §8, ARCHITECTURE §7, DEVOPS golden-image tiers, BACKEND `edge_devices.hw_tier s/m/l`) |
| V-9 | Retention defaults | KVS 30d, edge local 30d, evidence 30d default, snapshots 30d, debug bundles 90d (DEVOPS §8.4/OD-08, BACKEND §12.1, ARCHITECTURE §6.3) — consistent |
| V-10 | Biometric retention | Embeddings default "employment + 30d", attendance 90d hot; tenant-configurable 7–365d (SECURITY §3.6) — defaults vs configurability, not a conflict |
| V-11 | Audit chain | 7y retention for audit/evidence (SECURITY §2.5, §3.2); ClickHouse 7y default (BACKEND §3.4); hash chain SHA-256 format consistent (BACKEND §10.1 == SECURITY §3.2) |
| V-12 | Store-and-forward | 10-min offline = no loss (roadmap T-0154); eviction oldest-first; edge holds 30d local; cloud is the backup (ARCHITECTURE §7.2/§19, DEVOPS §3.4) |
| V-13 | No-silent-loss | DLQs + alerting + daily reconciliation everywhere (ARCHITECTURE §4.3/§6, BACKEND §8.5/§11.4, DEVOPS §3.5/§7.1) |
| V-14 | Camera-count tiers | 100 → 1k → 10k → 100k (DEVOPS §8) with BACKEND §12.1 capacity math at 10k (Aurora r7g.4xl, ClickHouse 3×2 r6g.4xl, OpenSearch 6–9 nodes, MSK 3 brokers) |
| V-15 | MSK scaling | Partition-only scaling 16→128, topic retention 7–30d, S3 mirror (BACKEND §8, DEVOPS §8.3) |
| V-16 | Silo triggers | Dedicated per-tenant resources >2,000 cams (ARCHITECTURE §17.2); tenant-silo Aurora shards >20k cams / >5k writes/s (BACKEND §12.4, DEVOPS §8.4) — different mechanisms, both documented |
| V-17 | Unit economics | $1M ARR / 18 mo; ≥25 customers; GM path 55→67→72%; 70% = Year-2 covenant; India SMB capped 35% of Year-2 revenue; edge rental amortized 36 mo (BUSINESS §6/§11 — internally consistent, single author) |
| V-18 | Analytics engines | Crowd density (FR-114), heatmaps, dwell: Phase 2 in ARCHITECTURE service table == PRD §11 == DEVOPS → consistent |
| V-19 | Triton + TRT INT8 | Single inference runtime, INT8/FP16, 5–10 FPS sampling, ROI gating (AI-ARCH §3, DEVOPS OD-11/OD-12, ARCHITECTURE §7) |
| V-20 | Identity | Cognito + Keycloak local-only, same token contract (ARCHITECTURE §10/§22, roadmap T-0001, DEVOPS) |
| V-21 | Security invariants | S1–S8, zero-trust zones Z1–Z5, STRIDE T1–T18, SD-01…SD-08 — single-author consistency within SECURITY; cross-referenced values (KMS, RLS, field encryption) match BACKEND/ARCHITECTURE |
| V-22 | Compliance pack list | GDPR, DPDP, CCPA/CPRA, BIPA + state packs, Law 25, AI Act, HIPAA guidance — PRD §15.4 subset == SECURITY §5 list (PRD is the summary, SECURITY the authority) |
| V-23 | MVP timeline | PRD "3–4 months to private beta" == BUSINESS "months 0–4, 8 design partners" == roadmap "weeks 1–12, private beta from S7" |
| V-24 | GA timing | BUSINESS "GA month 5–6" == roadmap "GA week 24 (S12)" |
| V-25 | False-positive target | ≤1 FA/5 cams/day: PRD §9 == AI-ARCH §2 zone-intrusion eval == UX severity language |

---

## 5. Required Source-Document Edits (summary)

| Doc | Edits required | Blocking? |
|---|---|---|
| **ENGINEERING-EXECUTION-ROADMAP** | §1 line 80 `edge/` → Go; T-0151 → Go scaffold; §7 line 407 Python 3.12; S5 deliverables + abandoned object; T-0004 citation + 5-role list | C-01 blocks task assignment |
| **PRD** | §10 add abandoned object + vehicle-class note; FR-204 → 5 seed roles; FR-113/FR-111 latency as ceiling note; §10 dashboards note (3 seeded on FR-207) | No |
| **ARCHITECTURE** | §6.3 archive "≤1 Mbps nominal (10.8 GB/cam/day), 1–2 Mbps burst" | No |
| **BUSINESS** | §9 private-beta module list + abandoned object; Plan B 15d → retention preset note | No |
| **DEVOPS** | §8.4 line 916 retention presets 7/15/30/90/365 | No |
| **AI-ARCHITECTURE** | Optional: add Occupancy module row (§2) for FR-115 (else documented in SRS) | No |
| **SECURITY** | None | — |
| **BACKEND** | None | — |
| **UX-DESIGN** | None | — |

## 6. New ADRs to record (via roadmap ADR process, T-0291)

| ADR | Title | Resolves |
|---|---|---|
| ADR-004 | Edge agent runtime = Go; vision/face engines = Python (Triton client) | C-01 |
| ADR-005 | MVP scope = 12 engines (AI-ARCH §7.6); FR-103 split (event-only in MVP) | C-03, C-04 |
| ADR-006 | Retention presets 7/15/30/90/365; any 7–365d configurable | C-05 |
| ADR-007 | Role taxonomy: 5 seeds + 3 presets pre-GA; seed list = Super Admin, Site Admin, Operator, Auditor, Viewer | C-08 |

*ADR-001 (AGPL verdict), ADR-002 (monorepo), ADR-003 (event backbone) already seeded per T-0291.*
