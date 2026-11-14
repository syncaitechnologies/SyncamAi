import { AlertApiClient } from "./alert-client";

export type Site = { id: string; name: string; timezone: string; status: string };
export type Camera = {
  id: string;
  site_id: string;
  name: string;
  serial_number: string;
  lifecycle_status: string;
};
export type ZoneKind = "intrusion" | "restricted_zone" | "loitering" | "tripwire";
export type Schedule = {
  timezone: string;
  windows: { days: string[]; start: string; end: string }[];
};
export type Zone = {
  id: string;
  site_id: string;
  camera_id?: string;
  name: string;
  kind: ZoneKind | "abandoned";
  geometry: { type: "Polygon" | "LineString"; coordinates: unknown };
  enabled: boolean;
  loiter_seconds?: number;
  subject_classes?: string[];
  schedule?: Schedule;
  config_version: number;
};
export type CameraStatus = { online: boolean; last_frame_at?: string; error?: string };
export type SiteKey = {
  id: string;
  site_id: string;
  label: string;
  created_at: string;
  revoked_at?: string;
  last_seen_at?: string;
  camera_status: Record<string, CameraStatus>;
};
export type TelegramTarget = {
  id: string;
  site_id: string;
  chat_id: number;
  chat_title: string;
  min_severity: string;
  muted: boolean;
};

export type StaffMember = {
  id: string;
  site_id: string;
  display_name: string;
  employee_code?: string;
  consent_recorded_at: string;
  consent_recorded_by: string;
  active: boolean;
};
export type AttendanceRecord = {
  id: string;
  staff_id: string;
  staff_name?: string;
  camera_id: string;
  occurred_at: string;
  confidence: number;
};

export const vehicleClasses =["bicycle", "bus", "car", "motorcycle", "truck", "van"];

function data<T>(payload: unknown): T {
  if (typeof payload !== "object" || payload === null || !("data" in payload)) {
    throw new Error("The server response did not match the v1 contract.");
  }
  return (payload as { data: T }).data;
}

/** Live control-plane calls for workspace setup screens. */
export class SyncamApi extends AlertApiClient {
  get siteId() {
    return this.config.siteId;
  }

  async listSites(signal?: AbortSignal) {
    return data<Site[]>(await this.request("/v1/sites", { method: "GET", signal }));
  }

  async createSite(name: string, timezone: string) {
    return data<Site>(
      await this.request("/v1/sites", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ name, timezone }),
      }),
    );
  }

  async listCameras(siteId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId });
    return data<Camera[]>(await this.request(`/v1/cameras?${query}`, { method: "GET", signal }));
  }

  async createCamera(siteId: string, name: string) {
    return data<Camera>(
      await this.request("/v1/cameras", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ site_id: siteId, name, serial_number: `cam-${crypto.randomUUID().slice(0, 8)}`, tags: [] }),
      }),
    );
  }

  async listZones(siteId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId });
    return data<Zone[]>(await this.request(`/v1/zones?${query}`, { method: "GET", signal }));
  }

  async createZone(zone: {
    site_id: string;
    camera_id: string;
    name: string;
    kind: ZoneKind;
    geometry: Zone["geometry"];
    loiter_seconds?: number;
    subject_classes?: string[];
    schedule?: Schedule;
  }) {
    return data<Zone>(
      await this.request("/v1/zones", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify({ ...zone, enabled: true }),
      }),
    );
  }

  async updateZone(zone: Zone, changes: { enabled?: boolean; name?: string; schedule?: Schedule | Record<string, never> }) {
    return data<Zone>(
      await this.request(`/v1/zones/${encodeURIComponent(zone.id)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ config_version: zone.config_version, ...changes }),
      }),
    );
  }

  async listSiteKeys(siteId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId });
    return data<SiteKey[]>(await this.request(`/v1/site-keys?${query}`, { method: "GET", signal }));
  }

  async createSiteKey(siteId: string, label: string) {
    const payload = (await this.request("/v1/site-keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ site_id: siteId, label }),
    })) as { data: SiteKey; secret: { site_key: string } };
    return { key: payload.data, secret: payload.secret.site_key };
  }

  async revokeSiteKey(keyId: string) {
    await this.request(`/v1/site-keys/${encodeURIComponent(keyId)}/revoke`, { method: "POST" });
  }

  async listTelegram(siteId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId });
    const payload = (await this.request(`/v1/notifications/telegram?${query}`, { method: "GET", signal })) as {
      data: TelegramTarget[];
      meta?: { bot?: string };
    };
    return { targets: payload.data, bot: payload.meta?.bot ?? "" };
  }

  async createTelegramLink(siteId: string) {
    return data<{ code: string; expires_at: string; bot: string }>(
      await this.request("/v1/notifications/telegram/link-codes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ site_id: siteId }),
      }),
    );
  }

  async deleteTelegram(targetId: string) {
    await this.request(`/v1/notifications/telegram/${encodeURIComponent(targetId)}`, { method: "DELETE" });
  }

  async listStaff(siteId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId });
    return data<StaffMember[]>(await this.request(`/v1/staff?${query}`, { method: "GET", signal }));
  }

  async createStaff(siteId: string, displayName: string, employeeCode: string) {
    return data<StaffMember>(
      await this.request("/v1/staff", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ site_id: siteId, display_name: displayName, employee_code: employeeCode, consent_confirmed: true }),
      }),
    );
  }

  async setStaffActive(staffId: string, active: boolean) {
    return data<StaffMember>(
      await this.request(`/v1/staff/${encodeURIComponent(staffId)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ active }),
      }),
    );
  }

  async listAttendance(siteId: string, from: Date, to: Date, signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: siteId, from: from.toISOString(), to: to.toISOString() });
    return data<AttendanceRecord[]>(await this.request(`/v1/attendance?${query}`, { method: "GET", signal }));
  }

  alertSnapshot(alertId: string, signal?: AbortSignal) {
    return this.imageUrl(`/v1/alerts/${encodeURIComponent(alertId)}/snapshot`, signal);
  }

  cameraReference(cameraId: string, signal?: AbortSignal) {
    return this.imageUrl(`/v1/cameras/${encodeURIComponent(cameraId)}/reference`, signal);
  }
}
