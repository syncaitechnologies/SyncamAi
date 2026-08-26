# Phase 4: synthetic planned-registry catalog

T-0360 publishes a versioned, in-memory catalog derived from the immutable
Phase 4 registry projection. Its mode is explicitly `synthetic_read_only`;
all capabilities retain their planned evaluation status and the ADR-001
external-model-promotion block.

The catalog is not an API or live control surface. It does not load model
artifacts, accept datasets or evidence, perform inference or evaluation,
activate, release, or promote a model. ADR-001 Legal approval and a separate
security-reviewed release workflow remain prerequisites for those operations.
