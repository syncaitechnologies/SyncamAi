export type Severity = "Critical" | "High" | "Medium";
export type AlertStatus =
  | "unacknowledged"
  | "acknowledged"
  | "dispatched"
  | "arrived"
  | "resolved"
  | "dismissed"
  | "snoozed";

export type AlertItem = {
  id: string;
  title: string;
  summary: string;
  severity: Severity;
  status: AlertStatus;
  type: string;
  site: string;
  siteId?: string;
  zone: string;
  camera: string;
  cameraId?: string;
  occurredAt: string;
  confidence: number;
  model: string;
  threshold: number;
  eventId: string;
  color: string;
  evidence: string;
  placeholder?: boolean;
};

export type ApiAlert = {
  id: string;
  tenant_id: string;
  event_id: string;
  site_id: string;
  camera_id: string;
  zone_id: string;
  event_type: string;
  severity: "critical" | "high" | "medium" | "low" | "info";
  status: AlertStatus;
  confidence: number;
  occurred_at: string;
  created_at: string;
  acked_at?: string;
  acked_by?: string;
};

export type RealtimeEnvelope = {
  v: 1;
  type: "event" | "snapshot" | "gap" | "pong";
  topic?: "alerts.*" | "alerts.created" | "alerts.state";
  seq: number;
  ts: string;
  payload?: unknown;
};

