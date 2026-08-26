# Phase 4: planned model-evaluation gates

T-0356 adds immutable, planned-only evaluation metadata for the 23 canonical
AI modules and the Camera Health bonus. Every entry maps exactly once to the
model registry and specifies evaluation categories plus the mandatory release
gates: licensed held-out evaluation, safety review, and human oversight.

The categories are planning inputs derived from the architecture's training and
data strategy. They are not datasets, test executions, measured metrics,
evaluation cards, model artifacts, inference code, promotion decisions, or
production enablement. A later, security-reviewed slice must record evidence,
perform the approved evaluation and promotion workflow, and exercise rollback
before any capability can be staged or released.
