export type RegistryReleaseStatus = "blocked" | "planned";
export type RegistryCatalogMode = "synthetic_read_only";

export const REGISTRY_CATALOG_SCHEMA_VERSION = 1;
export const SYNTHETIC_READ_ONLY_CATALOG_MODE: RegistryCatalogMode =
  "synthetic_read_only";

export interface RegistryCapability {
  id: string;
  name: string;
  family: string;
  owner: string;
  status: RegistryReleaseStatus;
}

export interface RegistryCatalog {
  schemaVersion: number;
  mode: RegistryCatalogMode;
  capabilities: readonly RegistryCapability[];
}

export const modelRegistryCatalog: RegistryCatalog = {
  schemaVersion: REGISTRY_CATALOG_SCHEMA_VERSION,
  mode: SYNTHETIC_READ_ONLY_CATALOG_MODE,
  capabilities: [
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
  })),
};

export function validateRegistryCatalog(catalog: RegistryCatalog): void {
  if (catalog.schemaVersion !== REGISTRY_CATALOG_SCHEMA_VERSION) {
    throw new Error("model registry catalog schema version is unsupported");
  }
  if (catalog.mode !== SYNTHETIC_READ_ONLY_CATALOG_MODE) {
    throw new Error("model registry catalog must remain synthetic and read-only");
  }
  if (catalog.capabilities.length !== 24) {
    throw new Error("model registry catalog must contain every planned capability");
  }

  const identifiers = catalog.capabilities.map(({ id }) => id);
  if (new Set(identifiers).size !== identifiers.length) {
    throw new Error("model registry catalog capability identifiers must be unique");
  }
  if (
    catalog.capabilities.some(
      ({ id, name, family, owner, status }) =>
        !id || !name || !family || !owner || status !== "blocked",
    )
  ) {
    throw new Error("model registry catalog must contain only blocked planning metadata");
  }
}

validateRegistryCatalog(modelRegistryCatalog);

export const registryCapabilities = modelRegistryCatalog.capabilities;

export function filterRegistryCapabilities(
  capabilities: readonly RegistryCapability[],
  query: string,
): RegistryCapability[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return [...capabilities];
  return capabilities.filter((capability) =>
    [capability.name, capability.family, capability.owner]
      .join(" ")
      .toLowerCase()
      .includes(normalized),
  );
}
