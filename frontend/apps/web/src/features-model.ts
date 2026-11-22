// Features page (ADR-019): pure rules for showing each AI feature, its
// switch and its cameras. The API decides what is allowed; this only words it.

export type FeatureKey =
  | "zone_alerts" | "loitering" | "attendance" | "weapon" | "live_view" | "camera_health"
  | "fire_smoke" | "fall" | "fight" | "ppe" | "number_plates";

export type FeatureStatus = "available" | "preview" | "always_on" | "coming_soon";

/** One row of GET /v1/features. */
export type Feature = {
  key: FeatureKey;
  status: FeatureStatus;
  per_camera: boolean;
  included: boolean;
  enabled: boolean;
  on: boolean;
  /** null = every camera. */
  cameras: string[] | null;
  /** null = no site box has reported yet (or it is older than 0.1.5). */
  box_ready: boolean | null;
  activity_30d: number | null;
  updated_at: string | null;
  live_view?: { limit_minutes: number | null; used_seconds?: number };
};

export type FeatureChanges = { enabled?: boolean; cameras?: string[] | null };

export type CardState = "on" | "off" | "not_in_plan" | "needs_box" | "always_on" | "coming_soon";

export const STATE_LABELS: Record<CardState, string> = {
  on: "On",
  off: "Off",
  not_in_plan: "Not in your plan",
  needs_box: "Needs site box setup",
  always_on: "Always on",
  coming_soon: "Coming soon",
};

export type FeatureText = {
  name: string;
  summary: string;
  goodFor: string;
  icon: string;
  /** Singular and plural for the 30-day count. */
  noun: [string, string];
  /** Switching it off stops security alerts, so ask first. */
  security: boolean;
};

export const FEATURE_TEXT: Record<FeatureKey, FeatureText> = {
  zone_alerts: {
    name: "Zone alerts",
    summary: "Alerts when a person or vehicle enters an area you draw, at the times you choose.",
    goodFor: "Stock rooms, yards and shutters after hours",
    icon: "polygon", noun: ["alert", "alerts"], security: true,
  },
  loitering: {
    name: "Loitering",
    summary: "Alerts when someone stays too long in one place, including around parked vehicles.",
    goodFor: "Parking, ATMs and back doors",
    icon: "clock", noun: ["alert", "alerts"], security: true,
  },
  attendance: {
    name: "Face attendance",
    summary: "Marks staff in when they pass an entrance camera. Only staff who have agreed are recognised.",
    goodFor: "Shops, offices and warehouses",
    icon: "users", noun: ["sighting", "sightings"], security: false,
  },
  weapon: {
    name: "Possible weapon",
    summary: "Flags a possible gun or knife for a person to check. It can be wrong, so every alert needs a human review.",
    goodFor: "Entrances and cash counters",
    icon: "shield", noun: ["alert", "alerts"], security: true,
  },
  live_view: {
    name: "Live view",
    summary: "Watch any camera live from anywhere. Nothing is recorded; recordings stay on your own DVR.",
    goodFor: "Checking a site right now",
    icon: "play", noun: ["minute", "minutes"], security: false,
  },
  camera_health: {
    name: "Camera offline alerts",
    summary: "Tells you when a camera stops sending pictures, so a blind spot never goes unnoticed.",
    goodFor: "Every site",
    icon: "camera", noun: ["alert", "alerts"], security: true,
  },
  fire_smoke: {
    name: "Fire and smoke",
    summary: "Early warning when smoke or flames appear on camera.",
    goodFor: "Warehouses, kitchens and godowns",
    icon: "activity", noun: ["alert", "alerts"], security: true,
  },
  fall: {
    name: "Fall detection",
    summary: "Alerts when someone falls and does not get up.",
    goodFor: "Care homes, workshops and stairs",
    icon: "trend", noun: ["alert", "alerts"], security: true,
  },
  fight: {
    name: "Fight detection",
    summary: "Alerts on a possible fight or violent struggle.",
    goodFor: "Bars, schools and bus stands",
    icon: "users", noun: ["alert", "alerts"], security: true,
  },
  ppe: {
    name: "Safety gear (PPE)",
    summary: "Checks for helmets and safety vests in work areas.",
    goodFor: "Construction sites and factories",
    icon: "shield", noun: ["alert", "alerts"], security: false,
  },
  number_plates: {
    name: "Number plates",
    summary: "Reads vehicle number plates at gates and keeps a log.",
    goodFor: "Gated sites and parking",
    icon: "grid", noun: ["read", "reads"], security: false,
  },
};

export function featureText(key: FeatureKey): FeatureText {
  return FEATURE_TEXT[key];
}

export function cardState(feature: Feature): CardState {
  if (feature.status === "coming_soon") return "coming_soon";
  if (feature.status === "always_on") return "always_on";
  if (!feature.included) return "not_in_plan";
  if (!feature.enabled) return "off";
  if (feature.box_ready === false) return "needs_box";
  return "on";
}

