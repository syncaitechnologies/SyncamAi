import type { AlertItem, Severity } from "./alert-contracts";
import type { ApiCamera } from "./camera-contracts";

export type CameraStatus =
  | "online"
  | "recording"
  | "offline"
  | "pending"
  | "degraded"
  | "masked";

export type CameraWallFilter =
  | "all"
  | "active"
  | "online"
  | "offline"
  | "pending"
  | "degraded"
  | "masked";

export type CameraWallLayout = 1 | 4 | 9 | 16 | 25;

export interface CameraTile {
  id: string;
  name: string;
  location: string;
  group: string;
  status: CameraStatus;
  activity: number[];
  alert: AlertItem | null;
  synthetic: boolean;
  tone: number;
}

interface CameraSeed extends Omit<CameraTile, "alert" | "synthetic"> {}

const cameraSeeds: CameraSeed[] = [
  {
    id: "CAM-07",
    name: "Dock west",
    location: "West loading bay",
    group: "Logistics",
    status: "online",
    activity: [18, 28, 22, 44, 63, 48, 72, 58],
    tone: 1,
  },
  {
    id: "CAM-12",
    name: "Aisle 4",
    location: "Solvent storage",
    group: "Restricted areas",
    status: "recording",
    activity: [16, 22, 45, 68, 78, 71, 54, 64],
    tone: 2,
  },
  {
    id: "CAM-03",
    name: "Fabrication",
    location: "Welding bay",
    group: "Production",
    status: "online",
    activity: [42, 38, 51, 47, 65, 59, 72, 68],
    tone: 3,
  },
  {
    id: "CAM-02",
    name: "East gate",
    location: "East entrance",
    group: "Perimeter",
    status: "degraded",
    activity: [62, 48, 39, 33, 28, 20, 14, 12],
    tone: 4,
  },
  {
    id: "CAM-18",
    name: "Charging room",
    location: "Battery charging",
    group: "Life safety",
    status: "recording",
    activity: [12, 18, 22, 46, 81, 74, 58, 49],
    tone: 5,
  },
  {
    id: "CAM-11",
    name: "Yard north",
    location: "North perimeter",
    group: "Perimeter",
    status: "offline",
    activity: [31, 26, 18, 12, 8, 0, 0, 0],
    tone: 6,
  },
  {
    id: "CAM-05",
    name: "Main lobby",
    location: "Visitor entrance",
    group: "Public areas",
    status: "online",
    activity: [14, 18, 20, 31, 26, 34, 29, 38],
    tone: 7,
  },
  {
    id: "CAM-09",
    name: "South corridor",
    location: "Privacy zone",
    group: "Administration",
    status: "masked",
    activity: [0, 0, 0, 0, 0, 0, 0, 0],
    tone: 8,
  },
  {
    id: "CAM-16",
    name: "Packing line",
    location: "Dispatch staging",
    group: "Logistics",
    status: "online",
    activity: [34, 42, 55, 49, 61, 70, 64, 77],
    tone: 9,
  },
];

const inactiveStatuses = new Set(["resolved", "dismissed", "snoozed"]);
const severityOrder: Record<Severity, number> = {
  Critical: 0,
  High: 1,
  Medium: 2,
};
const statusOrder: Record<CameraStatus, number> = {
  recording: 0,
  degraded: 1,
  offline: 2,
  pending: 3,
  online: 4,
  masked: 5,
};

function cameraId(camera: string) {
  return camera.split(/[·\s]/).find((part) => part.startsWith("CAM-")) ?? camera;
}

function activeAlertForCamera(alerts: AlertItem[], id: string) {
  return alerts
    .filter(
      (alert) =>
        !alert.placeholder &&
        !inactiveStatuses.has(alert.status) &&
        (alert.cameraId === id || cameraId(alert.camera) === id),
    )
    .sort((left, right) => {
      const severityDifference =
        severityOrder[left.severity] - severityOrder[right.severity];
      return (
        severityDifference ||
        new Date(right.occurredAt).getTime() -
          new Date(left.occurredAt).getTime()
      );
    })[0] ?? null;
}

