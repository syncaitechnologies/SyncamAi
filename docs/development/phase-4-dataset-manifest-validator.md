# Phase 4: future dataset-manifest validator

T-0363 adds a pure in-memory validator for one caller-supplied future
held-out-evaluation dataset manifest. It requires the Phase 4 provenance fields
defined by T-0362: source, license, capture-date range, tenant-consent class,
DVC commit, checksum manifest, and labeling QA of at least 5%.

The validator maps the manifest to one canonical Phase 4 capability, rejects
blank required metadata, requires the QA rate to be at most 100%, and rejects a
development or test dataset that declares production customer data. A manifest
that declares customer footage must include a non-blank documented opt-in
reference.

This is a metadata-shape and policy-boundary validator only. It does not load
any manifest, inspect a DVC remote or checksum, resolve a source or license,
verify consent or approval, or create or claim any dataset, footage, label,
model artifact, evaluation result, or promotion. A future governed integration
must supply real approved metadata and independently verify every reference.
