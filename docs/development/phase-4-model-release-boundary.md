# Phase 4: model-release provenance boundary

T-0357 adds immutable, planned-only release prerequisite records for every
canonical AI capability. Each record references ADR-001 and requires an
Apache-2.0 license, weight provenance, a model card, signature, rollback
metadata, and recorded legal approval before a future external model can be
considered.

External-model promotion is explicitly blocked because ADR-001 remains
proposed and requires Legal approval. The records do not accept evidence or
implement artifacts, datasets, signing, distribution, inference, evaluation,
staging, promotion, or production enablement. Those operations remain separate
security-reviewed work after the necessary approvals.