function toneForCamera(id: string) {
  let hash = 0;
  for (const character of id) hash = (hash * 31 + character.charCodeAt(0)) | 0;
  return Math.abs(hash % 9) + 1;
}

export function buildLiveCameraWallTiles(
  cameras: ApiCamera[],
  alerts: AlertItem[],
): CameraTile[] {
  return cameras
    .filter((camera) => camera.lifecycle_status !== "retired")
    .map((camera) => {
      const alert = activeAlertForCamera(alerts, camera.id);
      const status: CameraStatus =
        camera.lifecycle_status === "offline"
          ? "offline"
          : camera.lifecycle_status === "pending"
            ? "pending"
            : "online";
      return {
        id: camera.id,
        name: camera.name,
        location: camera.tags.length
          ? camera.tags.join(" · ")
          : "No location tags",
        group: camera.group_name || "Ungrouped",
        status,
        activity: [0, 0, 0, 0, 0, 0, 0, 0],
        alert,
        synthetic: false,
        tone: toneForCamera(camera.id),
      };
    });
}

export function buildCameraWallTiles(
  alerts: AlertItem[],
  dataMode: "demo" | "live",
): CameraTile[] {
  if (dataMode === "live") return [];

  return cameraSeeds.map((camera) => {
    const alert = activeAlertForCamera(alerts, camera.id);
    return {
      ...camera,
      status:
        alert && camera.status === "online" ? "recording" : camera.status,
      alert,
      synthetic: true,
    };
  });
}

export function filterCameraWallTiles(
  tiles: CameraTile[],
  query: string,
  filter: CameraWallFilter,
) {
  const normalizedQuery = query.trim().toLowerCase();

  return tiles
    .filter((tile) => {
      if (filter === "active" && !tile.alert) return false;
      if (
        filter === "online" &&
        tile.status !== "online" &&
        tile.status !== "recording"
      ) {
        return false;
      }
      if (
        !["all", "active", "online"].includes(filter) &&
        tile.status !== filter
      ) {
        return false;
      }
      if (!normalizedQuery) return true;

      return [
        tile.id,
        tile.name,
        tile.location,
        tile.group,
        tile.status,
        tile.alert?.title,
        tile.alert?.type,
      ].some((value) => value?.toLowerCase().includes(normalizedQuery));
    })
    .sort((left, right) => {
      if (left.alert && right.alert) {
        const severityDifference =
          severityOrder[left.alert.severity] - severityOrder[right.alert.severity];
        if (severityDifference) return severityDifference;
      }
      if (Boolean(left.alert) !== Boolean(right.alert)) return left.alert ? -1 : 1;
      return (
        statusOrder[left.status] - statusOrder[right.status] ||
        left.id.localeCompare(right.id)
      );
    });
}

export function groupCameraWallAlerts(alerts: AlertItem[]) {
  const grouped = new Map<string, AlertItem>();

  for (const alert of alerts) {
    if (alert.placeholder || inactiveStatuses.has(alert.status)) continue;
    const key = `${cameraId(alert.camera)}:${alert.type}`;
    const current = grouped.get(key);
    if (
      !current ||
      severityOrder[alert.severity] < severityOrder[current.severity] ||
      (alert.severity === current.severity &&
        new Date(alert.occurredAt).getTime() >
          new Date(current.occurredAt).getTime())
    ) {
      grouped.set(key, alert);
    }
  }

  return [...grouped.values()].sort((left, right) => {
    const severityDifference =
      severityOrder[left.severity] - severityOrder[right.severity];
    return (
      severityDifference ||
      new Date(right.occurredAt).getTime() -
        new Date(left.occurredAt).getTime()
    );
  });
}

export function wallColumnCount(layout: CameraWallLayout) {
  if (layout === 1) return 1;
  if (layout === 4) return 2;
  if (layout === 9) return 3;
  if (layout === 16) return 4;
  return 5;
}