const severities = new Set(["critical", "high", "medium", "low", "info"]);
const statuses = new Set([
  "unacknowledged",
  "acknowledged",
  "dispatched",
  "arrived",
  "resolved",
  "dismissed",
  "snoozed",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function isApiAlert(value: unknown): value is ApiAlert {
  if (!isRecord(value)) return false;
  return (
    isString(value.id) &&
    isString(value.tenant_id) &&
    isString(value.event_id) &&
    isString(value.site_id) &&
    isString(value.camera_id) &&
    isString(value.zone_id) &&
    isString(value.event_type) &&
    typeof value.severity === "string" &&
    severities.has(value.severity) &&
    typeof value.status === "string" &&
    statuses.has(value.status) &&
    typeof value.confidence === "number" &&
    value.confidence >= 0 &&
    value.confidence <= 1 &&
    isString(value.occurred_at) &&
    isString(value.created_at)
  );
}

export function parseAlertListEnvelope(value: unknown): ApiAlert[] {
  if (
    !isRecord(value) ||
    !Array.isArray(value.data) ||
    !value.data.every(isApiAlert)
  ) {
    throw new Error("Alert list response did not match the v1 contract.");
  }
  return value.data;
}

export function parseAlertEnvelope(value: unknown): ApiAlert {
  if (!isRecord(value) || !isApiAlert(value.data)) {
    throw new Error("Alert response did not match the v1 contract.");
  }
  return value.data;
}

export function parseTicketEnvelope(value: unknown): {
  ticket: string;
  expiresAt: string;
  protocol: "syncam.realtime.v1";
} {
  if (!isRecord(value) || !isRecord(value.data))
    throw new Error("Realtime ticket response did not match the v1 contract.");
  const { ticket, expires_at: expiresAt, protocol } = value.data;
  if (
    typeof ticket !== "string" ||
    ticket.length !== 43 ||
    !isString(expiresAt) ||
    protocol !== "syncam.realtime.v1"
  ) {
    throw new Error("Realtime ticket response did not match the v1 contract.");
  }
  return { ticket, expiresAt, protocol };
}

export function parseRealtimeEnvelope(value: unknown): RealtimeEnvelope {
  if (
    !isRecord(value) ||
    value.v !== 1 ||
    !["event", "snapshot", "gap", "pong"].includes(String(value.type)) ||
    !Number.isInteger(value.seq) ||
    Number(value.seq) < 0 ||
    !isString(value.ts)
  ) {
    throw new Error("Realtime message did not match the v1 envelope contract.");
  }
  if (
    value.topic !== undefined &&
    !["alerts.*", "alerts.created", "alerts.state"].includes(
      String(value.topic),
    )
  ) {
    throw new Error("Realtime message used an unsupported topic.");
  }
  return value as RealtimeEnvelope;
}

export function alertsFromSnapshot(envelope: RealtimeEnvelope): ApiAlert[] {
  if (
    envelope.type !== "snapshot" ||
    !isRecord(envelope.payload) ||
    !Array.isArray(envelope.payload.alerts) ||
    !envelope.payload.alerts.every(isApiAlert)
  ) {
    throw new Error("Realtime snapshot did not contain a valid alert list.");
  }
  return envelope.payload.alerts;
}

export function alertFromEvent(envelope: RealtimeEnvelope): ApiAlert | null {
  if (envelope.type !== "event" || !isRecord(envelope.payload)) return null;
  return isApiAlert(envelope.payload.alert) ? envelope.payload.alert : null;
}

const titleByEventType: Record<string, string> = {
  fall: "Person down detected",
  fire: "Fire pattern detected",
  smoke: "Smoke pattern detected",
  intrusion: "Restricted zone entry",
  restricted_zone: "Restricted zone entry",
  ppe: "PPE compliance exception",
  camera_health: "Camera health degraded",
};

function labelEventType(eventType: string) {
  return eventType
    .replaceAll("_", " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function compactId(value: string) {
  return value.length > 12 ? value.slice(0, 8) : value;
}

function severityForDisplay(severity: ApiAlert["severity"]): Severity {
  if (severity === "critical") return "Critical";
  if (severity === "high") return "High";
  return "Medium";
}

export function toAlertItem(alert: ApiAlert): AlertItem {
  const type = labelEventType(alert.event_type);
  return {
    id: alert.id,
    title: titleByEventType[alert.event_type] ?? `${type} detected`,
    summary: `${type} was confirmed by the site alert pipeline and requires operator review.`,
    severity: severityForDisplay(alert.severity),
    status: alert.status,
    type,
    site: `Site ${compactId(alert.site_id)}`,
    siteId: alert.site_id,
    zone: `Zone ${compactId(alert.zone_id)}`,
    camera: `CAM ${compactId(alert.camera_id)}`,
    cameraId: alert.camera_id,
    occurredAt: alert.occurred_at,
    confidence: alert.confidence,
    model: "Reported by event contract",
    threshold: 0.8,
    eventId: alert.event_id,
    color:
      alert.severity === "critical"
        ? "#ff6b5e"
        : alert.severity === "high"
          ? "#f2a93b"
          : "#62c9a4",
    evidence: "Evidence metadata pending",
  };
}

const severityRank: Record<Severity, number> = {
  Critical: 0,
  High: 1,
  Medium: 2,
};

export function sortAlerts(alerts: AlertItem[]): AlertItem[] {
  return [...alerts].sort((left, right) => {
    const unacked =
      Number(left.status !== "unacknowledged") -
      Number(right.status !== "unacknowledged");
    if (unacked !== 0) return unacked;
    const severity = severityRank[left.severity] - severityRank[right.severity];
    return severity !== 0
      ? severity
      : Date.parse(right.occurredAt) - Date.parse(left.occurredAt);
  });
}

export function initialAlertQueue(
  mode: "demo" | "live",
  demoAlerts: AlertItem[],
): AlertItem[] {
  return mode === "demo" ? sortAlerts(demoAlerts) : [];
}

export function upsertAlert(
  alerts: AlertItem[],
  incoming: ApiAlert,
): AlertItem[] {
  const mapped = toAlertItem(incoming);
  const index = alerts.findIndex((alert) => alert.id === mapped.id);
  if (index === -1) return sortAlerts([mapped, ...alerts]);
  const next = [...alerts];
  next[index] = { ...next[index], ...mapped };
  return sortAlerts(next);
}
