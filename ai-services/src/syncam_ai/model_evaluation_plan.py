"""Planned-only evaluation metadata for the Phase 4 AI registry.

The records in this module describe gates that must happen before a capability
can be considered for release.  They contain no model artifact, benchmark
result, evaluation-card publication, inference implementation, or promotion
decision.
"""

from __future__ import annotations

from dataclasses import dataclass

from syncam_ai.model_registry import CAPABILITY_COUNT, MODEL_REGISTRY


CANONICAL_EVALUATION_PLAN_REFERENCE = "AI-ARCHITECTURE.md#7-training-and-data-strategy"
PLANNED_EVALUATION_STATUS = "planned"
REQUIRED_RELEASE_GATES = (
    "licensed_held_out_evaluation",
    "safety_review",
    "human_oversight_gate",
)


@dataclass(frozen=True)
class ModelEvaluationPlan:
    """A non-executable evaluation plan for one registry capability."""

    capability_identifier: str
    evaluation_reference: str
    required_evaluation_categories: tuple[str, ...]
    required_release_gates: tuple[str, ...]
    status: str


def _plan(capability_identifier: str, *categories: str) -> ModelEvaluationPlan:
    return ModelEvaluationPlan(
        capability_identifier=capability_identifier,
        evaluation_reference=CANONICAL_EVALUATION_PLAN_REFERENCE,
        required_evaluation_categories=categories,
        required_release_gates=REQUIRED_RELEASE_GATES,
        status=PLANNED_EVALUATION_STATUS,
    )


# These are evaluation requirements, never evidence that an evaluation passed.
MODEL_EVALUATION_PLANS: tuple[ModelEvaluationPlan, ...] = (
    _plan("person_detection", "per_vertical_detection", "hardware_latency"),
    _plan("object_tracking", "sequence_tracking", "hardware_latency"),
    _plan("weapon_detection", "small_object_detection", "hard_negative_confusion"),
    _plan("vehicle_detection", "per_vertical_detection", "weather_and_night"),
    _plan("vehicle_tracking", "sequence_tracking", "camera_handoff"),
    _plan("face_detection", "entry_camera_detection", "privacy_review"),
    _plan("face_recognition", "identity_accuracy", "privacy_and_bias_review"),
    _plan("face_verification", "verification_accuracy", "privacy_and_bias_review"),
    _plan("face_liveness", "spoof_resistance", "privacy_and_bias_review"),
    _plan("license_plate_detection", "regional_detection", "weather_and_night"),
    _plan("plate_ocr", "regional_ocr", "weather_and_night"),
    _plan("fire_detection", "temporal_confirmation", "hard_negative_confusion"),
    _plan("smoke_detection", "temporal_confirmation", "hard_negative_confusion"),
    _plan("ppe_detection", "per_class_detection", "attachment_logic"),
    _plan("helmet_detection", "per_class_detection", "attachment_logic"),
    _plan("vest_detection", "per_class_detection", "attachment_logic"),
    _plan("fall_detection", "temporal_confirmation", "site_posture_calibration"),
    _plan("fight_detection", "temporal_confirmation", "hard_negative_confusion"),
    _plan("crowd_detection", "per_camera_calibration", "density_threshold"),
    _plan("zone_intrusion", "geometry_regression", "tracker_boundary_cases"),
    _plan("abandoned_object", "temporal_confirmation", "owner_association"),
    _plan("loitering_detection", "temporal_confirmation", "tracker_identity_stability"),
    _plan("anomaly_detection", "vocabulary_precision", "human_review_workflow"),
    _plan("camera_health", "camera_fault_scenarios", "hardware_telemetry"),
)


def validate_model_evaluation_plans(
    plans: tuple[ModelEvaluationPlan, ...] = MODEL_EVALUATION_PLANS,
) -> None:
    """Reject incomplete, non-canonical, or prematurely published plan metadata."""

    if len(plans) != CAPABILITY_COUNT:
        raise ValueError("evaluation plans must cover every registry capability")
    registry_identifiers = {entry.identifier for entry in MODEL_REGISTRY}
    plan_identifiers = [plan.capability_identifier for plan in plans]
    if set(plan_identifiers) != registry_identifiers or len(set(plan_identifiers)) != len(plans):
        raise ValueError("evaluation plans must map one-to-one with the model registry")
    for plan in plans:
        if plan.evaluation_reference != CANONICAL_EVALUATION_PLAN_REFERENCE:
            raise ValueError("evaluation plans must retain the canonical reference")
        if plan.status != PLANNED_EVALUATION_STATUS:
            raise ValueError("evaluation plans cannot claim a completed evaluation")
        if not plan.required_evaluation_categories:
            raise ValueError("evaluation plans require at least one planned category")
        if plan.required_release_gates != REQUIRED_RELEASE_GATES:
            raise ValueError("evaluation plans must preserve every release gate")


validate_model_evaluation_plans()
