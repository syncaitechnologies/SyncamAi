# Phase 4 weapon-review confirmation boundary

T-0420 implements a synthetic-safe temporal boundary for future FR-101
detector metadata. It accepts only tenant/site/camera/zone-bound ordered
batches with at most 64 normalized candidates. The internal class vocabulary
is restricted to `knife` and `firearm`, matching the canonical architecture;
tool, identity, biometric, plate, embedding, pixel, crop, and free-form labels
fail closed.

One candidate must remain class-, model-version-, and spatially consistent for
three consecutive frames no more than one second apart. Matching uses bounded
same-class IoU with deterministic tie-breaking. The event confidence is the
minimum across the confirmation streak. Unconfirmed state expires on a miss;
emitted state is retained for a bounded one through 30 frames to suppress
immediate duplicates. A runtime holds at most 256 candidate states (128 by
default), and all validation, ordering, scope, and capacity failures occur
before state or safe counters change.

The output uses the existing `weapon_review` event contract with empty
evidence references and mandatory pending human review. Candidate IDs,
bounding boxes, object class, identity, automatic response, and dispatch
instructions are not emitted. This boundary cannot sound an alarm, contact
emergency services, control access, or make a final threat determination.

Tests use synthetic metadata only. They cover three-frame confirmation,
minimum confidence, duplicate suppression, gap/class/spatial reset, replay,
scope, malformed input, and atomic capacity rejection. There is no detector,
model weight, dataset, image, customer footage, evidence object, benchmark,
hard-negative evaluation, per-site threshold, model activation, or deployment.
ADR-001 remains proposed, and real FR-101 operation still requires an approved
Apache-compatible model release, provenance, held-out tool-vs-weapon testing,
hardware evidence, human oversight, and controlled promotion.
