# Deliverable 02 — Deduplication Map
**Project:** SyncCam AI · **Date:** 2026-08-01
**Purpose:** Identify every fact duplicated across the 9 source documents, name the **canonical home** for each, and define the merge disposition for the unified SRS (Deliverable 03).

---

## 1. Principle

The 9 documents were authored as a **layered series** (each extends the prior: PRD → ARCHITECTURE → AI → BACKEND → UX → SECURITY → DEVOPS → BUSINESS → ROADMAP). Duplication is therefore intentional — later docs re-state earlier decisions so they read standalone. The SRS does **not** delete the source docs; it becomes the single normative reference and the source docs become design rationale (like an ADR library).

**Dedup rule applied in the SRS:** every fact has exactly one normative statement in the SRS, with provenance notes pointing to the source doc/section for rationale.

---

## 2. Duplication Map

| # | Fact / artifact | Duplicated in (doc §) | Canonical home | Disposition in SRS |
|---|---|---|---|---|
| D-1 | **SLO / target table** (99.9%, ≤3s p95, ≥1,000 ev/s, ≤10s push, ≥99.5% notify, ≥99.5% heartbeat, ≥99.9% dossier) | ARCHITECTURE §15.2, DEVOPS §7.1, BACKEND §15.2, PRD §9, roadmap §9 acceptance lines | **DEVOPS §7.1** (measurable, with burn-rate alerting) | SRS NFR-100 series; SLO table imported verbatim from DEVOPS; others cite it |
| D-2 | **Service catalog** (23 services, owner plane, language) | ARCHITECTURE §3, BACKEND §13.1 (resource sizing), DEVOPS §5.3 (HPA), roadmap §1 (repo layout) | **ARCHITECTURE §3** | SRS §Service Catalog: names, planes, languages, FR links |
| D-3 | **Store portfolio** (Aurora/Timescale, DynamoDB, ClickHouse, Redis, OpenSearch, S3, KVS, SQLite/RocksDB edge) | ARCHITECTURE §8, BACKEND §2, DEVOPS §3.1, PRD §11 | **BACKEND §2 + §3** (schemas, keys, retention) | SRS §Data Stores: table + schema pointer |
| D-4 | **Event/topic catalog** (detection types, topics, retention, partition keys) | ARCHITECTURE §4.2, BACKEND §8 (Kafka topics + Kinesis partition map), DEVOPS §3.5, SECURITY §1.3 | **BACKEND §8** (+ ARCHITECTURE §4.2 for backbone rationale) | SRS §Event Backbone: single topic table incl. RabbitMQ/SQS taxonomy (C-13) |
| D-5 | **Role matrix / permissions** | PRD FR-204, ARCHITECTURE §12.2, SECURITY §2.3 (MFA tiers), UX §5.16 (presets), BACKEND §4.2 (MFA roles), UX per-screen permission notes | **ARCHITECTURE §12.2** (+ UX §5.16 for UI presets, SECURITY §2.3 for MFA) | SRS §RBAC: 5 seed roles + preset gallery; per-screen permissions stay in UX |
| D-6 | **Security invariants + zones** (S1–S8, Z1–Z5, STRIDE T1–T18) | SECURITY §1–§2; ARCHITECTURE §14 (subset); DEVOPS §6 (SecOps subset) | **SECURITY** | SRS references SECURITY as normative; only invariant summary + threat-to-control matrix in SRS |
| D-7 | **Compliance pack list** | PRD §15.4 (checklist), SECURITY §5 (full packs), BUSINESS §10 (sales positioning), roadmap §10 (task hooks) | **SECURITY §5** | SRS §Compliance: per-region table; PRD remains the executive summary |
| D-8 | **DR design + targets** (RTO ≤60min, RPO ≤5min, region pairs, failover SLAs) | ARCHITECTURE §19, DEVOPS §9, SECURITY §6.7, BACKEND §12.6, roadmap S11/S24 | **ARCHITECTURE §19** (design) + **DEVOPS §9** (ops runbook) | SRS §DR: targets + pairs; runbook pointer |
| D-9 | **Scaling tiers / capacity math** (100/1k/10k/100k cameras, node counts, shard ladders) | BACKEND §12 (10k math), DEVOPS §8 (tier tables), ARCHITECTURE §17 (strategy) | **DEVOPS §8** (operational tiers) + **BACKEND §12.1** (capacity justification) | SRS §Scaling: single tier table; BACKEND math as appendix |
| D-10 | **Cost model** (per-cam/mo by tier, storage add-ons, KVS guardrails) | DEVOPS §8.5, BUSINESS §6 (unit economics), BUSINESS §4 (pricing), roadmap T-7 risk | **BUSINESS §6** (unit economics) + **DEVOPS §8.5** (engineering cost guardrails) | SRS references BUSINESS; engineering cost invariants (OD-12) restated |
| D-11 | **Edge process model + hardware tiers** (agent/engines/watchdog, S/M/L, golden image) | ARCHITECTURE §7, DEVOPS §3.3–3.5, BUSINESS §8 (sizing), BACKEND `edge_devices.hw_tier` | **DEVOPS §3.3–3.5** (ops) + **ARCHITECTURE §7.1** (design) | SRS §Edge Runtime: process model + tier matrix |
| D-12 | **AI module registry + model choices** | AI-ARCH §2 (registry), SECURITY §4.3 (model monitoring table), roadmap §7.5 (eval), BACKEND `model_versions` schema | **AI-ARCHITECTURE §2** | SRS references registry; eval gates in TDD |
| D-13 | **AI decision log D1–D6** | AI-ARCH §1; roadmap T-1/T-0256 (AGPL); BUSINESS §12.6; SECURITY model table; DEVOPS license gate | **AI-ARCHITECTURE §1** | SRS cites D1–D6 + ADR status |
| D-14 | **MVP scope** (modules, platform, out-of-MVP) | PRD §10, AI-ARCH §7.6, BUSINESS §9, roadmap S1–S6, UX shell choices | **PRD §10** (scope) + **AI-ARCH §7.6** (engine count, C-03/C-04 applied) | SRS §MVP Boundary: single list of 12 engines + platform scope |
| D-15 | **NFR latency/accuracy targets** (≤3s, ≥95/≥90 precision, ≤1 FA/5 cams) | PRD §9, ARCHITECTURE G1–G3, AI-ARCH §3 (expected accuracy), DEVOPS SLOs, UX severity copy | **PRD §9** (targets) + **AI-ARCH §3** (module-level bands) | SRS NFR + module accuracy table (C-09/C-10 applied) |
| D-16 | **Retention matrix** (store × retention × lifecycle) | BACKEND §3 (per-store), DEVOPS §8.4 (OD-08 lifecycle), SECURITY §3.5–3.6, BUSINESS §4 (plan tiers), ARCHITECTURE §6.3, roadmap T-0032/T-0238 | **DEVOPS §8.4** (operational) + **BACKEND §3** (schema TTL) | SRS §Retention: single matrix, presets 7/15/30/90/365 (C-05) |
| D-17 | **Erase/erasure workflows** (right-to-erase, manifests, legal hold) | SECURITY §3.7, BACKEND §11.3, DEVOPS §8.4, UX §5.15 (UI), roadmap T-0032 | **SECURITY §3.7** (policy) + **BACKEND §11.3** (mechanics) | SRS pointer + checklist |
| D-18 | **Audit log scope + retention** (what's audited, 7y, hash chain) | BACKEND §10, SECURITY §2.5/§3.2, ARCHITECTURE §15.6, UX §5.14 | **BACKEND §10** (format + hash chain) + **SECURITY §3.2** (retention/legal) | SRS §Audit: one table |
| D-19 | **Notification channels + severity routing** (push/SMS/WhatsApp/email/webhook, severities, flood control) | PRD FR-118, ARCHITECTURE §4.4, BACKEND §6/§8 (notify topics), UX §5.9, SECURITY §2.6 (flood) | **BACKEND §6** (API/contracts) + **ARCHITECTURE §4.4** (flood control) | SRS §Notify: channel matrix + severity mapping (P1–P4 ↔ Critical–Low) |
| D-20 | **Deployment ladder / readiness gates** (preview → staging → prod; canary; rollback) | DEVOPS §5 (CI/CD + canary), roadmap §8 (phases), ARCHITECTURE §21 (ops) | **DEVOPS §5** | SRS §Deployment Model |
| D-21 | **Glossary entries** (120+ terms) | ARCHITECTURE §23.4, BACKEND §16.3, DEVOPS §12.3, SECURITY §9.3 | **Deliverable 16 (Glossary)** | Consolidated once |
| D-22 | **Personas** | PRD §4, UX §2 (role screens), BUSINESS §5 (segments), roadmap (task refs) | **PRD §4** | SRS persona summary; UX owns per-screen behavior |
| D-23 | **Silo/large-tenant handling** (dedicated shards >2k, silo Aurora >20k/5k wps) | ARCHITECTURE §17.2, BACKEND §12.4, DEVOPS §8.4 | **BACKEND §12.4** | SRS §Multi-tenancy |
| D-24 | **Privacy data classes** (raw video / embeddings / metadata / biometric scope) | SECURITY §2 (zones), ARCHITECTURE §12.3, BACKEND (scope claims), UX §5 (privacy UI) | **SECURITY §2** (data classes) + **ARCHITECTURE §12.3** (scope/ABAC) | SRS §Data Classes: single table |
| D-25 | **OIDC/token/MFA mechanics** | ARCHITECTURE §10–11, BACKEND §4, SECURITY §2.3, roadmap T-0001–T-0003 | **BACKEND §4** (contracts) + **ARCHITECTURE §11** (flows) | SRS §Auth: flow summary + contract pointer |

---

## 3. Duplication by Design (retained, with pointers)

| Duplicate | Kept because | SRS treatment |
|---|---|---|
| SLOs in 4 docs | Each doc must stand alone for its audience | SRS NFR table is the *only* normative copy; source docs note "superseded by SRS §NFR" |
| Service catalog in 5 docs | Repo layout (roadmap), sizing (BACKEND), HPA (DEVOPS), personas (ARCHITECTURE) | SRS service table + per-doc role column |
| Role permissions in 6 docs | UX needs screen-level detail | SRS RBAC table + pointer to UX §5.16 gallery |
| Cost figures | Engineering (DEVOPS) vs commercial (BUSINESS) lenses differ | Keep both; SRS notes which lens applies where |
| Compliance packs | Sales (BUSINESS), engineering (SECURITY), summary (PRD) | SRS compliance matrix; SECURITY normative |

---

## 4. What the SRS Will NOT Re-State

To keep the SRS operational, the following live only in their source docs (SRS references them):

- Mermaid diagrams of internals → DEVOPS/ARCHITECTURE (SRS holds the canonical 8 diagrams of Deliverable 18)
- Full SQL schemas → BACKEND §3
- Full API/WS/Webhook contracts → BACKEND §5–§7
- STRIDE threat detail → SECURITY §1.4
- Market analysis/competitors → BUSINESS §5
- Sprint-by-sprint task text → roadmap (SRS maps FR ↔ milestone ↔ task)
- UX screen-by-screen specs → UX-DESIGN

---

## 5. Merge Execution Checklist (for Deliverable 03)

- [ ] Build SRS skeleton: Overview → Personas → MVP Boundary → FR-100 (19 engines + modules) → FR-200 (7 platform) → NFRs/SLOs → Architecture summary → Data/backbone → Auth/RBAC → Security/Compliance → Retention → DR → Scaling → Test strategy pointer
- [ ] Apply all 18 findings from `01-contradiction-analysis.md` (resolutions C-01…C-18)
- [ ] Apply every canonical-home rule in §2 table (D-1…D-25)
- [ ] Tag each SRS requirement with source provenance `[PRD FR-xxx]`, `[ARCH §y]`, `[AI-ARCH §z]`
- [ ] Record decision register links (ADR-001…007 + this doc)
- [ ] Cross-check traceability: every roadmap T-XXXX maps to ≥1 SRS requirement; every SRS requirement maps to ≥1 T-XXXX or explicit Phase-2 marker
