"""Fail-closed validation for caller-supplied Phase 4 dataset metadata.

This module validates the shape and minimum policy requirements of one future
held-out evaluation manifest. It neither loads a manifest nor accesses DVC,
object storage, footage, labels, consent records, or approval systems.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.dataset_provenance_boundary import (
    CUSTOMER_DATA_POLICY,
    DATASET_PROVENANCE_BOUNDARIES,
    HELD_OUT_EVALUATION_DATASET_USAGE,
    MINIMUM_LABELING_QA_SAMPLE_RATE,
    REQUIRED_DATASET_PROVENANCE_METADATA,
    validate_dataset_provenance_boundaries,
)


DATASET_MANIFEST_VALIDATION_REFERENCE = "docs/development/phase-4-dataset-manifest-validator.md"


@dataclass(frozen=True)
class DatasetManifestRecord:
    """Metadata supplied by a future governed held-out dataset manifest."""

    capability_identifier: str
    source: str
    license: str
    capture_date_range: str
    tenant_consent_class: str
    dvc_commit: str
    checksum_manifest: str
    labeling_qa_sample_rate: float
    contains_customer_footage: bool
    customer_footage_opt_in_reference: str | None
    is_development_or_test_dataset: bool
    contains_production_customer_data: bool


def validate_dataset_manifest_record(record: DatasetManifestRecord) -> None:
    """Reject incomplete or policy-incompatible future dataset metadata.

    A successful validation only establishes that supplied metadata satisfies
    this boundary. It does not verify that the metadata, any approval, or any
    referenced dataset is real or authorized.
    """

    validate_dataset_provenance_boundaries()
    boundary_by_identifier = {
        boundary.capability_identifier: boundary
        for boundary in DATASET_PROVENANCE_BOUNDARIES
    }
    boundary = boundary_by_identifier.get(record.capability_identifier)
    if boundary is None:
        raise ValueError("dataset manifest must identify a canonical capability")
    if boundary.dataset_usage != HELD_OUT_EVALUATION_DATASET_USAGE:
        raise ValueError("dataset manifest must be for held-out evaluation")
    if boundary.required_metadata != REQUIRED_DATASET_PROVENANCE_METADATA:
        raise ValueError("dataset manifest boundary must retain complete provenance metadata")
    if boundary.customer_data_policy != CUSTOMER_DATA_POLICY:
        raise ValueError("dataset manifest boundary must retain the customer-data policy")

    required_values = {
        "source": record.source,
        "license": record.license,
        "capture_date_range": record.capture_date_range,
        "tenant_consent_class": record.tenant_consent_class,
        "dvc_commit": record.dvc_commit,
        "checksum_manifest": record.checksum_manifest,
    }
    missing_values = [name for name, value in required_values.items() if not value.strip()]
    if missing_values:
        raise ValueError(f"dataset manifest is missing required metadata: {', '.join(missing_values)}")
    if not MINIMUM_LABELING_QA_SAMPLE_RATE <= record.labeling_qa_sample_rate <= 1:
        raise ValueError("dataset manifest must retain a labeling QA rate from 5% through 100%")
    if record.contains_customer_footage:
        if not record.customer_footage_opt_in_reference or not record.customer_footage_opt_in_reference.strip():
            raise ValueError("customer footage requires a documented opt-in reference")
    elif record.customer_footage_opt_in_reference is not None and not record.customer_footage_opt_in_reference.strip():
        raise ValueError("customer footage opt-in reference cannot be blank")
    if record.is_development_or_test_dataset and record.contains_production_customer_data:
        raise ValueError("development or test datasets cannot contain production customer data")
