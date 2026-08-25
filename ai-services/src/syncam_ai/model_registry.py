"""Metadata-only Phase 4 AI module registry.

This registry records the canonical architecture choices.  It does not load a
model, run inference, publish an evaluation card, or authorize promotion.
"""

from __future__ import annotations

from dataclasses import dataclass


CANONICAL_EVALUATION_REFERENCE = "AI-ARCHITECTURE.md#2-module-registry-master-table"
PLANNED_EVALUATION_STATUS = "planned"
MODULE_COUNT = 23
CAPABILITY_COUNT = 24


@dataclass(frozen=True)
class ModelRegistryEntry:
    """A canonical module capability with no executable model artifact."""

    identifier: str
    name: str
    family: str
    recommended_model: str
    owner: str
    hardware_tier: str
    evaluation_reference: str
    evaluation_status: str
    bonus_capability: bool = False


def _entry(
    identifier: str,
    name: str,
    family: str,
    recommended_model: str,
    owner: str,
    hardware_tier: str,
    *,
    bonus_capability: bool = False,
) -> ModelRegistryEntry:
    return ModelRegistryEntry(
        identifier=identifier,
        name=name,
        family=family,
        recommended_model=recommended_model,
        owner=owner,
        hardware_tier=hardware_tier,
        evaluation_reference=CANONICAL_EVALUATION_REFERENCE,
        evaluation_status=PLANNED_EVALUATION_STATUS,
        bonus_capability=bonus_capability,
    )


# AI-ARCHITECTURE §2 is canonical: 23 modules plus the Camera Health bonus.
MODEL_REGISTRY: tuple[ModelRegistryEntry, ...] = (
    _entry("person_detection", "Person Detection", "Detector", "YOLOv8m / YOLO11m (shared backbone)", "AI", "Orin NX -> T4"),
    _entry("object_tracking", "Object Tracking", "Tracker", "ByteTrack (IoU+Kalman)", "AI and Edge", "Orin NX -> CPU"),
    _entry("weapon_detection", "Weapon Detection", "Detector (small-object)", "YOLOv8m FT, P2 head + SAHI", "AI", "Orin NX -> T4/L4"),
    _entry("vehicle_detection", "Vehicle Detection", "Detector", "Shared backbone FT (BDD100K)", "AI", "Orin NX -> T4"),
    _entry("vehicle_tracking", "Vehicle Tracking", "Tracker+ReID", "ByteTrack + Vehicle-ReID", "AI", "Orin NX -> T4"),
    _entry("face_detection", "Face Detection", "Face", "SCRFD-500 (InsightFace)", "AI", "Orin NX -> T4"),
    _entry("face_recognition", "Face Recognition", "Face", "ArcFace MobileFaceNet / R100", "AI", "Orin NX -> T4"),
    _entry("face_verification", "Face Verification", "Face", "ArcFace 1:1 cosine", "AI", "Orin NX -> T4"),
    _entry("face_liveness", "Face Liveness", "Face", "miniFASNet-class texture CNN", "AI", "Orin NX -> T4"),
    _entry("license_plate_detection", "License Plate Detection", "LPR", "YOLOv8s-FT or WPOD-NET", "AI", "Orin NX -> T4"),
    _entry("plate_ocr", "Plate OCR", "LPR", "LPRNet / PP-OCRv4-mobile", "AI", "Orin NX -> T4"),
    _entry("fire_detection", "Fire Detection", "Classifier", "EfficientNet-B0 FT", "AI", "Orin NX -> T4"),
    _entry("smoke_detection", "Smoke Detection", "Classifier", "D-Fire FT", "AI", "Orin NX -> T4"),
    _entry("ppe_detection", "PPE Detection", "Detector", "Shared backbone FT", "AI", "Orin NX -> T4"),
    _entry("helmet_detection", "Helmet Detection", "Detector", "PPE model helmet class", "AI", "Orin NX -> T4"),
    _entry("vest_detection", "Vest Detection", "Detector", "PPE model vest class", "AI", "Orin NX -> T4"),
    _entry("fall_detection", "Fall Detection", "Pose+logic", "RTMPose-m / YOLOv8-pose-m", "AI", "Orin NX -> T4"),
    _entry("fight_detection", "Fight Detection", "Pose+logic", "Person tracks + pose/velocity scoring", "AI", "Orin NX -> T4"),
    _entry("crowd_detection", "Crowd Detection", "Density", "Person count + perspective calibration", "AI", "Orin NX -> T4"),
    _entry("zone_intrusion", "Zone Intrusion", "Logic", "Point-in-polygon + line crossing", "AI and Edge", "CPU"),
    _entry("abandoned_object", "Abandoned Object", "Logic+classifier", "Static-region detector + matching", "AI and Edge", "Orin NX -> T4"),
    _entry("loitering_detection", "Loitering Detection", "Logic", "Dwell-time FSM over tracks", "AI and Edge", "CPU"),
    _entry("anomaly_detection", "Anomaly Detection", "Open-vocab ML (v2)", "YOLO-World-s / GroundingDINO-T", "AI", "Orin NX -> T4/L4"),
    _entry("camera_health", "Camera Health", "Classifier+telemetry", "Blur/tamper/occlusion CNN + decode/FPS stats", "AI and Edge", "Orin NX -> CPU", bonus_capability=True),
)


def validate_model_registry(entries: tuple[ModelRegistryEntry, ...] = MODEL_REGISTRY) -> None:
    """Reject malformed registry metadata before any later promotion workflow."""

    if len(entries) != CAPABILITY_COUNT:
        raise ValueError("model registry must contain 23 modules plus camera health")
    identifiers = [entry.identifier for entry in entries]
    if len(set(identifiers)) != len(identifiers):
        raise ValueError("model registry identifiers must be unique")
    bonuses = [entry for entry in entries if entry.bonus_capability]
    if len(bonuses) != 1 or bonuses[0].identifier != "camera_health":
        raise ValueError("camera health must be the only bonus capability")
    for entry in entries:
        if not all((entry.identifier, entry.name, entry.family, entry.recommended_model, entry.owner, entry.hardware_tier)):
            raise ValueError("model registry entries require complete metadata")
        if entry.evaluation_reference != CANONICAL_EVALUATION_REFERENCE:
            raise ValueError("model registry evaluation references must remain canonical")
        if entry.evaluation_status != PLANNED_EVALUATION_STATUS:
            raise ValueError("model registry cannot claim an unpublished evaluation")


validate_model_registry()

