"""Versioned synthetic/read-only catalog for the Phase 4 planning registry.

The catalog is derived only from immutable planning metadata. It has no network
transport and does not accept model artifacts or release evidence, run
inference, record evaluation results, activate, release, or promote models.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.model_evaluation_plan import PLANNED_EVALUATION_STATUS
from syncam_ai.model_registry import CAPABILITY_COUNT, MODEL_REGISTRY, ModelRegistryEntry
from syncam_ai.model_registry_projection import (
    EXTERNAL_MODEL_PROMOTION_STATUS,
    MODEL_REGISTRY_PROJECTION,
    ModelRegistryProjection,
)

MODEL_REGISTRY_CATALOG_SCHEMA_VERSION = 1
SYNTHETIC_READ_ONLY_CATALOG_MODE = "synthetic_read_only"


@dataclass(frozen=True)
class ModelRegistryCatalogEntry:
    """Safe planning metadata for a single capability."""

    identifier: str
    name: str
    family: str
    owner: str
    hardware_tier: str
    evaluation_status: str
    external_model_promotion_status: str


@dataclass(frozen=True)
class ModelRegistryCatalog:
    """A versioned local catalog, never a live model-control surface."""

    schema_version: int
    mode: str
    capabilities: tuple[ModelRegistryCatalogEntry, ...]


def build_model_registry_catalog(
    registry: tuple[ModelRegistryEntry, ...] = MODEL_REGISTRY,
    projection: tuple[ModelRegistryProjection, ...] = MODEL_REGISTRY_PROJECTION,
) -> ModelRegistryCatalog:
    """Build the catalog only when projection and registry identifiers agree."""

    registry_by_identifier = {entry.identifier: entry for entry in registry}
    projection_identifiers = [entry.capability_identifier for entry in projection]
    if (
        len(registry_by_identifier) != CAPABILITY_COUNT
        or len(projection) != CAPABILITY_COUNT
        or set(projection_identifiers) != set(registry_by_identifier)
        or len(set(projection_identifiers)) != len(projection_identifiers)
    ):
        raise ValueError("catalog inputs must cover each canonical capability once")

    catalog = ModelRegistryCatalog(
        schema_version=MODEL_REGISTRY_CATALOG_SCHEMA_VERSION,
        mode=SYNTHETIC_READ_ONLY_CATALOG_MODE,
        capabilities=tuple(
            ModelRegistryCatalogEntry(
                identifier=entry.capability_identifier,
                name=entry.name,
                family=registry_by_identifier[entry.capability_identifier].family,
                owner=entry.owner,
                hardware_tier=entry.hardware_tier,
                evaluation_status=entry.evaluation_status,
                external_model_promotion_status=entry.external_model_promotion_status,
            )
            for entry in projection
        ),
    )
    validate_model_registry_catalog(catalog)
    return catalog


def validate_model_registry_catalog(
    catalog: ModelRegistryCatalog = None,
) -> None:
    """Reject catalog data that could be represented as live or releasable."""

    if catalog is None:
        catalog = MODEL_REGISTRY_CATALOG
    if catalog.schema_version != MODEL_REGISTRY_CATALOG_SCHEMA_VERSION:
        raise ValueError("catalog schema version must remain explicit")
    if catalog.mode != SYNTHETIC_READ_ONLY_CATALOG_MODE:
        raise ValueError("catalog must remain synthetic and read-only")
    if len(catalog.capabilities) != CAPABILITY_COUNT:
        raise ValueError("catalog must cover every canonical capability")

    identifiers = [entry.identifier for entry in catalog.capabilities]
    if len(set(identifiers)) != len(identifiers):
        raise ValueError("catalog capability identifiers must be unique")
    for entry in catalog.capabilities:
        if not all((entry.identifier, entry.name, entry.family, entry.owner, entry.hardware_tier)):
            raise ValueError("catalog entries require complete planning metadata")
        if entry.evaluation_status != PLANNED_EVALUATION_STATUS:
            raise ValueError("catalog cannot claim an evaluation result")
        if entry.external_model_promotion_status != EXTERNAL_MODEL_PROMOTION_STATUS:
            raise ValueError("catalog must keep external promotion blocked")


MODEL_REGISTRY_CATALOG = build_model_registry_catalog()
