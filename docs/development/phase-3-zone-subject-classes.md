# Phase 3 zone subject-class gates

A rule zone may carry an optional `subject_classes` allowlist. Its bounded
canonical values are `person`, `bicycle`, `bus`, `car`, `motorcycle`, `truck`,
and `van`. An omitted or empty list preserves the existing behavior of
evaluating every locally supplied tracker class. Unsupported or duplicate
values fail closed; no identity, biometric, plate, ReID, appearance, or animal
classification is introduced.

The allowlist is normalized, idempotent, audited, optimistic-versioned, and
persisted under the tenant RLS policy. It is included in immutable edge
configuration snapshots. A class that is not allowed for a rule is ignored
before that rule updates any local state, so it cannot produce an event or
affect a later allowed subject's state.

This remains configuration for deterministic local geometry rules only. It
does not add detector weights, raw frames, tracking, event transport, or
autonomous action. Privacy-mask zones remain reserved for the final dedicated
security-reviewed approval and pre-encode verification slice.
