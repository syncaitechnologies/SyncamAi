# SyncCam AI — AI Architecture v1.0

**Document:** AI Architecture v1.0 (Draft for Review)
**Date:** July 30, 2026
**Source:** `PRD-SyncCam-AI.md` (v1.0), `ARCHITECTURE.md` (v1.0)
**Architectural posture:** Edge-first, shared detection backbone, specialist heads, temporal confirmation, cheap-detector → expensive-verifier cascade.

---

## Table of Contents

1. [Executive Design Decisions](#1-executive-design-decisions)
2. [Module Registry (Master Table)](#2-module-registry-master-table)
3. [Per-Module Specifications](#3-per-module-specifications)
4. [Temporal Confirmation Layer](#4-temporal-confirmation-layer)
5. [Tool Recommendations — Verdicts](#5-tool-recommendations--verdicts)
6. [Hardware & GPU Matrix](#6-hardware--gpu-matrix)
7. [Training & Data Strategy](#7-training--data-strategy)
8. [Roadmap Mapping](#8-roadmap-mapping)
9. [Key Risks & Open Decisions](#9-key-risks--open-decisions)

---

## 1. Executive Design Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **One shared detection backbone, specialist heads** — a single multi-class YOLO-class detector carries person, weapon, PPE, vehicle, fire-hotspot classes; specialist models (face, LPR, smoke, pose, density) plug in on triggers | 5 of 23 modules share >80% of their features; separate networks per module would blow the Jetson VRAM/latency budget (ARCHITECTURE.md §7 requires 8–32 streams/box) |
| D2 | **Temporal confirmation on every event** — sequence state machines (3-frame fire confirm, 1.5s fall posture, dwell thresholds) sit between raw inference and event emission | Meets PRD §9 precision targets (≥90–95%) and the ≤1 false alert / 5 cams/day SLO |
| D3 | **Cheap detector → expensive verifier cascade** — edge runs fast models; flagged events are asynchronously verified by heavyweight cloud models (RT-DETR, GroundingDINO) to suppress false positives | PRD §13 "detection drift / class confusion" risk; reuses the eval-svc feedback loop in ARCHITECTURE.md §5 |
| D4 | **Rule/logic engines are first-class "AI modules"** — zone intrusion, loitering, abandoned-object are *track-geometry + state logic*, not DNNs; only anomaly detection uses ML | Honest engineering: these modules need zero training data and are more robust as logic; ML adds failure modes |
| D5 | **ByteTrack for single-cam tracking, ReID only at camera handoff** — no appearance model on every frame | Saves 30–50% tracker cost on edge; DeepSORT-style ReID reserved for FR-119 multi-cam (Phase 2) |
| D6 | **License check before model selection** — Ultralytics YOLOv8/11 is AGPL-3.0; RT-DETR/D-FINE are Apache-2.0 | AGPL virality conflicts with proprietary multi-tenant SaaS; this is a legal, not technical, decision |

---

## 2. Module Registry (Master Table)

All latency/VRAM figures: **Jetson Orin NX 16GB, TensorRT INT8** (unless noted), 640px input, single stream.

| Module | Family | Model (recommended) | Size (FP32/INT8) | Latency | VRAM | GPU (edge → cloud) | Expected Accuracy |
|---|---|---|---|---|---|---|---|
| Person Detection | Detector | YOLOv8m / YOLO11m (shared backbone) | 50 / 13 MB | 10–14 ms | ~1.2 GB | Orin NX → T4 | AP50 0.93 (CrowdHuman-FT) |
| Object Tracking | Tracker | ByteTrack (IoU+Kalman) | 0 (no model) | 1–3 ms CPU | 0 | Orin NX → CPU | MOTA 78–83, IDF1 75 (MOT17) |
| Weapon Detection | Detector (small-object) | YOLOv8m FT, P2 head + SAHI at 10–40m | 50 / 13 MB | 14–20 ms (SAHI) | ~1.3 GB | Orin NX → T4/L4 | mAP50 0.82–0.88; precision ≥0.90 tuned |
| Vehicle Detection | Detector | Shared backbone FT (BDD100K) | 50 / 13 MB | 10–14 ms | shared | Orin NX → T4 | mAP50 0.94 |
| Vehicle Tracking | Tracker+ReID | ByteTrack + Vehicle-ReID (ResNet50-ReID) at handoff | 60 / 20 MB | 3–5 ms (ReID only on handoff) | +0.3 GB | Orin NX → T4 | IDF1 0.78–0.82 (CityFlow) |
| Face Detection | Face | SCRFD-500 (InsightFace) | 8 / 4 MB | 3–5 ms | 0.3 GB | Orin NX → T4 | AP 0.90–0.93 (WIDER easy) |
| Face Recognition | Face | ArcFace MobileFaceNet (edge) / R100 (cloud) | 4 / 2 MB | 2 ms | 0.2 GB | Orin NX → T4 (batch) | TAR ≥0.96 @ FAR 1e-4 |
| Face Verification | Face | Same ArcFace, 1:1 cosine + per-site threshold | — | 2 ms | shared | shared | AUC ≥0.995; site-tuned FAR/FRR |
| Face Liveness | Face | miniFASNet-class texture CNN (passive RGB) | 3 / 1.5 MB | 3–5 ms | 0.1 GB | Orin NX → T4 | APCER ≤1.5%, BPCER ≤1% |
| License Plate Detection | LPR | YOLOv8s-FT (plate class) or WPOD-NET | 22 / 7 MB | 5–7 ms | 0.3 GB | Orin NX → T4 | mAP50 0.95–0.97 |
| Plate OCR | LPR | LPRNet (edge) / PP-OCRv4-mobile (x86) | 5 / 2 MB | 4–6 ms | 0.2 GB | Orin NX → T4 | char acc 0.96–0.98 clean; 0.88–0.93 night/rain |
| Fire Detection | Classifier | EfficientNet-B0 FT on flame crops + 3-frame confirm | 20 / 6 MB | 3–5 ms | 0.3 GB | Orin NX → T4 | prec 0.93–0.96, rec 0.90 |
| Smoke Detection | Classifier | D-Fire FT (smoke class) + motion-gated | 20 / 6 MB | 4–6 ms | shared | Orin NX → T4 | prec 0.88–0.92, rec 0.85 |
| PPE Detection | Detector | Shared backbone FT (SODA/SHWD + owned) | 50 / 13 MB | shared | shared | Orin NX → T4 | helmet mAP50 0.92, vest 0.90; prec ≥0.95 |
| Helmet Detection | Detector | Same PPE model (class: helmet) | — | — | — | — | mAP50 0.92 |
| Vest Detection | Detector | Same PPE model (class: vest) | — | — | — | — | mAP50 0.90 |
| Fall Detection | Pose+logic | RTMPose-m / YOLOv8-pose-m + posture/motion FSM | 55 / 20 MB | 8–12 ms | 0.8 GB | Orin NX → T4 | prec 0.92, rec 0.95 (Le2i) |
| Fight Detection | Pose+logic | Person tracks + velocity/pose cluster scoring (ML optional) | 55 / 20 MB (shared pose) | +1–3 ms | shared | Orin NX → T4 | prec/rec 0.85–0.92 (RWF-2000) |
| Crowd Detection | Density | Person-count + perspective calibration map (CSRNet-lite optional) | 5 / 2 MB | 2–4 ms | 0.1 GB | Orin NX → T4 | MAE 8–15% (ShanghaiTech B) |
| Zone Intrusion | Logic | Point-in-polygon + line-crossing over tracks | 0 | <1 ms | 0 | CPU | eval-only (PETS/AVSS); ≤1 FA/5 cams/day |
| Abandoned Object | Logic+Cls | Static-region detector + static-owner matching + classifier | 5 MB (cls) | 2–4 ms (on trigger) | 0.1 GB | Orin NX → T4 | det. rate ≥0.85, <1 FA/day/cam (ABODA) |
| Loitering Detection | Logic | Dwell-time FSM over tracks in zone | 0 | <1 ms | 0 | CPU | bounded by tracker IDF1 |
| Anomaly Detection | Open-vocab ML (v2) | YOLO-World-s edge / GroundingDINO-T cloud for unknown classes; VAD optional | 57 / 18 MB | 15–25 ms | 1.0 GB | Orin NX → T4/L4 | object-level prec 0.8–0.9; frame VAD AUC 0.75–0.82 |

**Bonus (in MVP but not in the module list):** Camera Health (FR-116) — blur/tamper/occlusion classifier (~2MB CNN) + decode/FPS stats; add to the registry.

---

## 3. Per-Module Specifications

### 3.1 Person Detection
| Attribute | Spec |
|---|---|
| Model | YOLOv8m (or YOLO11m) with P2 small-object head; 640–1280 adaptive input; part of shared backbone §D1 |
| Dataset | COCO pretrain → FT: CrowdHuman (23k imgs), MOT17/20, MOTSynth (synthetic), own site data (opt-in) |
| Training | Freeze backbone 30 epochs → unfreeze 60 epochs, AMP, mosaic/mixup, loss = box+cls+DFL (v8) with `person` class re-weighted; hard-negative mining (mannequins, shadows, reflections) |
| Inference | TRT INT8, per-camera ROI gating; 5–10 FPS analytics sampling; detector is the entry point for all downstream modules |
| Hardware | Orin NX 16GB (edge); T4 (cloud); no CPU-only deployment for this tier |
| Expected Accuracy | AP50 0.90–0.95 post-FT; target per PRD §9 ≥95% precision for PPE-class pipelines |
| Failure Cases | Distant/tiny persons (>30m), heavy occlusion in crowd, low-light IR flare, heat haze outdoors |
| Optimization | INT8 PTQ (per-class calibration), P2 head, pruning 20% via ultralytics, distil 8m→6s for low-tier boxes |
| Edge Deployment | Triton on Orin NX (all tiers); shared engine across modules (D1) |
| Cloud Deployment | Same engine on T4 for archive re-analysis (Phase 2) |
| GPU Recommendation | Edge: Orin NX/AGX; x86 boxes: RTX 4000 Ada. Cloud: T4 (g5.xlarge) |

### 3.2 Object Tracking
| Attribute | Spec |
|---|---|
| Model | **ByteTrack** (Kalman filter + IoU association, no appearance model); + OSNet-x1.0 ReID (9MB) only at multi-camera handoff (FR-119, Phase 2) |
| Dataset | None for ByteTrack (rule-based); MOT17/20 for benchmark gates; VeRi/OSNet pretrained weights for ReID |
| Training | N/A for ByteTrack; ReID: pretrained OSNet on Market-1501/VeRi, no FT needed at v1 |
| Inference | Per-frame update on GPU-detection CPU path; track state → input to every logic module (zones, dwell, fall, fight, abandoned) |
| Hardware | CPU-bound; zero VRAM |
| Expected Accuracy | MOTA 78–83 / IDF1 75 on MOT17-class scenes; ID stability is the critical metric for loitering/abandoned (D4) |
| Failure Cases | ID switches in dense crowds, long occlusions, camera shake, reappearance after >5s (no re-ID) |
| Optimization | Activation threshold per camera, track-buffer tuning, IoU-down ratio from ByteTrack paper, optional ReID refresh at low IoU |
| Edge Deployment | CPU thread per 4 streams |
| Cloud Deployment | Same code for archive tracking (Phase 2 analytics) |
| GPU Recommendation | None (CPU) |

### 3.3 Weapon Detection
| Attribute | Spec |
|---|---|
| Model | YOLOv8m FT with P2 head; classes: knife, firearm (pistol/rifle). Optional fusion: pose + hand-region crop classifier for knife |
| Dataset | Public: Weapon Detection (Kaggle ~5.5k), Roboflow Gun & Knife (~6k); **critical**: hard negatives (hammers, drills, screwdrivers, pipes — PRD §14 "tool vs weapon"); synthetic composites (COCO-composited weapons in factory scenes) |
| Training | FT from COCO; heavy augmentation (night, rain, IR, motion blur, 10–40m scales); focal loss; **per-site validation set with tool-class recall gate** before registry entry |
| Inference | Detector cascade at 10–40m range; small-object SAHI slicing when detection size < 32px; event only after 3-frame + class-consistency confirm; cloud **GroundingDINO verification** of ambiguous crops (D3) |
| Hardware | Orin NX 16GB minimum; T4 for verify pool |
| Expected Accuracy | mAP50 0.82–0.88; precision ≥0.90 after per-site threshold (PRD FR-101 0.5–0.9 configurable) |
| Failure Cases | Small knives at 40m, hands occluding weapon, dark background silhouettes, screwdriver-class confusion, reflections |
| Optimization | QAT for INT8 (small objects quantize poorly — validate per-class mAP before/after), SAHI only on triggered ROI, defer heavy verify to cloud |
| Edge Deployment | Always-on in high-risk zones (entry, cash, school gates) |
| Cloud Deployment | Verification-only (GroundingDINO on ambiguous crops) |
| GPU Recommendation | Orin NX 16GB; T4/L4 verify pool |

### 3.4 Vehicle Detection
| Attribute | Spec |
|---|---|
| Model | Shared backbone FT; classes: car, truck, bus, motorcycle, bicycle, van |
| Dataset | COCO → BDD100K (100k), UA-DETRAC (140k frames), AI City Challenge |
| Training | Same recipe as 3.1; night/rain augmentation from BDD100K subset |
| Inference | Detector output → vehicle tracks (3.5), speed estimate via homography (calibrated per camera) |
| Hardware | Orin NX (shared engine with PPE/person — zero extra cost) |
| Expected Accuracy | mAP50 0.92–0.95; class-accuracy ≥0.93 |
| Failure Cases | Night glare, partial occlusion in parking, rickshaw/3-wheeler classes missing in IN verticals (add class) |
| Optimization | Shared engine with PPE/person (D1) |
| Edge Deployment | Everywhere |
| Cloud Deployment | Archive counts for analytics (Phase 2) |
| GPU Recommendation | Orin NX → T4 |

### 3.5 Vehicle Tracking
| Attribute | Spec |
|---|---|
| Model | ByteTrack + Vehicle-ReID (ResNet50-ReID, 60MB) only at camera-handoff; LPR as secondary anchor where available |
| Dataset | CityFlow ReID, VeRi-776, AiMotive for ReID head |
| Training | ReID: triplet + ID loss, FT on IN-heavy fleet if available (trucks dominate logistics verticals) |
| Inference | Track ID maintained per camera; handoff: ReID match across camera graph when IoU continuity fails; dwell-time per zone from track timestamps |
| Hardware | Orin NX; +0.3 GB VRAM for ReID head |
| Expected Accuracy | IDF1 0.78–0.82; handoff accuracy ≥0.80 |
| Failure Cases | Same-color truck fleets (ReID weak), plate-less bikes, night (IR washout) |
| Optimization | Plate-assisted association when both visible (LPR tie-break), zone-based affinity weights |
| Edge Deployment | Handoff logic in edge-agent |
| Cloud Deployment | Cross-site vehicle journey (Phase 3) |
| GPU Recommendation | Orin NX → T4 |

### 3.6 Face Detection
| Attribute | Spec |
|---|---|
| Model | **SCRFD-500** (InsightFace) — best speed/accuracy at edge; RetinaFace-mobile as fallback |
| Dataset | WIDER FACE (32k imgs) pretrain; domain: corridor/entry cameras (downward angle, 2–8m) |
| Training | None needed at v1 (pretrained); fine-tune on owned entry-camera data if MAP drops below 0.90 |
| Inference | Triggered in enrollment zones / at entry tripwires only (privacy P4); feeds recognition + liveness |
| Hardware | Orin NX; 0.3 GB VRAM |
| Expected Accuracy | AP 0.90–0.93 (WIDER easy); 5px face detectable at 2–8m |
| Failure Cases | Masks (→ known limitation: attendance mode with mask = no match, configurable), backlight at doors, glasses glare, <20px faces |
| Optimization | INT8 fine (SCRFD quantizes cleanly); run at 1/4 frame rate in non-entry zones |
| Edge Deployment | Always (enrollment zones) |
| Cloud Deployment | Face-search over archive (Phase 2, with consent guardrails §15.3) |
| GPU Recommendation | Orin NX → T4 |

### 3.7 Face Recognition
| Attribute | Spec |
|---|---|
| Model | ArcFace: **MobileFaceNet** embeddings (512-d) on edge; R100 (65M) only for cloud enrollment/re-enrollment quality |
| Dataset | Pretrained MS1MV3/Glint360K (never ship raw); enrollment = customer staff photos via secure pipeline (no training on footage without opt-in, PRD §15.8) |
| Training | No training at v1 — embedding space reused; threshold per site (0.3–0.5 cosine); periodic re-embedding of low-confidence enrollments |
| Inference | Detect → align (SCRFD landmarks) → MobileFaceNet → cosine vs on-box embedding DB (SQLite, encrypted, per-tenant key) |
| Hardware | Orin NX fine; T4 for batch re-embedding |
| Expected Accuracy | TAR ≥0.96 @ FAR 1e-4; attendance-grade: ≥0.98 TAR @ 1e-3 site-tuned |
| Failure Cases | Age/clothing drift, twins (documented), enrollment photo vs camera lighting gap, 45°+ yaw |
| Optimization | 112×112 input, FP16; embedding cache in memory (10k enrollments ≈ 20MB) |
| Edge Deployment | Full attendance loop offline (G1/§14 resilience) |
| Cloud Deployment | Re-embedding, multi-site dedupe, HRIS sync |
| GPU Recommendation | Orin NX → T4 |

### 3.8 Face Verification (1:1)
| Attribute | Spec |
|---|---|
| Model | Same ArcFace MobileFaceNet — pure serving-logic split (1:1 cosine + threshold, never stored comparison) |
| Dataset | None; per-site FAR/FRR calibration set from enrollments |
| Training | N/A |
| Inference | Used for door/access-control pass/fail (FR-110 unauthorized entry support); response < 300ms end-to-end |
| Hardware | Shared with 3.7 (zero extra compute) |
| Expected Accuracy | AUC ≥0.995; choose threshold at site-desired FAR (default FAR 1e-3) |
| Failure Cases | Same as 3.7; add liveness gate always (3.9) |
| Optimization | Reuse 3.7 embeddings |
| Edge Deployment | Local decision; audit event to cloud (compliance) |
| Cloud Deployment | N/A (edge-local decision) |
| GPU Recommendation | Orin NX |

### 3.9 Face Liveness
| Attribute | Spec |
|---|---|
| Model | miniFASNet-class passive RGB anti-spoof CNN (no user interaction needed for CCTV); depth-IR model as upgrade for door mode |
| Dataset | SiW, OULU-NPU, CelebA-Spoof, Replay-Attack; hard negatives: printed photo, tablet/screen replay, paper mask |
| Training | FT on CCTV-angle spoofs; texture + micro-motion features; keep APCER/BPCER gate |
| Inference | Runs only when recognition match confidence is high (gating); score fused into match decision |
| Hardware | Orin NX; 0.1 GB VRAM |
| Expected Accuracy | APCER ≤1.5% / BPCER ≤1% on known attacks; realistic spoof success ≤2% |
| Failure Cases | High-res phone replay on bright screens, doll/mask attacks, very low light |
| Optimization | FP16, 3–5ms; periodic red-team retest (PRD §14 model security) |
| Edge Deployment | Attendance loop is offline-capable |
| Cloud Deployment | Adversarial eval lab |
| GPU Recommendation | Orin NX → T4 |

### 3.10 License Plate Detection
| Attribute | Spec |
|---|---|
| Model | YOLOv8s-FT (plate as class) — simpler than WPOD and reuses toolkit |
| Dataset | Regional mix: CCPD (China), OpenALPR/EU benchmarks, UFPR-ALPR (Brazil), **owned Indian plate set + TILDA-style synthetic** (IN has 4+ font variants — this is the weakest public-data area; plan synthetic-first) |
| Training | Region-pinned models (IN/EU/US per PRD FR-105), synthetic-to-real fine-tune, night/rain aug |
| Inference | Triggered on vehicle tracks; plate crop → rectify (4-point) → OCR (3.11) |
| Hardware | Orin NX; 0.3 GB VRAM |
| Expected Accuracy | mAP50 0.95–0.97 (day); 0.90 night with IR cameras |
| Failure Cases | Dirty/muddy plates, tow trucks, angled mounts, towing, headlight bloom, Indian font/format variance |
| Optimization | INT8 (small crop inputs quantize well), rectify via TRT-compatible warp, per-region engine in registry |
| Edge Deployment | Gate/entry cameras |
| Cloud Deployment | Archive batch LPR (Phase 2) |
| GPU Recommendation | Orin NX → T4 |

### 3.11 Plate OCR
| Attribute | Spec |
|---|---|
| Model | **LPRNet** (end-to-end, 2.2M params) on edge; **PP-OCRv4-mobile** as x86/CPU fallback (more robust, higher VRAM-free) |
| Dataset | Synthetic plates (TILDA-style random fonts/backgrounds) + regional real sets; post-FT per region |
| Training | CTC loss, synthetic-first then 10–20k real plates/region; punctuation/state-code handling per region |
| Inference | Sequence decode + regex validation per region (IN: `XX-NN-XXXX` variants); confidence + N-best candidates |
| Hardware | Orin NX; 0.2 GB VRAM; CPU-capable via PP-OCRv4 |
| Expected Accuracy | char 0.96–0.98 clean; 0.88–0.93 night/rain/motion blur; full-plate exact 0.85–0.92 |
| Failure Cases | Ambiguous chars (0/O, 8/B, 1/I), partial occlusions, trailer plates |
| Optimization | Small input (94×24), INT8; dictionary post-filter; plate-level re-inference on second frame if regex fails |
| Edge Deployment | Gate control offline-safe |
| Cloud Deployment | Whitelist sync + audit |
| GPU Recommendation | Orin NX (GPU) / x86 CPU (PP-OCRv4) → T4 |

### 3.12 Fire Detection
| Attribute | Spec |
|---|---|
| Model | EfficientNet-B0 classifier on candidate flame regions + **3-frame temporal confirm**; RegionProposalNet for candidate generation (motion + color prior) |
| Dataset | DFire (fire+smoke), FiSmo, "Flame and Smoke" Kaggle; hard negatives: welding arcs, orange vehicles, sunset glare, red LED |
| Training | Two-stage: object-level (cropped flame) classification; augment at night/IR; class-balanced + focal |
| Inference | Motion-gated candidates only (cost gate); state machine: 3 consecutive confirms → event (≤5s, FR-113); confidence aggregated over sequence |
| Hardware | Orin NX; 0.3 GB VRAM |
| Expected Accuracy | Precision 0.93–0.96 / recall 0.90 with temporal confirm; night recall drops to ~0.85 (IR profile) |
| Failure Cases | Welding sparks (top false positive — needs site-specific negative mining), moving red trucks, heater glow, sun flare |
| Optimization | INT8 3–5ms; candidate gate cuts 90% of compute; **complements not replaces fire panels** (PRD FR-113) |
| Edge Deployment | Always |
| Cloud Deployment | Verify crops with RT-DETR-fire variant (D3) |
| GPU Recommendation | Orin NX → T4 |

### 3.13 Smoke Detection
| Attribute | Spec |
|---|---|
| Model | D-Fire smoke classifier + **motion/rising-pattern gating** (smoke has characteristic slow upward drift) |
| Dataset | DFire, FiSmo smoke subset; hard negatives: steam, fog, dust, exhaust, incense |
| Training | Same as 3.12; temporal branch: lightweight motion statistics as auxiliary input |
| Inference | Candidate → 5-frame confirm with direction-of-motion consistency; longer window than fire (smoke is slower) |
| Hardware | Orin NX (shared with 3.12) |
| Expected Accuracy | Precision 0.88–0.92 / recall 0.85 — the hardest vision class (textureless, translucent) |
| Failure Cases | Steam vents, fog, industrial exhaust, low-light smoke invisible to RGB (note: IR cameras help) |
| Optimization | Confidence floor higher than fire (0.7 default), per-zone masking for known steam sources |
| Edge Deployment | Always |
| Cloud Deployment | Verify optional |
| GPU Recommendation | Orin NX → T4 |

### 3.14 PPE Detection (incl. Helmet 3.15, Vest 3.16)
| Attribute | Spec |
|---|---|
| Model | Shared backbone FT; classes: person, helmet, vest, mask, gloves, safety glasses, boots (FR-106 six-item matrix); helmet/vest are sub-classes of the same model — no separate networks |
| Dataset | SHWD (7.5k helmet), SODA (helmet+vest), CVPPA, HardHat (7k), Roboflow PPE sets + **owned construction/factory footage (opt-in)**; class-confusion mining: head vs helmet, shirt vs vest |
| Training | Per-class re-weighting (rare classes: gloves, glasses, boots need synthetic/site data); part-level supervision (helmet bbox → attach to person track); per-zone matrix eval (welding bay = all 6, PRD FR-106) |
| Inference | Person track + attachment logic (helmet/vest must overlap person bbox region) — this kills most false positives; per-zone PPE matrix from config-svc; daily compliance % aggregation |
| Hardware | Orin NX (shared engine, D1) |
| Expected Accuracy | Helmet mAP50 0.92, vest 0.90; PPE precision ≥0.95 (PRD §9); recall ≥0.90 |
| Failure Cases | Helmet carried in hand, vest worn open/under jacket, head-only views (person 30% visible), dark vest vs dark background, machinery occlusion |
| Optimization | Attachment-based gating (compliance decision, not raw bbox), per-zone models for difficult sites, QAT INT8 |
| Edge Deployment | Continuous in production zones |
| Cloud Deployment | Compliance analytics + reports (FR-117) |
| GPU Recommendation | Orin NX → T4 |

### 3.15 Helmet Detection
| Attribute | Spec |
|---|---|
| Model | Same PPE model (class: helmet) — no separate network |
| Dataset | SHWD, SODA (see 3.14) |
| Training | Per 3.14 |
| Inference | Per 3.14 attachment logic |
| Hardware | Shared |
| Expected Accuracy | mAP50 0.92 |
| Failure Cases | Head vs helmet confusion in low res, turbans/caps |
| Optimization | Per 3.14 |
| Edge Deployment | Per 3.14 |
| Cloud Deployment | Per 3.14 |
| GPU Recommendation | Orin NX |

### 3.16 Vest Detection
| Attribute | Spec |
|---|---|
| Model | Same PPE model (class: vest) — no separate network |
| Dataset | SODA (see 3.14) |
| Training | Per 3.14 |
| Inference | Per 3.14 attachment logic |
| Hardware | Shared |
| Expected Accuracy | mAP50 0.90 |
| Failure Cases | Shirt-vs-vest confusion, vest worn open |
| Optimization | Per 3.14 |
| Edge Deployment | Per 3.14 |
| Cloud Deployment | Per 3.14 |
| GPU Recommendation | Orin NX |

### 3.17 Fall Detection
| Attribute | Spec |
|---|---|
| Model | **RTMPose-m keypoints** + posture/motion FSM; video-3D-CNN rejected (too heavy, less interpretable) |
| Dataset | Le2i (191 videos), UR Fall, UP-Fall; hard negatives: sitting down, crouching, lying-down workers, wheelchair |
| Training | Pose model pretrained (COCO-WholeBody) — no FT needed; FSM thresholds tuned per site (hospital vs warehouse differ) |
| Inference | Per-person: posture angle + vertical velocity + floor-contact; **state machine: sustained ≥1.5s posture change** (PRD FR-111) → event; auto-escalate to emergency contact ≤5s (FR-111) |
| Hardware | Orin NX; 0.8 GB VRAM |
| Expected Accuracy | Precision 0.92 / recall 0.95 (Le2i/UR); degraded at oblique camera angles (<40° pitch) |
| Failure Cases | Sitting/crouching false positives (top issue), lying-down as normal (bed/stretcher zones need masking), occlusions in crowd, high-angle cameras |
| Optimization | Pose runs on person crops only (gated), FP16; optional second-camera cross-check for high-risk wards |
| Edge Deployment | Always (hospitals, warehouses) |
| Cloud Deployment | Fall incident audit + training data for FSM tuning |
| GPU Recommendation | Orin NX → T4 |

### 3.18 Fight Detection
| Attribute | Spec |
|---|---|
| Model | Logic over pose/tracks: 2+ persons within interaction radius + sudden velocity/acceleration cluster + limb-motion score; optional ML scorer (Tiny GNN on pose sequences, ~2MB) |
| Dataset | RWF-2000, Hockey Fight, AIC; negative mining: hugging, dancing, rough play, crowd jostling (PRD risk §13) |
| Training | If ML scorer used: sequence classification (temporal windows 30–60 frames); else pure rule thresholds |
| Inference | Trigger on track-cluster conditions → 1s evidence window (FR-112 10s pre-event buffer via ring buffer) → confirm |
| Hardware | Orin NX (shared pose, 3.17) |
| Expected Accuracy | Prec/rec 0.85–0.92 (RWF-2000); site-tunable |
| Failure Cases | Play-fighting (per-site tolerance), crowd pushing events, poor lighting at night |
| Optimization | Zone-scaled sensitivity (school playground vs factory floor), optional audio cross-check (Phase 3 roadmap) |
| Edge Deployment | Always |
| Cloud Deployment | Verify ambiguous clusters |
| GPU Recommendation | Orin NX → T4 |

### 3.19 Crowd Detection (Density)
| Attribute | Spec |
|---|---|
| Model | **Person-count + perspective calibration map** (regression head per camera grid cell); CSRNet-lite (16M→pruned 3M) only for mall verticals |
| Dataset | ShanghaiTech B, UCF-QNRF, JHU-CROWD++; per-site calibration frames (5–10 labeled frames per camera at onboarding) |
| Training | Count-regression FT with per-camera calibration (density = f(bbox size, location) homography-like mapping) |
| Inference | Count from detections + calibration map (no extra network in v1); density levels low/med/high/critical (FR-114); capacity alerts from zone capacity config |
| Hardware | Orin NX; 0.1 GB VRAM (v1 uses detection count) |
| Expected Accuracy | MAE 8–15% (ShanghaiTech B); critical threshold accuracy ≥0.90 |
| Failure Cases | Dense occlusion (head-count underestimates >50% at critical density — known limitation), perspective extremes |
| Optimization | Detection-based counting reuses 3.1 (zero cost); density CNN only when count > threshold (cascade) |
| Edge Deployment | Real-time alerts |
| Cloud Deployment | Heatmaps + hourly occupancy (FR-115, Phase 2 analytics-svc) |
| GPU Recommendation | Orin NX → T4 |

### 3.20 Zone Intrusion
| Attribute | Spec |
|---|---|
| Model | **None — geometric logic engine** (D4): point-in-polygon, tripwire line-crossing, dwell-in-zone on track geometry |
| Dataset | None (no training); eval on PETS 2009, AVSS, own synthetic scenes |
| Training | N/A; per-zone rules from config-svc (FR-203 rule builder) |
| Inference | Track → zone state machine → intrusion/exit events with direction; night/day/rain profiles (FR-109) |
| Hardware | CPU (<1ms) |
| Expected Accuracy | Deterministic given tracker; system false-alarm SLO ≤1/5 cams/day |
| Failure Cases | Tracker ID-loss at zone borders, shadow triggers (mitigate: motion+detection fusion), animals (option: person-class gate per zone) |
| Optimization | Zone hysteresis (enter/exit margins), anti-shake filtering, class-gated zones |
| Edge Deployment | Instant, offline-safe (life-safety) |
| Cloud Deployment | Config + audit |
| GPU Recommendation | None (CPU) |

### 3.21 Abandoned Object Detection
| Attribute | Spec |
|---|---|
| Model | Static-region detector (background-subtraction + object detector on static blobs) + **owner-track association** + object classifier (bag/box/suitcase) |
| Dataset | PETS 2006/2007, AVSS 2007, ABODA, VIRAT; synthetic scene compositing |
| Training | Classifier only (3–5MB); static logic is handcrafted |
| Inference | Object static > T1 (30–60s) while its owner track leaves the scene → "unattended object" event; alert escalation at T2 (configurable) |
| Hardware | Orin NX; 0.1 GB VRAM (classifier) |
| Expected Accuracy | Detection rate ≥0.85, <1 false alarm/day/camera (ABODA-class scenes) |
| Failure Cases | Parked equipment/wheelbarrows (site-specific allowlist), workers standing still beside object (owner association breaks), periodic vacating (warehouse docks need zone exclusion) |
| Optimization | Owner-matching radius, scene-change learning (weekly background refresh), per-zone allowlists |
| Edge Deployment | Always |
| Cloud Deployment | Evidence clip + audit |
| GPU Recommendation | Orin NX → T4 |

### 3.22 Loitering Detection
| Attribute | Spec |
|---|---|
| Model | **None — dwell FSM over tracks** (D4): track-in-zone duration with ID stability; two-threshold escalation (FR-108, 30s–10min configurable) |
| Dataset | None; eval VIRAT, own |
| Training | N/A; thresholds per zone (ATM vs parking lot differ) |
| Inference | Track enters zone → clock; duration > T1 → notice alert; > T2 → escalated alert with pre-event clip |
| Hardware | CPU (<1ms) |
| Expected Accuracy | Bounded by tracker IDF1 (3.2); with ByteTrack, dwell accuracy ±2s at 5 FPS |
| Failure Cases | ID switches reset the clock (mitigate: ReID refresh in loiter zones), guards/employees standing post |
| Optimization | Zone-exclusion lists, "known person" discounting via face module (optional) |
| Edge Deployment | Always |
| Cloud Deployment | Loitering analytics dashboards |
| GPU Recommendation | None (CPU) |

### 3.23 Anomaly Detection
| Attribute | Spec |
|---|---|
| Model | Two-tier: (a) **object-level**: open-vocabulary detector **YOLO-World-s** on edge (unknown/unusual objects → alert + cloud verify), **GroundingDINO-T** on cloud; (b) behavior-level frame-VAD (PredNet-class) — **deferred to Phase 2/3** (honest caveat: frame-level VAD AUC 0.75–0.82 on UCF-Crime, too noisy for life-safety) |
| Dataset | UCF-Crime, ShanghaiTech Campus (research only, for VAD); object-level: LVIS/COCO vocabulary + per-tenant custom vocab lists |
| Training | YOLO-World: zero-shot (no training); VAD: if adopted, supervised on owned curated events |
| Inference | Edge: YOLO-World-s at 1 FPS in low-traffic hours or configurable schedules; matched unknown-class → event → cloud GroundingDINO confirm |
| Hardware | Orin NX; 1.0 GB VRAM (gated) |
| Expected Accuracy | Object-level precision 0.8–0.9 on curated vocab; VAD AUC 0.75–0.82 (not production-grade for safety) |
| Failure Cases | Everything is "anomalous" in a construction site (vocab curation is the real work); adversarial inputs |
| Optimization | Vocabulary gating per site, cloud-only verification to keep edge cost off the critical path |
| Edge Deployment | Gated (schedules, low-traffic hours) |
| Cloud Deployment | Verify + model-vocab updates via registry |
| GPU Recommendation | Orin NX (gated) → T4/L4 |

---

## 4. Temporal Confirmation Layer (applies to all detectors)

| Module | Confirmation rule (before event emission) |
|---|---|
| Fire/Smoke | 3–5 consecutive frames, class-consistent |
| Weapon | 3 frames + cloud verify for ambiguous |
| Fall | ≥1.5s sustained posture + motion profile (FR-111) |
| Fight | 1s motion cluster window |
| Intrusion/loitering | crossing/dwell state machines |
| PPE violation | 2 detections within 5s window (avoids flicker) |

Event confidence = aggregated over the confirmation window (max + track-lifetime average), per-site threshold from config-svc.

---

## 5. Tool Recommendations — Verdicts

| Tool | Verdict | Why |
|---|---|---|
| **YOLO (Ultralytics)** | **USE — backbone of 10+ modules** | Best ecosystem, TRT/ONNX export, P2 small-object head, easiest hard-negative fine-tuning. **License: AGPL-3.0** → proprietary SaaS needs Ultralytics enterprise license OR switch to Apache-2.0 (RT-DETR, D-FINE). Decide in Week 1 (D6) |
| **RT-DETR** | **USE selectively** | Apache-2.0 (license win), superior small-object/dense scenes, better at 10–40m weapons; too slow for 8–32 stream edge (30–45ms vs 10–14ms) → cloud-verification tier + hard-scene edge sites |
| **OpenCV** | **USE (never for inference)** | Decode/preprocess/postprocess, geometry (point-in-polygon, tripwires), visualization; CPU DNN as last-resort fallback only |
| **DeepSORT** | **AVOID on edge, USE at Phase-2 handoff** | Needs ReID on every track (3–5ms extra per frame, no benefit single-cam); keep for multi-camera ID continuity (FR-119) with OSNet |
| **ByteTrack** | **USE — default tracker** | Model-free, 1–3ms CPU, robust in dense scenes, MOT17 MOTA ~80%; pairs with ReID only at handoff (D5) |
| **ArcFace** | **USE** | The recognition standard; MobileFaceNet (4MB) at edge, R100 in cloud; no viable alternative that beats it at this size/accuracy |
| **InsightFace** | **USE** | SCRFD detection + ArcFace + liveness models + ONNX/MTTK tooling, MIT license, active — saves ~3 months of face-stack work |
| **TensorRT** | **USE — mandatory (PRD §14)** | 2–5× over ONNX on Jetson; INT8 via PTQ+QAT; engine caching, multi-stream; the only way to hit 8–32 × 4K streams per box |
| **ONNX** | **USE as interchange + CPU runtime** | Single artifact format across TRT/OpenVINO/onnxruntime; not the edge performance end-state on Jetson |
| **DeepStream** | **DEFER to ≥16-stream boxes** | Zero-copy decode→infer pipeline is real, but adds plugin complexity and NVIDIA coupling; Triton+PyAV path covers MVP (ARCHITECTURE §7); revisit for Phase-3 64-stream boxes |
| **PyTorch** | **USE for training/research only** | Training, fine-tuning, torch.compile → export; never raw serve on edge (1.5–2× slower than TRT) |
| **OpenVINO** | **USE for non-NVIDIA x86 only** | Intel CPU/iGPU/NPU boxes (hardware abstraction, P7); ONNX→IR; skip on Jetson/x86+NVIDIA |
| **Label Studio** | **USE for classification + review UX** | Fast classification/regression labeling, ML-assisted, API-first; weaker on video tracks |
| **CVAT** | **USE — primary video annotation tool** | Best-in-class track interpolation, segment tracking, multi-annotator server, auto-annotation with our own models; the workhorse for all 23 modules' video data |
| **Roboflow** | **USE for dataset ops (self-hosted)** | Versioning, augmentation, export to YOLO/COCO for every module; **AGPL + data-residency** → keep customer footage off hosted Roboflow; use for public datasets only; CVAT on-prem for sensitive data |

---

## 6. Hardware & GPU Matrix

| Tier | Hardware | Streams | Model load | Power | Use for |
|---|---|---|---|---|---|
| Edge S | Jetson Orin NX 16GB | 8–16 @ 5–10 FPS | Full stack (det + pose + face + lpr + cls) ~5–6 GB VRAM | 15–25W | MVP reference (PRD §10) |
| Edge M | Jetson AGX Orin 64GB | 16–32 @ 4K | Full stack + YOLO-World anomaly | 40–60W | Enterprise sites |
| Edge L | x86 (i7-13th/Xeon E-2400) + RTX 4000 Ada 20GB / RTX 4070 Ti | 16–32 | Same, +OpenVINO fallback on iGPU | 150–250W | High-res 4K sites, existing hardware preference |
| Cloud burst | g5.xlarge (T4 16GB) per region | — | Verify tier (RT-DETR/GroundingDINO), batch LPR/face search | — | Phase 2 archive features |
| Training | SageMaker g5.24xlarge (A10G) or p4d (A100 40GB) | — | Fine-tunes, synthetic generation, eval gates | — | training-svc |

**Edge latency budget (single stream, Orin NX):** decode 2–4ms + gate 0.5ms + detector 10–14ms + pose (when gated) 8–12ms + face/lpr (on trigger) 8–15ms + logic 1–2ms ≈ **20–35ms analytic pass** → 5–10 FPS analytics × 8–16 streams fits with Triton dynamic batching (ARCHITECTURE §5.3).

---

## 7. Training & Data Strategy

1. **Family pipelines:** detector (COCO → domain FT → hard-negative round 2 → QAT), face (zero training, threshold calibration), LPR (synthetic-first TILDA + regional real), temporal (sequence-level eval gates).
2. **Dataset program (PRD §14):** per-vertical benchmark sets with versioned S3 storage + checksums; hard-negative programs are the #1 precision lever for weapon (tools), PPE (head vs helmet), fire (welding), fight (hugs/jostle), smoke (steam).
3. **Labeling:** CVAT on-prem for video (tracks/interpolation), Label Studio for classification/QA, Roboflow (self-hosted) for dataset versioning/augmentation; SOC ack/reject feedback (ARCHITECTURE §5.1) feeds eval-svc.
4. **Registry gates (eval-svc):** per-vertical precision/recall targets (PRD §9) before a model version can be staged; shadow mode on live traffic; auto-rollback.

---

## 8. Roadmap Mapping

- **MVP (models):** shared detector (person/weapon/PPE/vehicle/fire-hotspot), pose (fall/fight), face stack (SCRFD+ArcFace+liveness), fire/smoke classifiers, all logic modules, camera health → **12 engines, not 23**
- **Phase 2 (PRD §11):** LPR stack (LPD+LPRNet), vehicle ReID, crowd density, face search (cloud R100), OSNet multi-cam
- **Phase 3:** YOLO-World anomaly + GroundingDINO verify tier, DeepStream at 64-stream, marketplace fine-tunes

---

## 9. Key Risks & Open Decisions

| Risk / Decision | Impact | Mitigation |
|---|---|---|
| Ultralytics AGPL licensing (D6) | Legal — blocks proprietary SaaS | Week-1 decision: Ultralytics enterprise license vs Apache-2.0 stack (RT-DETR/D-FINE) |
| Weapon small-object INT8 drift | Accuracy at 10–40m | Per-class QAT validation gates; SAHI on triggered ROI; cloud verify |
| Smoke false positives on steam sites | Alert fatigue | Per-zone masking for known steam sources; higher confidence floor |
| Indian plate data scarcity | LPR quality in IN vertical | Synthetic-first (TILDA-style) + regional real fine-tune |
| Face threshold drift per site | Attendance errors | Monthly calibration job (eval-svc) with FAR/FRR report |
| Crowd counting underestimates at critical density | Safety false-negatives | Cascade to density CNN; documented limitation; calibration map |
| Frame-level VAD noise | Anomaly false alarms | Object-level open-vocab first; VAD deferred to Phase 2/3 |
