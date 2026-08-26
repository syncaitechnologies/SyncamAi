"""Planned-only dataset provenance requirements for Phase 4 evaluation.

This module describes the information a future held-out evaluation dataset must
provide before it can be used.  It contains no dataset pointer, checksum,
footage, labels, or evaluation result and performs no DVC or network I/O.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.model_registry import CAPABILITY_COUNT, MODEL_REGISTRY, validate_model_registry


CANONICAL_DATASET_PROVENANCE_REFERENCE = (
    "DEVOPS-MLOPS-SyncCam-AI.md#64-dataset-versioning-od-03--dvc"
)
PLANNED_DATASET_STATUS = "planned"
HELD_OUT_EVALUATION_DATASET_USAGE = "held_out_evaluation"
DVC_VERSIONING_REQUIREMENT = "dvc_commit_and_checksum_manifest_required"
CUSTOMER_DATA_POLICY = "opt_in_only_no_production_customer_data_in_dev_or_test"
MINIMUM_LABELING_QA_SAMPLE_RATE = 0.05
REQUIRED_DATASET_PROVENANCE_METADATA = (
    "source",
    "license",
    "capture_date_range",
    "tenant_consent_class",
    "dvc_commit",
    "checksum_manifest",
    "labeling_qa_sample_rate",
)


@dataclass(frozen=True)
class DatasetProvenanceBoundary:
    """Non-executable prerequisites for one capability's future eval dataset."""

    capability_identifier: str
    provenance_reference: str
    dataset_usage: str
    versioning_requirement: str
    required_metadata: tuple[str, ...]
    minimum_labeling_qa_sample_rate: float
    customer_data_policy: str
    status: str


def _boundary(capability_identifier: str) -> DatasetProvenanceBoundary:
    return DatasetProvenanceBoundary(
        capability_identifier=capability_identifier,
        provenance_reference=CANONICAL_DATASET_PROVENANCE_REFERENCE,
        dataset_usage=HELD_OUT_EVALUATION_DATASET_USAGE,
        versioning_requirement=DVC_VERSIONING_REQUIREMENT,
        required_metadata=REQUIRED_DATASET_PROVENANCE_METADATA,
        minimum_labeling_qa_sample_rate=MINIMUM_LABELING_QA_SAMPLE_RATE,
        customer_data_policy=CUSTOMER_DATA_POLICY,
        status=PLANNED_DATASET_STATUS,
    )


# These records are requirements, not evidence that any dataset exists or passed QA.
DATASET_PROVENANCE_BOUNDARIES: tuple[DatasetProvenanceBoundary, ...] = tuple(
    _boundary(entry.identifier) for entry in MODEL_REGISTRY
)


def validate_dataset_provenance_boundaries(
    boundaries: tuple[DatasetProvenanceBoundary, ...] = DATASET_PROVENANCE_BOUNDARIES,
) -> None:
    """Fail closed if future dataset prerequisites are weakened or completed."""

    validate_model_registry()
    if len(boundaries) != CAPABILITY_COUNT:
        raise ValueError("dataset provenance boundaries must cover every capability")
    registry_identifiers = {entry.identifier for entry in MODEL_REGISTRY}
    boundary_identifiers = [boundary.capability_identifier for boundary in boundaries]
    if (
        set(boundary_identifiers) != registry_identifiers
        or len(set(boundary_identifiers)) != len(boundaries)
    ):
        raise ValueError("dataset provenance boundaries must map one-to-one with the model registry")
    for boundary in boundaries:
        if boundary.provenance_reference != CANONICAL_DATASET_PROVENANCE_REFERENCE:
            raise ValueError("dataset provenance boundaries must retain the canonical reference")
        if boundary.dataset_usage != HELD_OUT_EVALUATION_DATASET_USAGE:
            raise ValueError("dataset provenance boundaries require held-out evaluation data")
        if boundary.versioning_requirement != DVC_VERSIONING_REQUIREMENT:
            raise ValueError("dataset provenance boundaries must require DVC and checksums")
        if boundary.required_metadata != REQUIRED_DATASET_PROVENANCE_METADATA:
            raise ValueError("dataset provenance boundaries must require complete provenance metadata")
        if boundary.minimum_labeling_qa_sample_rate < MINIMUM_LABELING_QA_SAMPLE_RATE:
            raise ValueError("dataset provenance boundaries must retain the minimum labeling QA sample")
        if boundary.customer_data_policy != CUSTOMER_DATA_POLICY:
            raise ValueError("dataset provenance boundaries must preserve the customer-data policy")
        if boundary.status != PLANNED_DATASET_STATUS:
            raise ValueError("dataset provenance boundaries cannot claim a dataset is ready")


validate_dataset_provenance_boundaries()
