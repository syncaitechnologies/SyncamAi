# Phase 4: planned dataset-provenance boundary

T-0362 records the non-executable prerequisites for a future held-out evaluation
dataset for each of the canonical Phase 4 capabilities. Each future manifest
must record source, license, capture-date range, tenant-consent class, DVC
commit, checksum manifest, and a labeling-QA sample rate of at least 5%.

The requirements preserve OD-03: DVC pointers and checksum manifests identify
write-once dataset versions; corrections require a new version. Customer footage
requires documented opt-in and production customer data is prohibited from dev
and test datasets. A future eval gate must verify the exact held-out dataset
version and its license before a model can be promoted.

This slice contains requirements only. It creates no dataset, DVC remote or
pointer, checksum, footage, labels, consent record, model artifact, evaluation
result, promotion, or production-enablement control. T-0112 remains the work
that must create and independently validate actual held-out datasets.
