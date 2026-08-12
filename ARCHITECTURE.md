# SyncCam AI — Production-Grade System Architecture

**Document:** Architecture v1.0 (Draft for Review)
**Date:** July 30, 2026
**Source PRD:** `PRD-SyncCam-AI.md` (v1.0)
**Architectural posture:** Edge-first, event-driven, multi-tenant SaaS + on-prem, AWS multi-region from day 1.

---

## Table of Contents

1. [Design Principles](#1-design-principles)
2. [High-Level Architecture](#2-high-level-architecture)
3. [Microservices Architecture](#3-microservices-architecture)
4. [Event-Driven Architecture](#4-event-driven-architecture)
5. [AI Pipeline](#5-ai-pipeline)
6. [Video Processing Pipeline](#6-video-processing-pipeline)
7. [Edge Computing Architecture](#7-edge-computing-architecture)
8. [Cloud Architecture](#8-cloud-architecture)
9. [Database Architecture](#9-database-architecture)
10. [API Gateway Architecture](#10-api-gateway-architecture)
11. [Authentication Flow](#11-authentication-flow)
12. [Authorization](#12-authorization)
13. [Multi-Tenancy](#13-multi-tenancy)
14. [Security Architecture](#14-security-architecture)
15. [Monitoring](#15-monitoring)
16. [Logging](#16-logging)
17. [Scalability Strategy](#17-scalability-strategy)
18. [Backup Strategy](#18-backup-strategy)
19. [Disaster Recovery](#19-disaster-recovery)
20. [Deployment Topology](#20-deployment-topology)
21. [End-to-End Data Flow](#21-end-to-end-data-flow)
22. [Technology Recommendations](#22-technology-recommendations)
23. [Appendix](#23-appendix)

---

## 1. Design Principles

The architecture is governed by the following principles, derived directly from PRD requirements:

| # | Principle | PRD Driver |
|---|---|---|
| P1 | **Edge-first intelligence** — all real-time detection runs on customer-premise hardware; the cloud never blocks life-safety decisions | G1 (≤3s detection), §6 portability, §14 network instability |
| P2 | **Detection over surveillance** — the default output is events + metadata + evidence clips, not continuous human monitoring | §15 privacy principles |
| P3 | **Zero data loss tolerance** — at-least-once delivery with idempotent consumers and dedupe; store-and-forward on edge | §6 reliability NFR |
| P4 | **Privacy by design** — masks applied before encoding at the edge; biometric data segregated and separately encrypted; right-to-erasure native | §15, §16 compliance |
| P5 | **Multi-tenant by construction** — every service, table, bucket, and queue is tenant-scoped from day 1 | FR-206, §6 scalability |
| P6 | **Multi-region day 1** — region-parameterized IaC; data residency pinned per tenant (India/EU/US) | §15.7, §16 |
| P7 | **No vendor lock-in at the edge** — hardware abstraction layer above Jetson/x86/NPU; ONVIF/RTSP standards for cameras | §6 interoperability, §14 |
| P8 | **Observability is a feature** — SLOs, traces, and auditability are built in, not bolted on | §6 maintainability, §9 metrics |
| P9 | **Regulated evidence** — tamper-evident, hash-chained incident artifacts | FR-117, §16 |

---

## 2. High-Level Architecture

### 2.1 System Context (C4 Level 1)

```mermaid
flowchart TD
    subgraph Users
        OPS["SOC Operator / Safety Manager"]
        DIR["Facility Director / CSO"]
        HRM["HR Manager / Auditor"]
        ADM["Site IT / Super Admin"]
    end

    subgraph SyncCam
        PLATFORM["SyncCam AI Platform<br/>(edge agents + cloud control & analytics plane)"]
    end

    subgraph ExternalSystems
        CAM["CCTV Cameras<br/>(ONVIF / RTSP, existing hardware)"]
        AC["Access Control / HRIS / Payroll"]
        NOTIFY["SMS / WhatsApp / Email / Push"]
        WEBHOOK["Partner Webhooks"]
    end

    CAM -- "RTSP/ONVIF streams" --> PLATFORM
    OPS --> PLATFORM
    DIR --> PLATFORM
    HRM --> PLATFORM
    ADM --> PLATFORM
    PLATFORM -- "REST / Webhooks / file exports" --> AC
    PLATFORM -- "alerts" --> NOTIFY
    PLATFORM -- "event stream" --> WEBHOOK
```

### 2.2 Container Overview (C4 Level 2)

```mermaid
flowchart TD
    subgraph Edge["Customer Edge Site (per site)"]
        EDGE_AGENT["Edge Agent (Go)<br/>stream mgmt, rules engine, persistence"]
        VISION["Vision Engine (Python + TensorRT)<br/>weapon/fire/PPE/fall/fight/intrusion..."]
        FACE["Face Engine (ArcFace)<br/>attendance + liveness"]
        RING["Ring Buffer + Local Store<br/>(NVMe, evidence clips)"]
        MASK["Privacy Mask Pipeline<br/>(pre-encode redaction)"]
        WATCH["Watchdog + OTA Agent<br/>(Greengrass)"]
    end

    subgraph Cloud["Cloud Control & Data Plane (per region)"]
        GW["API Gateway (Kong)"]
        AUTH["Cognito / Keycloak"]
        CORE["Platform Services (Go)<br/>config, events, alerts, reports,<br/>playback, search, audit, billing"]
        AI["AI Services (Python)<br/>model registry, eval, cloud inference"]
        EVT["Event Backbone<br/>Kinesis + MSK + SQS/SNS"]
        VIDEO["KVS + S3 Video Archive"]
        DBs["Aurora / DynamoDB / OpenSearch / Redis"]
        RT["Realtime Gateway (WS)"]
    end

    CAM["Cameras (RTSP)"] --> EDGE_AGENT
    EDGE_AGENT --> MASK --> VISION
    EDGE_AGENT --> FACE
    VISION --> RING
    FACE --> RING
    RING -->|"fragments + metadata"| VIDEO
    EDGE_AGENT -->|"events (Kinesis Producer)"| EVT
    EDGE_AGENT <-->|"mTLS control"| GW
    WATCH <-->|"IoT Core / Jobs"| CORE
    EVT --> CORE
    CORE --> DBs
    CORE --> AI
    AI --> EVT
    RT --> CORE
    CORE --> VIDEO
    AUTH --> GW
    Users --> CloudFront --> GW
    Users --> RT
```

### 2.3 Architectural Themes at a Glance

- **Three planes:** (a) *Edge plane* — detection, masking, buffering, store-and-forward; (b) *Control plane* — configuration, identity, fleet management, rules; (c) *Data plane* — event stream, alerts, analytics, video archive, reports.
- **One backbone:** all intelligence flows as events over a Kinesis/Kafka backbone; services are loosely coupled consumers.
- **Two deployments:** fully-managed SaaS (edge + cloud) and local-only appliance mode (edge + embedded control plane) per PRD §10.
- **Region-locked data:** tenant data never leaves its pinned region by default (§15.7).

---

## 3. Microservices Architecture

Services are grouped into **control plane**, **data plane**, **AI plane**, and **platform infrastructure**. All services are stateless (state lives in the database/backbone), horizontally scalable, and speak gRPC internally / REST externally.

### 3.1 Service Catalog

| Service | Plane | Language | Responsibilities | PRD Ref |
|---|---|---|---|---|
| **identity-svc** | Control | Go | Users, tenants, roles, MFA, token issuance (via Cognito/Keycloak), SSO/SAML | FR-204 |
| **tenant-svc** | Control | Go | Tenant onboarding, site hierarchy, residency pinning, quotas, billing metadata | FR-206 |
| **config-svc** | Control | Go | Cameras, zones, rules, PPE matrix, thresholds, rule versioning; **source of truth pushed to edge** | FR-202, FR-203 |
| **device-svc** | Control | Go | Edge fleet registry, OTA (model + firmware), device health, store-and-forward status | FR-116, §6 |
| **audit-svc** | Control | Go | Immutable, hash-chained audit trail; privileged access reviews | §15.6, §16 |
| **event-svc** | Data | Go | Event normalization, validation, dedupe (idempotency), enrichment, Kinesis ingestion | §6 reliability |
| **alert-svc** | Data | Go | Severity classification, aggregation, mute/snooze, escalation policies, priority ordering | FR-118 |
| **notify-svc** | Data | Go | Multi-channel fan-out (push/SMS/WhatsApp/email/webhook) with per-channel rate limits & delivery receipts | FR-118 |
| **analytics-svc** | Data | Go | Occupancy, dwell, crowd density, heatmaps, time-series aggregates | FR-114/115, Phase 2 |
| **report-svc** | Data | Go | Incident dossiers (pre/post clips, timeline, confidence), PDF/CSV/JSON export, hash-chained evidence | FR-117 |
| **playback-svc** | Data | Go | Timeline, scrub, HLS session mgmt, WebRTC low-latency view, clip export | FR-201 |
| **search-svc** | Data | Go | Event/incident search; face & plate search (Phase 2 vector search) | Phase 2 |
| **integration-svc** | Data | Go | Webhooks, HRIS/payroll adapters, access-control adapters (HID/ZKTeco), insurer exports | FR-205, Phase 2 |
| **model-registry-svc** | AI | Python | Model versions, A/B, shadow mode, per-site calibration, rollback | §6 maintainability, §14 |
| **training-svc** | AI | Python | Dataset curation, SageMaker pipelines, retrain triggers from feedback | §14 drift mitigation |
| **eval-svc** | AI | Python | Benchmark suite per vertical, regression gates, drift monitoring | §9 accuracy metrics |
| **realtime-gw** | Infra | Go | WebSocket gateway for live ops view, alert push, dashboards | FR-207 |
| **api-gateway (Kong)** | Infra | — | Routing, JWT/RBAC scoping, rate limits, mTLS termination, webhook ingress | FR-205 |
| **billing-svc** | Infra | Go | Usage metering (per-camera licensing), quotas (Phase 2+) | G3 |

### 3.2 Service Topology

```mermaid
flowchart LR
    subgraph EdgePlane["EDGE PLANE (customer site)"]
        EA["edge-agent"] --> VE["vision-engine"]
        EA --> FE["face-engine"]
        EA --> RULE["rules-engine"]
    end

    subgraph ControlPlane["CONTROL PLANE"]
        ID["identity-svc"]
        TN["tenant-svc"]
        CF["config-svc"]
        DV["device-svc"]
        AU["audit-svc"]
    end

    subgraph DataPlane["DATA PLANE"]
        EV["event-svc"] --> AL["alert-svc"] --> NF["notify-svc"]
        EV --> AN["analytics-svc"]
        EV --> RS["report-svc"]
        EV --> SE["search-svc"]
        EV --> IN["integration-svc"]
        PB["playback-svc"]
    end

    subgraph AIPlane["AI PLANE"]
        MR["model-registry-svc"]
        TR["training-svc"]
        EV2["eval-svc"]
    end

    subgraph Backbone["EVENT BACKBONE"]
        KS["Kinesis (hot)"]
        MK["MSK Kafka (replay/analytics)"]
    end

    EA -->|"detections"| EV
    CF -->|"rule config"| EA
    DV -->|"OTA"| EA
    EV --> KS
    KS --> MK
    MK --> AN
    MK --> SE
    KS --> AL
    AL --> NF
    EV --> RS
    RS --> PB
    EV2 --> MR --> DV
    TR --> MR
    EV2 --> TR
```

### 3.3 Service Interaction Rules

- **No direct service-to-service REST calls** — inter-service communication goes through gRPC within a namespace, or through events on the backbone for anything cross-cutting.
- **Config-svc is the single writer** of camera/zone/rule state; edges converge to it, never diverge.
- **Event-svc is the single ingress** for detection events from the edge (one choke point for dedupe/enrichment).
- **Idempotency keys** (`event_id`, `tenant_id`, `camera_id`, `occurred_at` fingerprint) make all consumers safe under redelivery.

---

## 4. Event-Driven Architecture

### 4.1 Event Taxonomy

| Event Type | Producer | Consumers | Semantics |
|---|---|---|---|
| `detection` (typed: weapon, fire, ppe, fall, fight, intrusion, zone, loitering, occupancy, crowd, vehicle, lpr) | edge-agent | event-svc → alerts/analytics/search | at-least-once, deduped |
| `attendance` | edge-agent | event-svc → integration (HRIS) | at-least-once |
| `camera-health` | edge-agent/watchdog | device-svc → alerts | at-least-once |
| `device-heartbeat` | edge-agent | device-svc | every 10–30s |
| `alert-created / alert-routed / alert-ack` | alert-svc | notify-svc, realtime-gw, audit-svc | idempotent |
| `incident-created / incident-exported` | report-svc | integration-svc, audit-svc | exactly-once intent |
| `config-changed` | config-svc | device-svc → edge | command-driven |
| `model-deployed / model-rolled-back` | model-registry-svc | device-svc, eval-svc | command-driven |
| `face-search-requested` | search-svc | audit-svc (compliance log) | audit |

### 4.2 Backbone Topology

```mermaid
flowchart TD
    EDGE["Edge Agents (10k+ devices)"] -->|"Kinesis Producer<br/>(batch, retry, backoff)"| KDS["Kinesis Data Streams<br/>hot path, high throughput"]
    KDS -->|"KCL consumers"| EVENT["event-svc (dedupe/enrich)"]
    EVENT -->|"idempotent"| SQS_AL["SQS - alert queue"]
    EVENT -->|"idempotent"| SQS_AN["SQS - analytics queue"]
    EVENT -->|"replay + warehouse"| MSK["MSK Kafka<br/>durable log (7-30d)"]
    SQS_AL --> ALERT["alert-svc"]
    SQS_AN --> ANALYTICS["analytics-svc"]
    MSK --> SEARCH["search-svc"]
    MSK --> BATCH["Spark/Athena batch (reports,<br/>ML features)"]
    ALERT -->|"routed alert"| SNS["SNS topics<br/>(per severity, per channel)"]
    SNS --> NOTIFY["notify-svc adapters"]
    NOTIFY --> PUSH["APNs/FCM"]
    NOTIFY --> SMS["SMS (Twilio)"]
    NOTIFY --> EMAIL["Email (SES)"]
    NOTIFY --> WA["WhatsApp/Telegram"]
    NOTIFY --> WH["Webhook (partner)"]
    ALERT --> RTGW["realtime-gw (WebSocket)"]
    CLOUD_EVENTS["AWS EventBridge<br/>(schedules, <br/>cloud-side events)"] --> INTEG["integration-svc"]
```

### 4.3 Delivery Guarantees

- **Ingest:** ≥1,000 events/sec sustained (PRD NFR) — Kinesis scales to tens of thousands via shard addition.
- **Reliability model:** at-least-once everywhere + idempotent consumers + 5-minute Redis dedupe window keyed on `sha256(tenant|camera|event_type|occurred_at|frame_seq)`. This satisfies "at-least-once + dedupe" from PRD §6.
- **No silent loss:** DLQs with alerting on depth, dead-letter replay tooling, and a daily reconciliation job comparing edge counters vs cloud counters (`device-heartbeat` includes `events_sent`, `events_acked`, `events_buffered`).
- **Ordering:** per-camera ordering preserved via Kinesis partition key `camera_id`; cross-camera ordering is not required.
- **Backpressure:** edge uses adaptive batch + exponential backoff; on sustained failure it persists to local store (store-and-forward, §7).

### 4.4 Alert Flood Control (PRD risk: alert fatigue)

Alert-svc implements: severity-based routing (FR-118), per-zone mute/snooze, **alert aggregation** (same camera+type within T seconds → state machine, not event storm), confidence thresholds per site, and ML false-positive suppression (eval-svc feedback loop).

---

## 5. AI Pipeline

### 5.1 Lifecycle

```mermaid
flowchart LR
    subgraph Data["DATA / LABELING"]
        DS["Datasets: internal benchmarks,<br/>opt-in customer clips,<br/>hard-negative programs"]
        LAB["Labeling pipeline (human-in-the-loop)<br/>- SOC confirm/dismiss feedback<br/>- per-vertical labeling queues"]
    end

    subgraph Train["TRAINING (cloud)"]
        FT["Fine-tune / train<br/>(SageMaker, PyTorch)"]
        EV["Offline eval: benchmark suite<br/>per vertical (PPE, fire, weapon...)"]
        EXP["Export: TensorRT INT8/FP16,<br/>ONNX, quantized"]
    end

    subgraph Registry["MODEL REGISTRY"]
        REG["Model Registry<br/>versioned, signed,<br/>shadow/A-B staged"]
        CAL["Per-site calibration<br/>(thresholds, ROI, zones)"]
    end

    subgraph EdgeDeploy["EDGE DEPLOY"]
        DEP["Canary rollout via IoT Jobs<br/>(Greengrass component)"]
        INF["Inference: 5-10 FPS analytics,<br/>TensorRT on Jetson/x86"]
    end

    subgraph Feedback["FEEDBACK"]
        DRIFT["Drift monitor:<br/>confidence distribution,<br/>embedding distance,<br/>false-positive rate"]
        FBL["Retrain trigger<br/>(precision < site threshold)"]
    end

    DS --> LAB --> FT
    FT --> EV --> EXP --> REG
    REG --> DEP
    DEP --> INF
    INF --> DRIFT
    DRIFT --> FBL --> FT
    LAB -.->|"SOC ack/reject<br/>per alert"| DRIFT
    CAL --> REG
```

### 5.2 Inference Engines & Models

| Engine | Model Family | Acceleration | Notes (PRD) |
|---|---|---|---|
| Vision (weapon, fire/smoke, PPE, fall, fight, intrusion, loitering, crowd) | YOLO-class detectors + pose/velocity logic (fall: ≥1.5s posture change; fight: 2+ persons, velocity cluster) | TensorRT INT8, 5–10 FPS analytics | FR-101…114; false-positive suppression via hard negatives |
| Face (attendance, liveness, search) | ArcFace embeddings + anti-spoof liveness model | TensorRT FP16 | FR-102; biometric guardrails §15.3 |
| ReID (multi-camera person tracking) | OSNet/BoT embeddings; camera-graph handoff | TensorRT | FR-104, FR-119 (Phase 2) |
| LPR | Detection + OCR (regional formats IN/EU/US), night/rain augmentation | TensorRT | FR-105 (Phase 2) |
| Crowd density | Head-count CNN + calibration map | TensorRT | FR-114/115 (Phase 2) |

### 5.3 Runtime Design

- **Single inference runtime everywhere:** NVIDIA Triton Inference Server serves models identically on edge (Jetson/x86) and in cloud (GPU burst), so a model trained once deploys everywhere with the same inputs/outputs.
- **Scheduling:** frames are sampled at 5–10 FPS for analytics while the stream is passed through at full FPS for recording (PRD §14 mitigation for 4K cost).
- **Region-of-interest gating:** only decoded regions with motion/detector triggers enter expensive models (cascaded cheap detector → expensive classifier).
- **Temporal smoothing:** every event passes through a state machine (e.g., fire: 3+ consecutive frame confirms; fall: posture change ≥1.5s; loitering: dwell threshold) to meet precision targets in §9.
- **Per-site tuning:** thresholds (0.5–0.9 confidence), zone matrix, and ROI are tenant config pushed by config-svc (FR-101, §14).
- **Drift management:** shadow-mode evaluation of new model versions on live traffic; rollback via model registry (SageMaker + GitOps version pin).

### 5.4 Training Infrastructure (cloud)

- SageMaker Pipelines: preprocessing → training → evaluation → register; triggered by schedule, data additions, or drift alarms.
- GPU fleet: p4d/p5 instances for training; g5 for cloud inference burst.
- Dataset governance: opt-in-only customer data (§15.8), per-vertical benchmark programs (§14), versioned datasets in S3 with checksums.

---

## 6. Video Processing Pipeline

### 6.1 Pipeline Stages

```mermaid
flowchart TD
    CAM["Camera (RTSP/H.264/H.265, ONVIF S/T)"] -->|"RTSP pull"| DEC["Decode<br/>(GPU/VAAPI, full FPS)"]

    DEC -->|"Branch A - recording"| ENC["Re-encode preview 720p/1080p<br/>(H.265) + keyframe fragments"]
    ENC --> MASK["Privacy mask (pre-encode,<br/>pixel-level, zone-based)"]
    MASK --> RINGB["Ring buffer (10-30s pre-event)<br/>+ evidence clips (NVMe)"]

    DEC -->|"Branch B - analytics"| SAMP["Frame sampling 5-10 FPS<br/>+ motion gating"]
    SAMP --> PREP["Preprocess (resize,<br/>letterbox, normalization)"]
    PREP --> INFER["Inference engines<br/>(Triton + TensorRT)"]
    INFER --> RULE["Rules engine<br/>(zones, PPE matrix, thresholds)"]
    RULE --> EVENTS["Events (typed, with<br/>confidence + metadata)"]

    RINGB -->|"store-and-forward"| UPLOAD["Uploader (priority queue):<br/>metadata > evidence > video"]
    UPLOAD -->|"evidence MP4 + SHA-256 chain"| S3E["S3 evidence bucket<br/>(SSE-KMS, tenant prefix)"]
    UPLOAD -->|"fragments"| KVS["KVS (live + short archive)"]
    EVENTS -->|"Kinesis"| CLOUD["Cloud event backbone"]

    CTRL["config-svc (zones, masks,<br/>rules, model versions)"] --> RULE
    CTRL --> MASK
```

### 6.2 Key Design Decisions

| Decision | Rationale |
|---|---|
| **Decode once, branch twice** | Full-FPS decode feeds both the recording path and the analytics path from a single decode; GPU/VAAPI decode minimizes CPU per stream (8–32 streams/box per PRD). |
| **Analytics at 5–10 FPS** | Meets ≤3s detection latency (G1) while cutting 4K inference cost ~5–10× (PRD §14). |
| **Privacy masks pre-encode** | Lockers/restrooms are redacted at the pixel level *before* any encoding; masks are tamper-resistant because the masked pixels never exist downstream (§15.1). |
| **Ring buffer for pre-event evidence** | FR-112/117 require 10s pre-event context; NVMe ring preserves it until a detection promotes it to a clip. |
| **Dual video destinations** | KVS for live/low-latency viewing (WebRTC), S3 for durable evidence and tiered retention (30/90/365 days) — PRD §14 tiering. |
| **Upload priority queue** | Metadata first, then evidence, then archive video — guarantees event fidelity even on constrained links; store-and-forward resumes with resume/offset. |
| **Hash-chained evidence** | Each clip's SHA-256 links to the previous clip hash (chain of custody, FR-117, §16 insurance/forensic standard). |

### 6.3 Bandwidth Model (per camera)

| Stream | Rate (typical) | Purpose |
|---|---|---|
| Analytics (5–10 FPS detections) | ~50–150 KB/s metadata + thumbnails | Detection |
| Evidence clips (only on event) | bursty, ≤30s @ 2–4 Mbps | Incidents |
| Cloud archive (optional 720p) | ≤1 Mbps nominal (10.8 GB/camera/day); 1–2 Mbps burst envelope | Long retention; capacity and cost planning use the nominal value |
| Live view (on-demand, WebRTC/HLS) | 0 when idle | SOC viewing |

Site uplink planning uses this table; edges auto-degrade archive quality when link utilization exceeds 80%.

---

## 7. Edge Computing Architecture

### 7.1 Edge Node Software Stack

```mermaid
flowchart TD
    subgraph EdgeHW["REFERENCE HARDWARE"]
        HW_J["NVIDIA Jetson Orin (AGX/NX)<br/>8-32 camera streams, 4K"]
        HW_X["Certified x86 box (Advantech/Dell-class)<br/>+ optional discrete GPU"]
        HW_N["(v2) NPU boxes"]
    end

    subgraph EdgeSW["EDGE SOFTWARE (containerized)"]
        OS["Ubuntu LTS (minimal, secured)<br/>secure boot + LUKS disk encryption"]
        RUNTIME["containerd + Docker<br/>compose-defined pods"]
        GREEN["AWS IoT Greengrass v2 nucleus<br/>- device shadow<br/>- IoT Jobs OTA<br/>- cert provisioning"]
        AGENT["edge-agent (Go)<br/>- stream manager (RTSP)<br/>- rules engine<br/>- store-and-forward<br/>- health beacon"]
        ENGINE["Triton + TensorRT engines<br/>(vision, face, +optional lpr/reid)"]
        STORE["Local store<br/>- SQLite/RocksDB events<br/>- NVMe ring + clip cache<br/>- WAL + compaction"]
        WATCH["watchdog / supervisor<br/>auto-restart, disk hygiene,<br/>watchdog health checks"]
    end

    HW_J --> OS
    HW_X --> OS
    OS --> RUNTIME
    RUNTIME --> GREEN
    RUNTIME --> AGENT
    RUNTIME --> ENGINE
    AGENT --> STORE
    WATCH --> AGENT
    WATCH --> ENGINE
    GREEN -.->|"OTA: models, firmware,<br/>containers"| AGENT
```

### 7.2 Responsibilities

- **Real-time detection** with local rule evaluation (zones, PPE matrix, thresholds) — zero cloud dependency for life-safety (G1, §14 network instability).
- **Store-and-forward:** events → local queue; evidence → NVMe cache; video → ring. On reconnect: resume uploads with backoff; cloud reconciliation verifies completeness (see §4.3).
- **OTA:** model artifacts, engine versions, firmware, and edge-agent binaries are delivered as Greengrass components via IoT Jobs with canary device groups and automatic rollback on health-beacon failure.
- **Edge high availability:** dual-power, RAID/NVMe mirroring for the store, watchdog auto-restart, degraded mode (drop archive video first, keep detection).
- **Zero-touch deployment:** pre-flashed SD/SSD images, first-boot enrollment via serial + QR pairing to tenant site, config convergence from config-svc (PRD G4 — 100-camera site in ≤5 days).

### 7.3 Edge ↔ Cloud Contract (mTLS)

| Channel | Protocol | Purpose |
|---|---|---|
| Events | HTTPS/Kinesis PUT (batch) | Detections, health, heartbeats |
| Video | KVS PutMedia (presigned, STS) | Fragments + evidence |
| Control | MQTT (IoT Core) + shadows | Config convergence, jobs, commands |
| Telemetry | OTLP/HTTPS | Metrics, logs (sampled) |
| Uploads | S3 (presigned) | Evidence clips, exports, debug bundles |

Certificates: device X.509 issued at enrollment, short-lived STS credentials for data-plane uploads, certificate rotation via IoT Jobs.

---

## 8. Cloud Architecture

### 8.1 Multi-Region Topology (Day 1)

```mermaid
flowchart TD
    subgraph Region1["Region A - ap-south-1 (India)"]
        R1_EDGE["Edge devices (IN customers)"]
        R1_STACK["Full control + data plane stack<br/>(EKS, Kinesis, MSK, KVS, S3,<br/>Aurora, DynamoDB, OpenSearch)"]
        R1_KMS["KMS keys (per tenant alias)"]
    end

    subgraph Region2["Region B - us-east-1 (US)"]
        R2_EDGE["Edge devices (US customers)"]
        R2_STACK["Full stack (identical IaC)"]
        R2_KMS["KMS keys"]
    end

    subgraph Region3["Region C - eu-central-1 (EU)"]
        R3_EDGE["Edge devices (EU customers)"]
        R3_STACK["Full stack (identical IaC)"]
        R3_KMS["KMS keys"]
    end

    subgraph Global["GLOBAL (pinned services)"]
        R53["Route 53 (latency + health failover)"]
        CF["CloudFront (static app, WAF)"]
        GA["Global Accelerator (API endpoints)"]
        IDP["Cognito user pools (per region,<br/>sync where required) / IdP federation"]
        IAM["Identity: SSO federation (SAML/OIDC)"]
    end

    R1_EDGE --> R1_STACK
    R2_EDGE --> R2_STACK
    R3_EDGE --> R3_STACK
    Users --> CF --> R53
    Users --> GA --> R53
    R53 --> R1_STACK
    R53 --> R2_STACK
    R53 --> R3_STACK
    R1_STACK --> R1_KMS
    R2_STACK --> R2_KMS
    R3_STACK --> R3_KMS
```

### 8.2 Region Architecture (one region, expanded)

```mermaid
flowchart TD
    subgraph EdgeNet["Customer edge (VPC peering / private links optional)"]
        EDGE["Edge boxes"]
    end

    subgraph EdgeVPC["Edge ingress"]
        IOT["AWS IoT Core (MQTT,<br/>certificates, shadows, Jobs)"]
        STS["STS credential provider"]
    end

    subgraph AppVPC["Application VPC"]
        subgraph Public["Public subnets"]
            NAT["NAT gateways"]
            ALB["ALB (internet-facing)"]
        end
        subgraph Private["Private subnets"]
            EKS["EKS control + data plane<br/>- platform services (Go)<br/>- AI services (Python)<br/>- KCL workers, realtime-gw<br/>- Kong gateway"]
            RDS["Aurora PostgreSQL<br/>(+ TimescaleDB)"]
            DDB["DynamoDB"]
            OSE["OpenSearch"]
            RED["ElastiCache Redis"]
            MSK["MSK Kafka"]
        end
        subgraph Data["Data plane"]
            KVS["Kinesis Video Streams"]
            S3["S3 buckets:<br/>video-archive / evidence /<br/>reports / models / datasets /<br/>audit (Object Lock)"]
            KDS["Kinesis Data Streams"]
            SQS["SQS + DLQs"]
            SNS["SNS topics"]
        end
    end

    EDGE -->|"mTLS MQTT"| IOT
    IOT -->|"presigned/STS"| KVS
    IOT --> S3
    EDGE -->|"Kinesis PUT"| KDS
    KDS --> EKS
    EKS --> RDS
    EKS --> DDB
    EKS --> OSE
    EKS --> RED
    EKS --> MSK
    EKS --> S3
    EKS --> KVS
    S3 --> KVS
    ALB --> EKS
```

### 8.3 Region Strategy

- **Pinned tenants:** each tenant is bound to exactly one region at onboarding (residency guarantee §15.7); control-plane metadata (tenant row) carries `data_region`.
- **Same IaC everywhere:** Terraform modules parameterized by region (CIDRs, AZs, instance classes); no region-specific forks.
- **Federated identity:** enterprise SSO via SAML/OIDC against customer IdPs; user pools per region, user identity mirrored at the directory level.
- **Global services used sparingly:** Route53, CloudFront, Global Accelerator — only for routing; all data stays regional.
- **Future regions** (e.g., Saudi/UAE per roadmap §17.9) are added by instantiating the same module set.

---

## 9. Database Architecture

### 9.1 Database Portfolio (polyglot persistence)

| Store | Data | Why (PRD mapping) |
|---|---|---|
| **Aurora PostgreSQL + TimescaleDB** | Tenants, users, sites, cameras, zones, rules, incidents, reports metadata, audit (hot), occupancy/dwell time-series | Relational integrity for config & RBAC; Timescale hypertables for analytics (FR-115, Phase 2); Global Database for DR |
| **DynamoDB** | Alert fast-path, event log (30d hot), device shadows, dedupe window, rate-limit counters, sessions | Single-digit-ms at ≥1,000 events/sec (NFR); infinite scale with per-tenant partition keys |
| **OpenSearch** | Incident/event search, camera-health index, face-embedding vector search (k-NN, Phase 2), operational log search | Full-text + vector search (FR-117, Phase 2 face search) |
| **S3 (Intelligent-Tiering + Glacier)** | Video archive, evidence clips, snapshots, reports, model artifacts, datasets, audit archive | Tiered retention 30/90/365d, cost control (PRD §14) |
| **ElastiCache Redis** | Rule cache, presence, rate limits, alert aggregation windows, WebSocket pub/sub, dedupe | Sub-ms hot state; TTL-native |
| **SQLite/RocksDB (edge)** | Local event queue, config cache, ring-buffer index | Offline operation (store-and-forward) |

### 9.2 Relationships & Partitioning

```mermaid
erDiagram
    TENANT ||--o{ SITE : owns
    TENANT ||--o{ USER : has
    SITE ||--o{ CAMERA : has
    SITE ||--o{ ZONE : has
    ZONE ||--o{ RULE : has
    CAMERA ||--o{ EVENT : produces
    ZONE ||--o{ EVENT : scopes
    EVENT ||--o| ALERT : escalates
    ALERT ||--o{ NOTIFICATION : fans_out
    EVENT ||--o{ INCIDENT : aggregates
    INCIDENT ||--|| EVIDENCE : includes
    CAMERA ||--o{ CAMERA_HEALTH : reports
    SITE ||--o{ MODEL_ASSIGNMENT : runs
    MODEL_ASSIGNMENT ||--|| MODEL_VERSION : pins

    TENANT {
        uuid id PK
        string name
        string data_region
        string tier
        jsonb settings
    }
    EVENT {
        uuid id PK
        uuid tenant_id FK
        uuid camera_id FK
        string event_type
        float confidence
        timestamp occurred_at
        jsonb payload
    }
    ALERT {
        uuid id PK
        uuid tenant_id FK
        uuid event_id FK
        string severity
        string status
        timestamp created_at
    }
```

### 9.3 Data Partitioning & Multi-Tenant Isolation Rules

- **Aurora:** every table carries `tenant_id`; **row-level security** policy enforces `tenant_id = current_setting('app.tenant')`; Timescale hypertables partitioned by `(tenant_id, time)`; indexes suffix `(tenant_id, ...)` on every hot query.
- **DynamoDB:** primary key = `tenant_id + entity_id`; GSIs always begin with `tenant_id`; per-tenant capacity in **on-demand** mode with per-tenant burst protection at the API layer (quota middleware).
- **S3:** bucket-per-function + `s3://<bucket>/<tenant_id>/<site_id>/...` prefixes; lifecycle policies scoped per prefix; **SSE-KMS with per-tenant key aliases** (crypto isolation).
- **Kinesis/Kafka:** streams partitioned by `tenant_id`; larger tenants get dedicated shards/partitions (noisy-neighbor isolation).
- **OpenSearch:** tenant index-per-tenant (small tenants share an index with a `tenant_id` filter — aliased by size class).

### 9.4 Retention & Erasure (PRD §15.4)

Retention is a first-class tenant setting (7/30/90/365 days) enforced at **three layers**:

1. S3 lifecycle rules (prefix-scoped) → delete/transition to Glacier.
2. Timescale/DynamoDB TTLs → automatic row expiration.
3. **Erasure jobs** (tenant-svc) → full delete + right-to-erasure API that walks every store and produces a deletion manifest (audited). Biometric embeddings destroyed on tenant or employee erasure request.

---

## 10. API Gateway Architecture

### 10.1 Gateway Topology

```mermaid
flowchart LR
    SPA["React SPA (CloudFront)"] -->|"HTTPS/TLS 1.3"| WAF["AWS WAF<br/>(OWASP rules, IP<br/>reputation, rate)"]
    MOB["Mobile apps (Phase 2)"] --> WAF
    SYS["Partner systems (webhooks/API)"] --> WAF
    WAF --> GA["Global Accelerator"]
    GA --> KONG["Kong Gateway (EKS, multi-AZ)<br/>2+ replicas/zone"]

    KONG -->|"/api/... JWT"| SVC["Platform services"]
    KONG -->|"/ws"| RTGW["realtime-gw"]
    KONG -->|"/webhooks/in"| INTEG["integration-svc"]
    KONG -->|"/edge/... mTLS"| DV["device-svc"]

    OIDC["Cognito / Keycloak (JWKS)"] -->|"jwks_uri (cached)"| KONG
```

### 10.2 Gateway Responsibilities

| Concern | Implementation |
|---|---|
| **Authentication** | JWT validation (signature, issuer, audience, expiry) with cached JWKS; rejects tokens lacking `tenant_id` claim |
| **Authorization** | OPA (Rego) policy checks: scope required vs token scopes, tenant match, zone-level ABAC assertions passed as context headers |
| **Rate limiting** | Per-tenant + per-user sliding windows (Redis); 429 + `Retry-After`; tenant quotas from billing-svc |
| **Routing** | Path-based: `/api/*` (REST), `/ws/*` (WebSocket upgrade), `/webhooks/in/*` (HMAC-verified partner calls), `/edge/*` (mTLS-only, X.509 client certs) |
| **Edge identity** | mTLS termination for edge devices; validates client cert against device registry, injects `X-Device-Id` |
| **Observability** | Correlation-ID injection, access-log emission to Loki/CloudWatch, request sampling to Tempo |
| **Resilience** | Circuit breakers to services, request size limits, JSON schema validation, CORS policy |

### 10.3 API Contract

- REST over HTTPS with OpenAPI 3.1 spec; webhook-out with HMAC signatures and replay windows (FR-205).
- Versioned (`/v1/...`); additive changes only within a version; deprecation policy ≥6 months.
- Idempotency support on POST mutation endpoints (`Idempotency-Key` header).
- Event-stream consumers can subscribe via webhooks (delivered by integration-svc) or pull (REST pagination + cursor).

---

## 11. Authentication Flow

### 11.1 User Authentication (OIDC Authorization Code + PKCE)

```mermaid
sequenceDiagram
    participant U as User
    participant SPA as React SPA (PWA)
    participant CF as CloudFront/CDN
    participant IDP as Cognito Pool<br/>(or Keycloak local)
    participant K as Kong Gateway
    participant API as Platform Services

    U->>SPA: opens app
    SPA->>IDP: /authorize (code + PKCE, prompt=login)
    IDP-->>U: login form (MFA if required)
    U->>IDP: credentials + MFA (TOTP/WebAuthn)
    IDP-->>SPA: authorization code
    SPA->>IDP: /token (code_verifier, client_id)
    IDP-->>SPA: access_token (15 min) + refresh_token (30d, rotating)
    SPA->>K: GET /api/v1/sites (Bearer JWT)
    K->>K: validate JWT signature (JWKS cache),<br/>issuer, aud, exp, tenant_id claim
    K->>K: OPA policy: scope, tenant, zone access
    K-->>API: forward + X-Tenant-Id, X-User-Id, X-Scopes
    API-->>SPA: 200 (tenant-scoped data)
    Note over SPA,IDP: Refresh rotation on 401; re-auth on refresh replay
```

- **MFA enforced** for Super Admin, Auditor, and any role with export/delete privileges.
- **SSO:** enterprise IdPs via SAML 2.0/OIDC federation; SCIM provisioning for user lifecycle.
- **Local-only deployments:** the same flow runs against an embedded Keycloak instance with offline token validation (no cloud round-trip) — PRD §10.

### 11.2 Device (Edge) Authentication — mTLS + STS

```mermaid
sequenceDiagram
    participant E as Edge Agent
    participant IOT as IoT Core
    participant STS as STS/Device Credentials
    participant KVS as KVS / S3
    participant DV as device-svc

    E->>IOT: MQTT connect (X.509 client cert,<br/>CN = device-id, signed by tenant CA)
    IOT->>IOT: validate cert against device registry<br/>(active, not revoked, site matched)
    IOT-->>E: MQTT CONNACK + device shadow
    E->>STS: request credentials (cert-based, role = edge-data-uploader)
    STS-->>E: short-lived STS creds (15 min, scoped:<br/>kvs:PutMedia, s3:PutObject on own prefix)
    E->>KVS: PutMedia (fragment upload)
    E->>DV: heartbeat (authenticated channel)
    Note over E,DV: Cert rotation via IoT Jobs;<br/>revocation = disconnect + credential denial
```

### 11.3 Service-to-Service Authentication

- In-cluster: Kubernetes Service Accounts + IRSA (OIDC) — services assume narrow IAM roles; mTLS via service mesh where required.
- Out-of-cluster (Kinesis, S3, KVS, MSK): IAM roles with least-privilege policies; no long-lived keys.

---

## 12. Authorization

### 12.1 Model: RBAC + ABAC

```mermaid
flowchart TD
    TOK["JWT: sub, tenant_id,<br/>site_ids[], scopes[], role"] --> GW["Kong + OPA"]
    POL["OPA Rego policies:<br/>- role capability matrix<br/>- site scope containment<br/>- zone-level data class (raw vs metadata)<br/>- biometric data guardrails"] --> GW
    GW -->|"allow/deny + context headers"| SVC["Services"]
    SVC -->|"RLS: tenant_id"| DB["Aurora RLS / DynamoDB key"]
    SVC -->|"ABAC: zone & data-class"| ENC["Masked/redacted payloads<br/>(privacy §15)"]
```

### 12.2 Role Matrix (FR-204)

| Capability | Super Admin | Site Admin | Operator (SOC) | Auditor | Viewer |
|---|---|---|---|---|---|
| Manage tenants/sites | ✓ | site-scoped | — | — | — |
| Configure zones/rules/cameras | ✓ | ✓ | view | view | view |
| Live view (raw video) | ✓ | ✓ | ✓ | — | — |
| Acknowledge/escalate alerts | ✓ | ✓ | ✓ | — | — |
| Export evidence/reports | ✓ | ✓ | ✓ | ✓ | — |
| Face search / biometric access | ✓ (audited) | site-opt-in | site-opt-in | — | — |
| View audit log | ✓ | — | — | ✓ | — |
| Delete data / erasure | ✓ (dual-approval) | — | — | — | — |
| View analytics/dashboards | ✓ | ✓ | ✓ | ✓ | ✓ |

### 12.3 ABAC Policies (examples)

- `site_scope ⊆ token.site_ids` — a user can never touch another site's data even with a valid tenant token.
- **Data class:** `raw video` vs `events+metadata` vs `biometric`. Privacy principle §15.2: default access is metadata; raw video requires explicit role + logged access.
- **Biometric guardrails (§15.3):** face-embedding endpoints require a separate `biometric:*` scope, always audited, tenant must have opted into biometric mode.
- Enforcement happens at the gateway (coarse) **and** in services (fine, for query filters and payload redaction) — never trust gateway alone.

---

## 13. Multi-Tenancy

### 13.1 Tenant Model

- **Tenant = company**; hierarchy: `tenant → site → camera/zone → rule`. Sites may be grouped for enterprise (CSO) dashboards (persona: Vikram).
- **Tenant classes:** SMB (pooled) and Enterprise (dedicated edge, dedicated capacity, private networks), per pricing plan (G3).
- **Isolation levels:**

```mermaid
flowchart TD
    subgraph Shared["POOLED (SMB)"]
        E1["Edge: per-site box (shared cloud control)"]
        DB1["Cloud: tenant_id everywhere + RLS"]
        B1["Buckets: tenant prefixes + KMS alias"]
    end
    subgraph Dedicated["DEDICATED (Enterprise)"]
        E2["Edge: dedicated boxes,<br/>private VLAN / air-gapped option"]
        DB2["Optional dedicated Aurora instance /<br/>DynamoDB table per tenant"]
        B2["Dedicated buckets + dedicated KMS key"]
    end
    subgraph Local["LOCAL-ONLY (§10)"]
        E3["Edge: full stack on-prem<br/>(control plane embedded)"]
    end
```

### 13.2 Tenant Governance

| Concern | Mechanism |
|---|---|
| Data isolation | `tenant_id` on every record, RLS, S3 prefixes, DynamoDB PK, Kafka partition keys (§9.3) |
| Crypto isolation | Per-tenant KMS key aliases; biometric data on separate key hierarchy per tenant |
| Quotas & limits | Per-tenant rate limits, camera count, retention days, storage caps (enforced at gateway + billing-svc metering) |
| Residency | `tenant.data_region` pinned at onboarding; cross-region copy APIs blocked by policy |
| Compliance per tenant | Retention profile, masking defaults, audit verbosity — all tenant settings |
| Erasure | Tenant-level or data-subject erasure API with manifest + audit (right-to-erasure) |
| Noisy neighbor | Dedicated Kinesis shards/MSK partitions for large tenants; per-tenant P95 latency dashboards |

### 13.3 Multi-Tenancy Test Gates

Every release runs isolation tests: cross-tenant read attempts must 404/403, RLS bypass attempts must fail, bucket-prefix traversal must fail, and tenant A's erasure must not affect tenant B.

---

## 14. Security Architecture

### 14.1 Defense in Depth

```mermaid
flowchart TD
    L0["L0 Edge hardening<br/>secure boot, signed images, LUKS,<br/>minimal OS, watchdog, cert rotation"] --> L1
    L1["L1 Network<br/>VPC isolation, private subnets,<br/>security groups, VPC endpoints,<br/>no public DBs"] --> L2
    L2["L2 Identity & access<br/>Cognito MFA, least-privilege IRSA,<br/>scoped STS for edge,<br/>device cert registry + revocation"] --> L3
    L3["L3 Data security<br/>TLS 1.3 in transit, AES-256 at rest (KMS),<br/>per-tenant keys, biometric field-level<br/>encryption, redaction pipelines"] --> L4
    L4["L4 Application<br/>WAF + rate limits, input validation,<br/>SAST/DAST, dependency scanning,<br/>secret management (no secrets in config)"] --> L5
    L5["L5 Evidence integrity<br/>hash-chained artifacts,<br/>immutable audit (S3 Object Lock)"] --> L6
    L6["L6 Detection & response<br/>GuardDuty, CloudTrail, anomaly<br/>alerts, IR runbooks, pen tests pre-GA"]
```

### 14.2 Specific Controls (PRD-mapped)

| Control | Implementation | PRD Ref |
|---|---|---|
| TLS 1.2+ everywhere; TLS 1.3 on user-facing paths | CloudFront/WAF/ALB termination, mTLS edge↔cloud | §6 security NFR |
| AES-256 at rest, hardware-backed keys | KMS (HSM-backed, multi-region replicas); edge uses TPM/secure element where available | §6 |
| No silent loss, tamper-evidence | Hash chains on evidence; Object Lock on audit bucket | FR-117, §16 |
| Biometric protection | Separate KMS key hierarchy + field-level encryption; no identity photos stored by default (§15.3 attendance-only mode) | §15.3 |
| Access logging & attribution | Every playback/export/identity lookup logged (audit-svc) | §15.6 |
| Threat detection | GuardDuty, WAF managed rules, CloudTrail to SIEM, edge-behavior anomaly (device-svc) | §6 |
| Supply chain | Signed container images (Cosign), signed model artifacts, SBOM generation, image/model provenance in registry | §6 |
| Penetration & red team | Pen test pre-GA; adversarial ML testing (liveness, input sanitization) | §6, §14 |
| Secrets | AWS Secrets Manager + external secrets operator; no secrets in Git, code, or edge images | — |

### 14.3 Compliance Envelope (PRD §16)

SOC 2 Type II / ISO 27001 evidence pipelines are produced from the same telemetry (audit log, access logs, config change history, backup/restore logs). GDPR/DPDP features (DPIA kit, consent tooling, erasure, residency, DPO contact) are configuration-driven per region. Biometric consent + BIPA-style notice packs are delivered per tenant at onboarding.

---

## 15. Monitoring

### 15.1 Telemetry Stack

```mermaid
flowchart LR
    subgraph Sources
        SVC["Services (OTel SDK)"]
        EDGE["Edge agents (OTel exporter)"]
        INFRA["EKS / Aurora / MSK /<br/>Kinesis / KVS metrics"]
        UX["Frontend (RUM)"]
        SYN["Synthetics canaries"]
    end

    subgraph Collect["COLLECT"]
        COL["OTel Collectors (DaemonSet,<br/>auto-scaling deployment)"]
    end

    subgraph Store["STORE"]
        PROM["Prometheus (+ Thanos)<br/>metrics"]
        LOKI["Loki<br/>logs"]
        TEMPO["Tempo<br/>traces"]
        CW["CloudWatch<br/>infra + service quotas"]
        SENT["Sentry<br/>frontend errors"]
    end

    subgraph Use["USE"]
        GRAF["Grafana (dashboards,<br/>tenant + platform views)"]
        ALM["Alertmanager"]
        SLI["SLO engine (alert on burn rate)"]
        PG["PagerDuty / Slack / Teams"]
    end

    SVC --> COL
    EDGE --> COL
    INFRA --> COL
    UX --> SENT
    SYN --> COL
    COL --> PROM
    COL --> LOKI
    COL --> TEMPO
    COL --> CW
    PROM --> GRAF
    LOKI --> GRAF
    TEMPO --> GRAF
    GRAF --> SLI --> ALM --> PG
    INFRA --> CW
```

### 15.2 SLOs (from PRD §6, §9)

| SLO | Target | Measure |
|---|---|---|
| Platform availability | 99.9% monthly | Uptime of API + core services (multi-region pooled) |
| Detection latency (life-safety) | ≤3s p95 edge→alert | OTel span edge→alert-svc→notify |
| Event ingestion | ≥1,000 events/sec sustained | Kinesis throughput + consumer lag |
| Alert delivery | p95 ≤10s (push), ≤60s (email) | notify-svc delivery spans |
| Alert accuracy | false-alert rate ≤1/5 cameras/day | alert-svc + eval-svc feedback |
| Evidence availability | ≥99.9% of incidents have complete dossiers | report-svc reconciliation job |

### 15.3 Edge Health & Business Monitoring

- **Per-device:** heartbeat age, store-and-forward queue depth, GPU/CPU util, FPS per engine, model version, disk hygiene, watchdog events. Threshold alarms → device-svc tickets (FR-116 camera health incl. blur/occlusion/tamper/lens obstruction from vision engine).
- **Business dashboards (per persona):** SOC live ops (alerts/min, MTTA, MTTR), Safety (PPE compliance %, incidents by type/zone), Operations (occupancy, dwell, heatmaps), CSO (cross-site KPIs, ROI reports).
- **Capacity signals:** Kinesis consumer lag, MSK broker load, Aurora CPU/replicas lag, OpenSearch pressure, per-tenant P95 latency — feed the scalability strategy (§17).

---

## 16. Logging

### 16.1 Log Classification

| Class | Content | Store | Retention |
|---|---|---|---|
| **Application logs** | Structured JSON: trace_id, tenant_id, service, level, msg | Loki (hot) → S3 archive | 30d hot / 1y archive |
| **Access logs** | Gateway/ALB: user, tenant, route, status, latency | Loki / CloudWatch | 180d (compliance) |
| **Audit log (immutable)** | Admin actions, exports, playback, identity lookups, erasure, config changes | S3 Object Lock + OpenSearch (searchable copy) | ≥7y / per tenant policy |
| **Edge logs** | journald, rotated; sampled telemetry uplink; full logs on support bundle request | local + S3 debug bucket | 7d local / 90d cloud |
| **Model/ML logs** | Inference FPS, confidence histograms, drift metrics | Prometheus + S3 metrics archive | 1y |

### 16.2 Rules

- Every log line is **structured JSON** with `trace_id` propagated from gateway (correlation IDs) so a single incident is traceable across edge → gateway → services → storage.
- **PII-safe by default:** log redaction filters (names, faces, plate numbers, video paths) applied at the collector; raw video never logs.
- **Audit immutability:** write-once, WORM via S3 Object Lock; hash-chained; dual-administrator override protocol (break-glass, itself audited).
- **Retention is tenant-configurable** per §15.4; deletion jobs cover logs too.
- **Alerting on anomalies:** log spikes, auth failures, repeated 4xx/5xx, DLQ depth.

---

## 17. Scalability Strategy

### 17.1 Scaling Model (1 → 10,000+ cameras, PRD NFR)

| Layer | Scale Unit | Mechanism |
|---|---|---|
| Edge | camera-per-box (8–32 streams) | Add certified boxes per site; device-svc auto-registers, config converges; 10k cameras ≈ 300–1,200 boxes |
| Event ingest | Kinesis shards | Scale shards with sustained throughput; 1,000 ev/s baseline, ~10× headroom via auto-scaling on consumer lag |
| Processing | Kubernetes pods | Stateless services HPA/KEDA (CPU, queue depth, Kinesis lag); Karpenter burst nodes |
| AI (cloud) | GPU nodes | Burst inference for Phase 2 features (face search over archive); GPU node pool scale-to-zero |
| Databases | Read replicas / shards | Aurora replicas (read-heavy analytics), DynamoDB on-demand, OpenSearch node add, Timescale chunk tuning |
| Video | KVS streams + S3 | Per-camera KVS with retention; S3 Intelligent-Tiering; upload bandwidth shaping per site |
| Realtime | WebSocket nodes | Horizontal realtime-gw with Redis pub/sub backplane; sticky sessions not required |
| Notifications | SNS/Lambda fan-out | Push adapter as Lambda (burst-safe), SQS decoupling |

### 17.2 Large-Tenant Handling

- Tenants >2,000 cameras get: dedicated Kinesis shard group, dedicated MSK partitions, larger-rate quotas, optional dedicated DB capacity (silo tier, §13.1).
- Per-tenant burst control at gateway (Redis token bucket) prevents noisy-neighbor (PRD risk: alert floods).

### 17.3 Capacity & Perf Guards

- **Unit-cost model:** tracked $/camera/month per deployment tier (compute, storage, bandwidth, inference) — informs G3 70% gross margin target.
- **Load tests:** 1,000 ev/s ingest, 100k concurrent WS, 10k-camera synthetic fleet, 4K multi-stream boxes — run in CI (k6 + Locust + custom edge simulators).
- **Chaos engineering:** quarterly GameDays (node loss, region failure, Kinesis throttle, edge disconnect) validating §17/§19 runbooks.

---

## 18. Backup Strategy

### 18.1 Backup Matrix

| Data | Store | Backup Mechanism | RPO | RTO |
|---|---|---|---|---|
| Relational (Aurora) | Aurora PostgreSQL | Automated backups + PITR (35d); manual snapshots pre-migration; cross-region via Global DB secondary | ≤5 min | ≤15 min |
| Event log (DynamoDB) | DynamoDB | On-demand PITR (35d) | ≤5 min | ≤15 min |
| Search (OpenSearch) | OpenSearch | Daily snapshots → S3 (snapshot repo), 30d | ≤24 h | ≤1 h |
| Video archive | S3 | Versioning + lifecycle + cross-region replica (CRR) | real-time | <1 h |
| Evidence/reports | S3 (Object Lock) | Versioning + WORM + CRR; hash chains independent of backup | real-time | <1 h |
| Kafka (MSK) | MSK | Replicated across AZs; topic retention 7–30d; mirroring to S3 via connector (analytic replay) | ≤5 min (mirror) | <1 h |
| Redis | ElastiCache | AOF (append-only) + RDB snapshots to S3 | ≤5 min | ≤15 min |
| Configuration | Terraform state + Git | Git as source of truth; state in S3 with versioning + DynamoDB lock | n/a | n/a |
| Edge (local) | Device storage | Local WAL + cloud store-and-forward (the cloud *is* the backup); offline clip cache survives reboot | n/a | n/a |

### 18.2 Backup Operations

- Backup jobs are **scheduled, monitored, and restored-tested** (quarterly restore drills with recovery time measurement).
- Immutable backups for evidence data (Object Lock) — an incident dossier remains recoverable even if the tenant's live data is deleted within retention.
- Right-to-erasure exceptions are explicitly *not* backed up beyond legal hold flags.

---

## 19. Disaster Recovery

### 19.1 DR Strategy: Multi-AZ within region + Active-Passive cross-region

- **In-region (AZ loss):** automatic — EKS multi-AZ, Aurora Multi-AZ, MSK Multi-AZ, Kinesis/S3/KVS 3-AZ, Redis Cluster mode, OpenSearch Multi-AZ. No manual action.
- **Cross-region (region loss):** tenant fails over to its **paired DR region** (e.g., India: ap-south-1 ↔ ap-southeast-1; US: us-east-1 ↔ us-west-2; EU: eu-central-1 ↔ eu-west-2). Targets: **RTO ≤ 60 min, RPO ≤ 5 min** (consistent with 99.9% availability).

### 19.2 Regional Failover Flow

```mermaid
sequenceDiagram
    participant MON as Observability (region A)
    participant R53 as Route 53 (health checks)
    participant OPS as On-call (paging)
    participant DR as DR Region (standby stack)
    participant GLB as Aurora Global Database
    participant EDGE as Edge devices

    MON->>MON: Region A: multiple SLO burn alerts<br/>(API 5xx, Aurora failure, EKS loss)
    MON->>OPS: PagerDuty incident (critical)
    OPS->>OPS: Confirm via runbook (5 min decision)
    OPS->>DR: Failover command (Terraform/tfstate switch + flags)
    DR->>GLB: Promote regional secondary (Aurora,<br/>RPO <1s via Global Database)
    DR->>DR: Replay Kinesis/MSK offsets from S3 mirror<br/>+ DynamoDB PITR if needed
    OPS->>R53: Switch routing (health-check fails →<br/>DR endpoint; TTL 30s)
    R53-->>EDGE: Edge reconnects to DR region<br/>(mTLS + region endpoint from device shadow)
    EDGE->>DR: store-and-forward replay (data preserved)
    DR-->>OPS: Confirmed: dashboards green,<br/>DR drill report filed
```

### 19.3 DR Details

| Concern | Approach |
|---|---|
| Data plane | Aurora Global Database (secondary in DR region, RPO <1s); S3 CRR continuous; Kinesis fan-out mirrored (S3 + MSK replica); DynamoDB PITR restore |
| Edge continuity | Edge is **self-sufficient**: detection and alerts continue during a full cloud outage (local rules engine); alerts queue on edge and are replayed after reconnect; escalation via local relay (SMS gateway at site, optional) |
| Applications | Standby stack running at reduced capacity (warm standby: scaled-down EKS, scaled-down DB); scaled up on failover via IaC |
| Realtime | WebSocket clients reconnect to DR region endpoint; alert history served from DR DB |
| Failback | Reverse procedure after RCA; edge reconnects to primary; data reconciliation job merges (dedupe by event_id) |
| Governance | Quarterly DR drills (failover + failback timed and measured); annual full-region outage exercise; DR playbooks versioned in Git with screenshots of every command |

---

## 20. Deployment Topology

### 20.1 Full Deployment View

```mermaid
flowchart TD
    subgraph CustomerSite["CUSTOMER SITE"]
        subgraph Network["Site network (VLANs)"]
            CAM1["Cameras (ONVIF/RTSP)"]
            EDGE1["Edge box (Jetson Orin)<br/>8-32 streams"]
            EDGE2["Edge box (x86)<br/>additional streams"]
            SW1["PoE switch"]
        end
    end

    subgraph RegionP["PRIMARY REGION (tenant-pinned)"]
        subgraph VPC_AZ1["VPC - AZ1"]
            K8S_A["EKS (platform, AI,<br/>workers, gateway)"]
            DB_A["Aurora (writer)<br/>DynamoDB, Redis,<br/>OpenSearch, MSK"]
        end
        subgraph VPC_AZ2["VPC - AZ2"]
            K8S_B["EKS (same stack)"]
            DB_B["Aurora (reader)<br/>MSK, Redis"]
        end
        DATA["Kinesis, KVS, S3,<br/>SNS/SQS (region services)"]
        IDP["Cognito + IoT Core"]
    end

    subgraph RegionDR["DR REGION (warm standby)"]
        STBY["Same stack (scaled down)<br/>Aurora Global secondary,<br/>S3 CRR, mirrors"]
    end

    subgraph CI["CI/CD + INFRA"]
        GH["GitHub Actions"]
        ARGO["ArgoCD (GitOps)"]
        ECR["ECR (images + SBOM)"]
        SAGE["SageMaker Pipelines (models)"]
        TF["Terraform (IaC)"]
    end

    CAM1 --> SW1 --> EDGE1
    EDGE1 <--> EDGE2
    EDGE1 -->|"mTLS + events + video"| IDP
    EDGE1 --> DATA
    K8S_A --> DB_A
    K8S_A --> DATA
    K8S_A --> K8S_B
    DB_A -.->|"Global DB replication"| STBY
    DATA -.->|"CRR + mirror"| STBY
    GH --> ECR --> ARGO --> K8S_A
    ARGO --> K8S_B
    SAGE -->|"model registry"| IDP
    TF --> RegionP
    TF --> RegionDR
```

### 20.2 Environments & Promotion

| Environment | Purpose | Promotion gate |
|---|---|---|
| `dev` | Feature branches, synthetic edge simulators | PR + unit/integration tests |
| `staging` | Full stack + staging edge hardware | e2e, load smoke, security scan (Trivy/SAST/DAST) |
| `preview` (per-region) | Regional config validation | Terraform plan check, cross-region config diff |
| `prod` (3 regions) | Live | Canary 5% → 50% → 100% via ArgoCD; automated rollback |

Deployment model: **GitOps** — all changes (code, config, models) are declarative in Git; ArgoCD converges clusters; image/model provenance recorded in the deployment record (audited).

---

## 21. End-to-End Data Flow

```mermaid
flowchart LR
    CAM["Camera"] -->|"RTSP 25/30 FPS"| EDGE["Edge: decode, mask,<br/>analytics 5-10 FPS"]
    EDGE -->|"A: detections + metadata"| KS["Kinesis"]
    KS --> EV["event-svc: validate,<br/>dedupe, enrich"]
    EV -->|"alert candidate"| AL["alert-svc: severity,<br/>aggregation, policy"]
    AL -->|"alert"| NF["notify-svc"]
    NF -->|"push/SMS/WA/email"| USER["SOC / Manager"]
    AL -->|"alert"| RTG["WebSocket → live ops UI"]

    EDGE -->|"B: evidence clips (hash-chained)"| S3E["S3 evidence"]
    EDGE -->|"C: video fragments"| KVS["KVS live"]
    KVS -->|"on-demand"| PB["playback-svc"]
    PB -->|"HLS/WebRTC"| SOC["SOC playback UI"]

    EV -->|"stream"| MSK["Kafka"]
    MSK --> AN["analytics-svc"]
    AN -->|"occupancy, dwell,<br/>heatmaps"| DASH["Dashboards"]
    MSK --> SE["search-svc → OpenSearch"]
    MSK --> BATCH["batch jobs"]
    BATCH -->|"reports/ML features"| REP["report-svc"]
    REP -->|"incident dossiers<br/>(PDF/CSV/JSON)"| USERS["Auditors / HR / Insurance"]

    EV --> AUDIT["audit-svc (immutable)"]
    AL -->|"ack/reject"| FBL["feedback → training loop"]

    subgraph Privacy["PRIVACY BRANCH"]
        EDGE -->|"masked zones pre-encode<br/>(never visible downstream)"| NONE["Pixels destroyed at edge"]
    end
```

---

## 22. Technology Recommendations

### 22.1 Frontend

| Technology | Why |
|---|---|
| **React 18 + TypeScript + Vite** | Largest talent pool; Vite for fast builds; TS for a typed API contract with the OpenAPI spec |
| **Tailwind CSS + shadcn/ui** | Design-system velocity for dashboards across 12 verticals with per-vertical templates (FR-207) |
| **TanStack Query + Zustand** | Server-state caching/retry for event-heavy UIs; minimal client state |
| **PWA (Workbox)** | PRD §10 requires web-PWA in MVP (no native app); offline-friendly operator workflows (FR-118); installable for SOC |
| **HLS.js + KVS WebRTC SDK** | Playback scrub (HLS, latency-tolerant) + low-latency live view (WebRTC, ~300–500ms) — both backed by KVS (§6.3) |
| **ECharts** | Heatmaps, occupancy curves, density gauges (FR-115, Phase 2) without licensing friction |
| **MapLibre GL** | Site maps with camera placement, zones, tripwires (FR-203 builder UX) |

### 22.2 Backend

| Technology | Why |
|---|---|
| **Go** (platform services) | High concurrency per pod (Kinesis consumers, WebSocket gateways), small memory footprint, single-binary edge agent, static typing; ideal for the 1,000+ ev/s data path |
| **Python** (AI services) | The ML ecosystem (PyTorch, Triton, SageMaker SDK); AI-plane services only |
| **gRPC + Protobuf** (internal) | Typed, efficient service mesh calls; streaming support for video metadata |
| **REST + OpenAPI 3.1** (external) | FR-205 partner API suite; versioned, documented |
| **PostgreSQL via TimescaleDB** | Relational core + time-series in one engine (occupancy, dwell) — fewer moving parts |
| **NATS? No** — Kinesis/MSK chosen | Managed backbone removes self-hosting burden at 10k-camera scale (see 22.4) |

### 22.3 AI Services

| Technology | Why |
|---|---|
| **PyTorch** | Research-to-production standard; model family coverage (YOLO, ArcFace, ReID, LPR) |
| **NVIDIA Triton Inference Server** | One serving runtime on Jetson *and* cloud GPU; model versioning and dynamic batching; matches PRD §14 TensorRT requirement |
| **TensorRT (INT8/FP16)** | 2–5× latency/throughput gains on Jetson; required to hit 8–32 4K streams per box (§14) |
| **Ultralytics YOLO** | Detector backbone with hard-negative tuning programs; strong on small objects (weapons at 10–40m, FR-101) |
| **InsightFace/ArcFace + liveness model** | FR-102 attendance with anti-spoof; embeddings-only mode for privacy (§15.3) |
| **SageMaker Pipelines + Model Registry** | Training orchestration, versioning, shadow/AB, rollback, drift monitoring (§5.3) |
| **S3 + dataset versioning** | Labeled data governance with checksums and opt-in governance (§14.1, §5.4) |

### 22.4 Streaming

| Technology | Why |
|---|---|
| **RTSP/ONVIF Profile S/T** | Camera-side standard — zero hardware replacement (G4, §13 risk mitigation) |
| **Amazon Kinesis Video Streams** | Managed RTSP ingest, fragment archival, WebRTC signaling + HLS generation; directly solves §6 video scale without self-managed media servers |
| **HLS** | Playback/scrub and archive viewing — CDN-friendly (CloudFront) |
| **WebRTC (KVS signaling)** | ≤500ms live view for SOC operators (FR-201) — latency impossible with HLS alone |

### 22.5 Storage

| Technology | Why |
|---|---|
| **S3 (Intelligent-Tiering + Glacier)** | Durability (11×9s), lifecycle-driven retention 30/90/365d, evidence WORM (Object Lock) — meets §14 cost + §16 forensic needs |
| **Aurora PostgreSQL (+TimescaleDB)** | Managed RDS, Multi-AZ, Global Database for DR, RLS for multi-tenancy |
| **DynamoDB** | Hot event/alert path at ≥1,000 ev/s with predictable single-digit-ms; TTL for retention |
| **OpenSearch** | Full-text + vector (k-NN) search for incidents and Phase 2 face search |
| **Redis (ElastiCache)** | Sub-ms dedupe, rate-limit, aggregation, WS pub/sub |
| **NVMe on edge** | Pre-event ring buffer + store-and-forward cache (offline resilience, §6.2) |

### 22.6 Queues & Event Backbone

| Technology | Why |
|---|---|
| **Kinesis Data Streams** | High-throughput hot path (1,000+ ev/s), per-camera ordering, shard scaling — serverless, no broker to run |
| **MSK (Kafka)** | Durable replayable log for analytics/search/batch + multi-region mirroring; 7–30d retention for reprocessing |
| **SQS + DLQs** | Decoupled workers (alerts, notifications) with dead-letter replay — "no silent event loss" (§6) |
| **SNS + EventBridge** | Channel fan-out (severity topics) and cloud-side integrations/schedules (webhooks, HRIS syncs) |

### 22.7 Notification Service

| Technology | Why |
|---|---|
| **SNS → Lambda adapters** | Burst-safe push fan-out to APNs/FCM; per-channel adapters isolated (FR-118) |
| **Twilio** | SMS + WhatsApp in one API, global reach (India/EU/US) — deliverability matters for life-safety alerts |
| **Amazon SES** | Email with DKIM/DMARC, quota management, audit of sends |
| **Webhooks (integration-svc)** | HMAC-signed partner delivery with replay window + dead-letter (FR-205, Phase 2 Slack/Teams) |

### 22.8 Authentication

| Technology | Why |
|---|---|
| **Amazon Cognito** (cloud) | Managed OIDC + MFA + SAML federation + SCIM; multi-region pools; no IdP to operate |
| **Keycloak** (local-only mode) | PRD §10 requires offline deployment; embedded OIDC with same token contract as Cognito |
| **AWS IoT Core (X.509)** | Device identity, cert registry, shadows, Jobs (OTA) — the edge control channel (FR-202/FR-116) |
| **JWT (short-lived) + rotating refresh** | Stateless gateway validation; 15-min access tokens bound tenant/site scopes (§11, §12) |

### 22.9 Monitoring

| Technology | Why |
|---|---|
| **OpenTelemetry** | Vendor-neutral traces/metrics/logs from edge *and* cloud — one contract across planes |
| **Prometheus + Thanos + Grafana** | Industry-standard metrics; multi-region query; tenant + platform dashboards |
| **Loki** | Log correlation with traces (logQL); cheaper than full-text on hot logs |
| **Tempo** | Distributed traces across edge→gateway→services (detection latency SLO §15.2) |
| **Sentry** | Frontend/mobile error capture with user context |
| **Alertmanager + PagerDuty** | Severity-based paging; SLO burn-rate alerting |
| **CloudWatch** | AWS-infrastructure metrics and service quotas (complementary, not primary) |

### 22.10 Containerization

| Technology | Why |
|---|---|
| **Docker + containerd** | Standard build/runtime everywhere; identical images on edge (Jetson ARM64) and cloud (x86_64) via multi-arch builds |
| **Kubernetes (EKS) + Karpenter** | Self-healing, autoscaling platform; Karpenter for burst GPU nodes; avoids EC2 fleet management |
| **Helm + ArgoCD** | Declarative chart packaging + GitOps convergence with rollback (§20.2) |
| **Greengrass v2 (edge)** | OTA components, device shadows, Jobs — the edge-side "CD" story |
| **Cosign + Trivy** | Signed images + vulnerability gate in CI (supply chain, §14.2) |

### 22.11 CI/CD

| Technology | Why |
|---|---|
| **GitHub Actions** | Build, unit/integration/e2e tests, SAST/DAST, image build + sign, Terraform plan — single pipeline in the SCM |
| **ArgoCD** | GitOps deployment to 3 regions with canary progression and automated rollback |
| **Terraform (+ Terragrunt)** | Region-parameterized IaC (multi-region day 1, P6); state in S3 + DynamoDB lock |
| **SageMaker Pipelines** | Model CI/CD: train → eval gates → register → release via IoT Jobs |
| **k6/Locust + edge simulators** | Performance gates in CI (1,000 ev/s, 4K streams) |

### 22.12 Cloud Provider

| Technology | Why |
|---|---|
| **AWS (multi-region: ap-south-1, us-east-1, eu-central-1 + DR pairs)** | Unique fit for this workload: Kinesis Video Streams (managed RTSP/WebRTC), IoT Greengrass (edge OTA), SageMaker (MLOps), Aurora Global Database (sub-second RPO), S3 Object Lock (forensic evidence), WAF/GuardDuty (security), and the compliance footprint (SOC 2, ISO 27001, GDPR, DPDP alignment) — plus regional edge economics for India GTM |
| **Abstraction layer** | Edge hardware (Jetson/x86/NPU) and ONVIF camera standards prevent provider/device lock-in (P7, §13 risk) |

---

## 23. Appendix

### 23.1 Assumptions

1. Edge-first deployment with cloud control plane (PRD §10 recommended path); local-only mode supported via embedded control plane.
2. Multi-region day 1 (India/US/EU) with tenant-pinned residency; paired DR regions as listed in §19.
3. MVP web is PWA-only; native mobile in Phase 2 (PRD §11).
4. Camera protocol baseline: RTSP + ONVIF Profile S/T (PRD §6 interoperability).
5. Billing metering infrastructure (per-camera) is built in MVP to inform Phase-2 monetization.

### 23.2 Decisions Log

| # | Decision | Date | Rationale |
|---|---|---|---|
| AD-01 | Edge-first inference (Jetson + x86, abstraction layer) | 2026-07-30 | PRD §14; G1 latency; offline resilience |
| AD-02 | AWS multi-region day 1 | 2026-07-30 | GTM decision; KVS/Greengrass/SageMaker fit |
| AD-03 | Kinesis (hot) + MSK (replay) + SQS/SNS backbone | 2026-07-30 | Throughput + replay + decoupling (NFR) |
| AD-04 | Polyglot: Go (platform), Python (AI), TS/React (web) | 2026-07-30 | Throughput, ML ecosystem, talent |
| AD-05 | Aurora+Timescale, DynamoDB, OpenSearch, Redis, S3 | 2026-07-30 | Data-shape fit, RLS tenancy, cost tiers |
| AD-06 | Kong gateway with OPA authorization | 2026-07-30 | Vendor-neutral, mTLS, policy engine |
| AD-07 | Cognito cloud + Keycloak local-only | 2026-07-30 | PRD §10 local-only deployment |
| AD-08 | RPO ≤5 min, RTO ≤60 min; warm standby DR | 2026-07-30 | 99.9% availability target, cost balance |
| AD-09 | Privacy masks pre-encode at edge | 2026-07-30 | §15.1 — pixels never leave device |

### 23.3 PRD Feature → Component Mapping (MVP)

| PRD Feature | Component(s) |
|---|---|
| FR-101..114 detections | edge vision/face engines + event-svc + alert-svc |
| FR-116 camera health | vision engine health signals + device-svc |
| FR-117 incident reports | report-svc + S3 evidence + hash chain |
| FR-118 notifications | notify-svc + SNS + Twilio/SES/APNs/FCM |
| FR-201/202/203 | playback-svc, config-svc (ONVIF discovery, rule builder) |
| FR-204 RBAC | identity-svc + OPA policies |
| FR-205 API suite | Kong + integration-svc webhooks |
| FR-206 multi-tenancy | tenant-svc + RLS + per-tenant keys (§13) |
| FR-207 dashboards | analytics-svc + realtime-gw + React |
| §6 NFRs | Kinesis scale, DR §19, security §14 |
| §15 privacy | masking, biometric key hierarchy, erasure jobs |

### 23.4 Glossary

| Term | Meaning |
|---|---|
| Edge Agent | Go service on-premise managing streams, rules, persistence |
| Store-and-forward | Local buffering of events/clips during network loss with resume |
| Ring buffer | NVMe pre-event clip cache (10–30s) for evidence |
| KVS | Amazon Kinesis Video Streams |
| ReID | Person/vehicle re-identification across cameras |
| RLS | PostgreSQL Row-Level Security |
| PITR | Point-in-time recovery |
| WORM | Write-once-read-many (S3 Object Lock) |
