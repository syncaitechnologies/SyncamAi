import { vehicleClasses } from "./subject-classes.ts";
import type { Schedule, Zone, ZoneKind } from "./syncam-api.ts";

/**
 * One-click protection for a whole camera view. Each template is an ordinary
 * zone (it can be paused or changed later like any other), created with the
 * existing audited zones API.
 */
export type TemplateId = "night-intrusion" | "car-park";

export type ProtectionTemplate = {
  id: TemplateId;
  label: string;
  /** Zone name; also how an existing template zone is recognised. */
  zoneName: string;
  help: string;
  kind: ZoneKind;
  classes: string[];
  loiterSeconds?: number;
  window: { start: string; end: string };
};

export const allDays = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

export const protectionTemplates: readonly ProtectionTemplate[] = [
  {
    id: "night-intrusion",
    label: "Night intrusion",
    zoneName: "Night intrusion (whole view)",
    help: "Alert when a person is seen anywhere in view from 22:00 to 06:00, every day.",
    kind: "intrusion",
    classes: ["person"],
    window: { start: "22:00", end: "06:00" },
  },
  {
    id: "car-park",
    label: "Car park watch",
    zoneName: "Car park watch (whole view)",
    help: "Alert when a person stays near parked vehicles for 90 seconds, from 20:00 to 07:00 every day. Parked vehicles never alert.",
    kind: "loitering",
    classes: ["person", ...vehicleClasses],
    loiterSeconds: 90,
    window: { start: "20:00", end: "07:00" },
  },
];

export function templateById(id: string) {
  return protectionTemplates.find((template) => template.id === id);
}

/** The whole picture, kept just inside the edges. */
export function wholeView(): Zone["geometry"] {
  return { type: "Polygon", coordinates: [[[0.02, 0.02], [0.98, 0.02], [0.98, 0.98], [0.02, 0.98], [0.02, 0.02]]] };
}

export function templateZone(template: ProtectionTemplate, target: { siteId: string; cameraId: string; timezone: string }) {
  const schedule: Schedule = { timezone: target.timezone, windows: [{ days: allDays, ...template.window }] };
  return {
    site_id: target.siteId,
    camera_id: target.cameraId,
    name: template.zoneName,
    kind: template.kind,
    geometry: wholeView(),
    subject_classes: template.classes,
    ...(template.loiterSeconds ? { loiter_seconds: template.loiterSeconds } : {}),
    schedule,
  };
}

export function hasTemplate(zones: readonly Zone[], cameraId: string, template: ProtectionTemplate) {
  return zones.some((zone) => zone.camera_id === cameraId && zone.name === template.zoneName);
}

/** What the site box saw in a channel's picture decides the default protection. */
export function suggestTemplate(scene: { vehicles?: number }): TemplateId {
  return (scene.vehicles ?? 0) > 0 ? "car-park" : "night-intrusion";
}
