# Phase 3 zone-runtime activation

`T-0333` connects the validated configuration-delivery boundary to the local non-mask zone-rule runtime.

- The Go edge agent remains the trust boundary for mTLS pull, SHA-256 validation, and durable atomic file activation. The runtime accepts only that already-verified metadata payload.
- A candidate revision is fully parsed and compiled before it replaces the active evaluator. Malformed, stale, or mask-bearing candidates fail closed; the last known-good runtime remains active.
- The runtime emits no zone events before a valid configuration is active. Configuration activation is serialized with rule evaluation so one observation cannot be evaluated against a partial candidate.

This does not add detector weights, tracker transport, frame processing, or model promotion. Those workstreams remain separately gated by the Apache-compatible model/provenance decision in ADR-001, privacy validation, and hardware-in-loop tests.
