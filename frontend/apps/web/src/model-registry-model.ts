export type RegistryReleaseStatus = "blocked" | "planned";

export interface RegistryCapability {
  id: string;
  name: string;
  family: string;
  owner: string;
  status: RegistryReleaseStatus;
}

export const registryCapabilities: RegistryCapability[] = [
  ["person_detection", "Person detection", "Detector", "AI"],
  ["object_tracking", "Object tracking", "Tracker", "AI and Edge"],
  ["weapon_detection", "Weapon detection", "Detector", "AI"],
  ["vehicle_detection", "Vehicle detection", "Detector", "AI"],
  ["vehicle_tracking", "Vehicle tracking", "Tracker + ReID", "AI"],
  ["face_detection", "Face detection", "Face", "AI"],
  ["face_recognition", "Face recognition", "Face", "AI"],
  ["face_verification", "Face verification", "Face", "AI"],
  ["face_liveness", "Face liveness", "Face", "AI"],
  ["license_plate_detection", "License plate detection", "LPR", "AI"],
  ["plate_ocr", "Plate OCR", "LPR", "AI"],
  ["fire_detection", "Fire detection", "Classifier", "AI"],
  ["smoke_detection", "Smoke detection", "Classifier", "AI"],
  ["ppe_detection", "PPE detection", "Detector", "AI"],
  ["helmet_detection", "Helmet detection", "Detector", "AI"],
  ["vest_detection", "Vest detection", "Detector", "AI"],
  ["fall_detection", "Fall detection", "Pose + logic", "AI"],
  ["fight_detection", "Fight detection", "Pose + logic", "AI"],
  ["crowd_detection", "Crowd detection", "Density", "AI"],
  ["zone_intrusion", "Zone intrusion", "Logic", "AI and Edge"],
  ["abandoned_object", "Abandoned object", "Logic + classifier", "AI and Edge"],
  ["loitering_detection", "Loitering detection", "Logic", "AI and Edge"],
  ["anomaly_detection", "Anomaly detection", "Open-vocabulary ML", "AI"],
  ["camera_health", "Camera health", "Classifier + telemetry", "AI and Edge"],
].map(([id, name, family, owner]) => ({
  id: id!,
  name: name!,
  family: family!,
  owner: owner!,
  status: "blocked" as const,
}));

export function filterRegistryCapabilities(
  capabilities: RegistryCapability[],
  query: string,
): RegistryCapability[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return capabilities;
  return capabilities.filter((capability) =>
    [capability.name, capability.family, capability.owner]
      .join(" ")
      .toLowerCase()
      .includes(normalized),
  );
}
