"""Planned-only model release prerequisites derived from ADR-001.

This module records the evidence required before a future external model may be
considered. It neither accepts evidence nor loads, signs, distributes, stages,
or promotes a model artifact.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.model_registry import CAPABILITY_COUNT, MODEL_REGISTRY


LEGAL_GATE_REFERENCE = "docs/adr/ADR-001-model-license.md"
ALLOWED_MODEL_LICENSES = ("Apache-2.0",)
EXTERNAL_MODEL_PROMOTION_STATUS = "blocked_pending_legal_approval"
REQUIRED_RELEASE_METADATA = (
    "license",
    "weight_provenance",
    "model_card",
    "signature",
    "rollback_metadata",
    "legal_approval",
)


@dataclass(frozen=True)
class ModelReleaseBoundary:
    """The non-executable release prerequisites for one capability."""

    capability_identifier: str
    legal_gate_reference: str
    allowed_model_licenses: tuple[str, ...]
    required_release_metadata: tuple[str, ...]
    external_model_promotion_status: str


def _boundary(capability_identifier: str) -> ModelReleaseBoundary:
    return ModelReleaseBoundary(
        capability_identifier=capability_identifier,
        legal_gate_reference=LEGAL_GATE_REFERENCE,
        allowed_model_licenses=ALLOWED_MODEL_LICENSES,
        required_release_metadata=REQUIRED_RELEASE_METADATA,
        external_model_promotion_status=EXTERNAL_MODEL_PROMOTION_STATUS,
    )


# The registry may describe a recommended family, but none has been approved.
MODEL_RELEASE_BOUNDARIES: tuple[ModelReleaseBoundary, ...] = tuple(
    _boundary(entry.identifier) for entry in MODEL_REGISTRY
)


def validate_model_release_boundaries(
    boundaries: tuple[ModelReleaseBoundary, ...] = MODEL_RELEASE_BOUNDARIES,
) -> None:
    """Fail closed if a release prerequisite becomes incomplete or permissive."""

    if len(boundaries) != CAPABILITY_COUNT:
        raise ValueError("release boundaries must cover every registry capability")
    registry_identifiers = {entry.identifier for entry in MODEL_REGISTRY}
    boundary_identifiers = [boundary.capability_identifier for boundary in boundaries]
    if set(boundary_identifiers) != registry_identifiers or len(set(boundary_identifiers)) != len(boundaries):
        raise ValueError("release boundaries must map one-to-one with the model registry")
    for boundary in boundaries:
        if boundary.legal_gate_reference != LEGAL_GATE_REFERENCE:
            raise ValueError("release boundaries must retain the ADR-001 legal gate")
        if boundary.allowed_model_licenses != ALLOWED_MODEL_LICENSES:
            raise ValueError("release boundaries must preserve the Apache-2.0 allowlist")
        if boundary.required_release_metadata != REQUIRED_RELEASE_METADATA:
            raise ValueError("release boundaries must require complete provenance metadata")
        if boundary.external_model_promotion_status != EXTERNAL_MODEL_PROMOTION_STATUS:
            raise ValueError("external model promotion remains blocked pending legal approval")


validate_model_release_boundaries()
