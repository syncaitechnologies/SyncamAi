# Real-camera three-feature prototype: three-week target

Owner clarification on 2026-09-29 UTC: use actual iPhone Chrome camera images,
not scripted observations; detect people, firearms and knives, and identify
previously enrolled consenting people by name. Canada is the target country.
Full registry delivery and production CCTV rollout are out of scope.

This is a conditional engineering target, not a guaranteed deadline or an
automatic background work schedule. Each slice needs local verification,
a draft PR, review/merge and runtime validation. Delayed approvals, unsuitable
models, hardware limits or failed evaluation can move the deadline. No paid
subscription, commercial model licence or cloud resource has been purchased.

## Milestones and measurable exit criteria

| Target | Deliverable | Exit criterion |
|---|---|---|
| Week 1 | Real phone input and person detector | On the actual iPhone, Start opens the rear camera and a real approved model draws person boxes/confidence. Empty scenes produce no boxes; errors clear stale results; Stop/background ends capture and inference. Measure latency and memory on that phone and laptop. |
| Week 2 | Firearm and knife candidates | An approved model sees controlled safe test props/authorized evaluation images and tool negatives. Three class-consistent observations produce a human-review candidate, not a certainty/safety conclusion. Record missed and false candidates, thresholds and model version; evaluate latency instead of inventing FPS or accuracy. |
| Week 3 | Enrolled-name recognition and rehearsal | Only after Phase 5 approvals: authorized enrollment of opted-in adults, isolated encrypted templates, real matching and evaluated liveness. Enrolled person, unenrolled person, withdrawal/deletion, ambiguous match and photo/screen spoof cases pass the approved criteria. Rehearse the complete phone journey and verify the chosen laptop/hosted setup. |

An approved test set and thresholds must be declared before running inference.
These phone tests do not establish FR-101's 10-40m or production SLOs, nor
FR-102 attendance/payroll readiness. Unknown/ambiguous identities remain
unknown or review-required. No autonomous emergency, access or employment action.
Use inert props and a controlled private scene; no need to obtain live firearms.

## Current first slice: T-0433

The **Phone camera** workspace connects a real local browser camera after
explicit Start. It requests no microphone and does not record, extract,
store or transmit frames. It stops on page hiding, navigation, Stop and camera
changes, including late permission responses. Camera streaming and model
inference are presented as separate statuses: all three models remain
unavailable in this slice. This is genuine camera input, not a completed AI
feature, and must not be presented as one.

For a demo-mode frontend open `?camera=local`, or use **Phone camera** in
navigation. Live data mode retains the existing authenticated entry gate and
does not use this query to bypass it. HTTPS is required on the phone; laptop
localhost is only a local development exception. HTTP access through a LAN
IP is not an iPhone delivery solution. Browser permission remains user-owned.

## Blocking decisions and evidence

1. **Model authority:** ADR-001 is still Proposed. Legal/CTO/AI must approve
   exact implementations, weights, dataset provenance, versions, hashes,
   model cards, evaluation and release/rollback scope. No default model or
   unreviewed automatic download is permitted. Package allowlist review is
   also needed for any new runtime dependency.
2. **Weapon weights:** the existing three-observation library consumes
   candidate metadata, not pixels. A generic detector's kitchen-knife class
   is not proof of firearm support or of weapon-vs-tool reliability. A
   specialist licensed detector or an evaluated open-vocabulary candidate
   must cover both classes before Week 2 can be called complete.
3. **Biometric authority:** the
   [Phase 5 boundary](phase-5-consent-attendance-boundary.md) blocks actual
   enrollment/recognition/liveness until named Product, Privacy/Legal,
   Security and AI approvals are recorded. Canada's
   [OPC business guidance](https://www.priv.gc.ca/en/privacy-topics/health-information-genetics-biometrics/biometrics/gd_bio_org-final/)
   treats meaningful, purpose-specific consent and safeguards as important;
   ordinary camera permission is not consent to biometric processing.
   Province, authorized site, purpose, participants, retention, withdrawal,
   alternative participation and data-processing location remain unresolved.
   This is a review input, not a legal compliance verdict.
4. **Runtime and hosting:** inspected laptop CPU is Intel i7-1165G7 with
   Iris Xe graphics and 11.7 GiB OS-visible RAM, not a verified NVIDIA inference host. Inference speed
   has not been measured. Confirm iPhone model/iOS, RAM and whether the
   first presentation can rely on this laptop. A laptop-off version needs
   either proven in-phone inference or a reviewed hosted runtime. Vercel
   frontend reachability alone is not inference availability.
5. **Image path:** before any laptop/server upload, define authentication,
   tenant/site/session authorization, payload/time limits, TLS, CORS, no
   footage logging/retention and disconnect cleanup. Existing edge mTLS
   endpoints are not repurposed for browser camera traffic. Biometrics must
   never enter the generic event payload.

## Research candidates, not approved models

Reviewed primary sources on 2026-09-29 UTC; no artifacts downloaded.

- Person: publisher [RT-DETR R18 model card](https://huggingface.co/PekingU/rtdetr_r18vd)
  labels its COCO-trained model Apache-2.0. It is a candidate for an approved
  person benchmark, not a selected or measured iPhone runtime. Exact revision,
  artifact checksum, data provenance and conversion licence need review.
- Weapon: publisher [Grounding DINO tiny card](https://huggingface.co/IDEA-Research/grounding-dino-tiny)
  describes text-conditioned object detection. It may be evaluated for
  review-required firearm/knife candidates after approval; it is not a
  validated specialist weapon model and no accuracy claim is made.
- Face: [InsightFace's model policy](https://github.com/deepinsight/insightface/blob/master/python-package/docs/model_zoo.md)
  separates MIT library code from non-commercial pretrained weights. The
  original architecture's code-licence label cannot authorize those weights
  for a commercial client demonstration. Obtain appropriate terms or review
  an alternative with verified weight and training-data rights, then obtain
  biometric and liveness approval. An embedding network alone is insufficient.
- Browser execution: [Transformers.js](https://huggingface.co/docs/transformers.js)
  offers browser inference. Assess a small approved model with WASM fallback,
  pinned artifacts, worker/resource bounds and explicit disposal before
  choosing it over canonical Python 3.12 AI processes for this prototype.

The local camera implementation follows
[ADR-014](../adr/ADR-014-real-phone-camera-prototype.md).
ADR-013 and T-0432 remain synthetic-only work; their merge does not satisfy
any exit criterion above.

## Test matrix for each subsequent slice

- Physical iPhone Chrome: grant/deny, rear/front camera, rotate, background,
  lock/unlock, network change, permission revocation, Stop/restart and cleanup.
- Real-model tests: actual approved pixels and published runtime version;
  no injected box metadata or scripted output counted as detection evidence.
- Security: unauthorized sessions/sites denied, no secrets in browser,
  stale/out-of-order responses discarded, bounded payload/rate limits,
  no frame/embedding data in logs and no cross-tenant matches.
- Face: informed enrollment, unknown, ambiguous, withdrawn, deleted and
  spoofed inputs, review/correction; no fake liveness checkbox.
- Full `scripts/verify.ps1`, Go coverage >=80%, dependency/secret checks,
  reproducible build, deployed endpoint/phone validation and draft PR evidence.

Physical camera/model evidence belongs in approved private storage, not the
public repository. This plan does not record a model licence purchase, legal
sign-off, consent, successful inference or physical iPhone verification.
