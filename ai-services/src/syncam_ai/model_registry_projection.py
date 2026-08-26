"""Fail-closed planned-only projection of the Phase 4 AI registry.

This module joins immutable planning metadata already present in the registry,
evaluation plan, and release boundary. It performs no I/O and neither accepts
release evidence nor loads, evaluates, activates, releases, or promotes models.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.model_evaluation_plan import (
    MODEL_EVALUATION_PLANS,
    PLANNED_EVALUATION_STATUS,
    REQUIRED_RELEASE_GATES,
    ModelEvaluationPlan,
    validate_model_evaluation_plans,
)
from syncam_ai.model_registry import (
    CAPABILITY_COUNT,
    MODEL_REGISTRY,
    ModelRegistryEntry,
    validate_model_registry,
)
from syncam_ai.model_release_boundary import (
    ALLOWED_MODEL_LICENSES,
    EXTERNAL_MODEL_PROMOTION_STATUS,
    MODEL_RELEASE_BOUNDARIES,
    REQUIRED_RELEASE_METADATA,
    ModelReleaseBoundary,
    validate_model_release_boundaries,
)

PLANNED_REGISTRY_PROJECTION_STATUS = "planned_only"


@dataclass(frozen=True)
class ModelRegistryProjection:
    """Read-only planning state for one canonical capability."""

    capability_identifier: str
    name: str
    owner: str
    hardware_tier: str
    evaluation_status: str
    required_evaluation_categories: tuple[str, ...]
    required_release_gates: tuple[str, ...]
    allowed_model_licenses: tuple[str, ...]
    required_release_metadata: tuple[str, ...]
    external_model_promotion_status: str
    status: str


def build_model_registry_projection(
    registry: tuple[ModelRegistryEntry, ...] = MODEL_REGISTRY,
    plans: tuple[ModelEvaluationPlan, ...] = MODEL_EVALUATION_PLANS,
    boundaries: tuple[ModelReleaseBoundary, ...] = MODEL_RELEASE_BOUNDARIES,
) -> tuple[ModelRegistryProjection, ...]:
    """Join canonical planning metadata and reject any permissive input."""

    validate_model_registry(registry)
    validate_model_evaluation_plans(plans)
    validate_model_release_boundaries(boundaries)

    plans_by_capability = {plan.capability_identifier: plan for plan in plans}
    boundaries_by_capability = {
        boundary.capability_identifier: boundary for boundary in boundaries
    }
    registry_identifiers = {entry.identifier for entry in registry}
    if (
        set(plans_by_capability) != registry_identifiers
        or set(boundaries_by_capability) != registry_identifiers
    ):
        raise ValueError("projection inputs must map one-to-one with the model registry")

    projection = tuple(
        ModelRegistryProjection(
            capability_identifier=entry.identifier,
            name=entry.name,
            owner=entry.owner,
            hardware_tier=entry.hardware_tier,
            evaluation_status=plans_by_capability[entry.identifier].status,
            required_evaluation_categories=plans_by_capability[
                entry.identifier
            ].required_evaluation_categories,
            required_release_gates=plans_by_capability[
                entry.identifier
            ].required_release_gates,
            allowed_model_licenses=boundaries_by_capability[
                entry.identifier
            ].allowed_model_licenses,
            required_release_metadata=boundaries_by_capability[
                entry.identifier
            ].required_release_metadata,
            external_model_promotion_status=boundaries_by_capability[
                entry.identifier
            ].external_model_promotion_status,
            status=PLANNED_REGISTRY_PROJECTION_STATUS,
        )
        for entry in registry
    )
    _validate_model_registry_projection(projection)
    return projection


def _validate_model_registry_projection(
    projection: tuple[ModelRegistryProjection, ...],
) -> None:
    if len(projection) != CAPABILITY_COUNT:
        raise ValueError("registry projection must cover every capability")
    identifiers = [entry.capability_identifier for entry in projection]
    if len(set(identifiers)) != len(identifiers):
        raise ValueError("registry projection identifiers must be unique")
    for entry in projection:
        if not all(
            (
                entry.capability_identifier,
                entry.name,
                entry.owner,
                entry.hardware_tier,
                entry.required_evaluation_categories,
            )
        ):
            raise ValueError("registry projection requires complete planning metadata")
        if entry.evaluation_status != PLANNED_EVALUATION_STATUS:
            raise ValueError("registry projection cannot claim an evaluation result")
        if entry.required_release_gates != REQUIRED_RELEASE_GATES:
            raise ValueError("registry projection must preserve every evaluation gate")
        if entry.allowed_model_licenses != ALLOWED_MODEL_LICENSES:
            raise ValueError("registry projection must preserve the license allowlist")
        if entry.required_release_metadata != REQUIRED_RELEASE_METADATA:
            raise ValueError("registry projection must preserve provenance requirements")
        if entry.external_model_promotion_status != EXTERNAL_MODEL_PROMOTION_STATUS:
            raise ValueError("registry projection must keep promotion blocked")
        if entry.status != PLANNED_REGISTRY_PROJECTION_STATUS:
            raise ValueError("registry projection must remain planned-only")


MODEL_REGISTRY_PROJECTION = build_model_registry_projection()