/** Whether the workspace can flip this feature's switch. */
export function canSwitch(feature: Feature) {
  return (feature.status === "available" || feature.status === "preview") && feature.included;
}

/** The value line: what the feature did for the workspace lately. */
export function activityLabel(feature: Feature): string | null {
  if (feature.key === "live_view") {
    const limit = feature.live_view?.limit_minutes;
    if (limit === undefined) return null;
    const used = Math.ceil(Math.max(0, feature.live_view?.used_seconds ?? 0) / 60);
    if (limit === null) return `${used} min watched this month · unlimited`;
    if (limit === 0) return null;
    return `${Math.min(used, limit)} of ${limit} min used this month`;
  }
  const count = feature.activity_30d;
  if (count === null || count === undefined) return null;
  const [one, many] = featureText(feature.key).noun;
  if (count === 0) {
    if (!feature.on) return null;
    return feature.key === "attendance" ? "No sightings in the last 30 days" : "Nothing caught in the last 30 days, all quiet";
  }
  if (feature.key === "attendance") return `${count.toLocaleString("en-US")} ${count === 1 ? one : many} in the last 30 days`;
  return `Caught ${count.toLocaleString("en-US")} ${count === 1 ? one : many} in the last 30 days`;
}

/** "All cameras", "2 of 5 cameras" or "No cameras chosen". */
export function cameraSummary(feature: Feature, cameraIds: readonly string[]) {
  if (!feature.per_camera || feature.cameras === null) return "All cameras";
  const known = feature.cameras.filter((id) => cameraIds.includes(id)).length;
  if (known === 0) return "No cameras chosen";
  return `${known} of ${cameraIds.length} ${cameraIds.length === 1 ? "camera" : "cameras"}`;
}

/**
 * Ticks or unticks one camera. Starting from "all cameras", the list becomes
 * every camera except the unticked one. Cameras not shown here (other sites)
 * are kept. Ticking every camera turns back into "all cameras".
 */
export function toggleCamera(current: string[] | null, cameraIds: readonly string[], cameraId: string): string[] | null {
  const chosen = current === null ? [...cameraIds] : [...current];
  const index = chosen.indexOf(cameraId);
  if (index >= 0) chosen.splice(index, 1);
  else chosen.push(cameraId);
  const others = chosen.filter((id) => !cameraIds.includes(id));
  if (others.length === 0 && cameraIds.every((id) => chosen.includes(id))) return null;
  return chosen;
}

/** Asked before switching off a feature that stops security alerts. */
export function offConfirmation(feature: Feature): string | null {
  const text = featureText(feature.key);
  if (!text.security || !feature.on) return null;
  return `${text.name} will stop on ${feature.cameras === null ? "all cameras" : "the chosen cameras"}. You will not get these alerts until you switch it back on.`;
}

export function askToAddSubject(key: FeatureKey) {
  return `Add ${featureText(key).name} to my SyncCam plan`;
}

export function interestedSubject(key: FeatureKey) {
  return `I'm interested in ${featureText(key).name} for SyncCam`;
}

/** A plain message for a failed change. */
export function featureError(error: unknown): string {
  const code = typeof error === "object" && error !== null && "code" in error ? (error as { code?: unknown }).code : undefined;
  const status = typeof error === "object" && error !== null && "status" in error ? (error as { status?: unknown }).status : undefined;
  if (code === "FEATURE_NOT_INCLUDED") return "This feature is not in your plan yet. Ask us to add it.";
  if (status === 403) return "Only the workspace owner can change features. Sign in with your authenticator and try again.";
  if (error instanceof Error && error.message) return error.message;
  return "The change could not be saved. Try again.";
}

/** The page sections: what you have, what you can add, and what is coming. */
export function groupFeatures(features: readonly Feature[]) {
  return {
    yours: features.filter((feature) => feature.status !== "coming_soon" && feature.included),
    add: features.filter((feature) => feature.status !== "coming_soon" && !feature.included),
    soon: features.filter((feature) => feature.status === "coming_soon"),
  };
}

/** "4 of 5 features on" for the page heading. */
export function headline(features: readonly Feature[]) {
  const switchable = features.filter((feature) => feature.status !== "coming_soon" && feature.included);
  const on = switchable.filter((feature) => feature.on).length;
  return `${on} of ${switchable.length} ${switchable.length === 1 ? "feature" : "features"} on`;
}

/** Applies a change locally so the switch moves at once; the server answer replaces it. */
export function optimistic(feature: Feature, changes: FeatureChanges): Feature {
  const enabled = changes.enabled ?? feature.enabled;
  const cameras = "cameras" in changes ? changes.cameras ?? null : feature.cameras;
  return { ...feature, enabled, cameras, on: feature.included && enabled };
}
