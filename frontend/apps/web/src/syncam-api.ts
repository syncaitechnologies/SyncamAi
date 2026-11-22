import { AlertApiClient, AlertApiError } from "./alert-client";
import { errorCode, frameProblem, type LiveFrameProblem, type LiveViewUsage } from "./live-view-model";
import type { AdminChanges, AdminWorkspace } from "./admin-model";
import type { Feature, FeatureChanges, FeatureKey } from "./features-model";
import { RESTART_TIMEOUT_MS, restartFailure } from "./site-box-model";

export type { LiveFrameProblem, LiveViewUsage } from "./live-view-model";

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
  /** Present when the box paired with a code (PEM, P-256). */
  box_public_key?: string;
};
export type PairingCode = { code: string; site_id: string; expires_at: string };
export type SetupBox = { paired: boolean; online: boolean; key_id?: string; label?: string; public_key?: string; last_seen_at?: string };
export type SetupTaskType = "scan" | "probe" | "adopt" | "restart";
export type SetupTask = {
  id: string;
  type: SetupTaskType;
  status: "pending" | "running" | "done" | "failed";
  result?: unknown;
  error?: string;
};
export type FoundDevice = {
  device_id: string;
  ip: string;
  rtsp_port: number;
  ports: number[];
  family: string | null;
  brand: string;
  name: string;
  model: string;
  onvif: boolean;
};
/** `vehicles` and `people` are what the site box detected in the channel's picture. */
export type ProbedChannel = { channel: number; thumbnail?: string; width?: number; height?: number; vehicles?: number; people?: number };
export type ProbeResult = { device_id: string; family: string | null; brand: string; channels: ProbedChannel[]; error: string };
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

export { vehicleClasses } from "./subject-classes";

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

  /** The paired site box whose public key camera passwords are encrypted for. */
  async setupBox(siteId: string, signal?: AbortSignal) {
    return data<SetupBox>(await this.request(`/v1/sites/${encodeURIComponent(siteId)}/setup/box`, { method: "GET", signal }));
  }

  async createSetupTask(siteId: string, type: SetupTaskType, payload?: unknown) {
    return data<SetupTask>(
      await this.request(`/v1/sites/${encodeURIComponent(siteId)}/setup/tasks`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload === undefined ? { type } : { type, payload }),
      }),
    );
  }

  async setupTask(siteId: string, taskId: string, signal?: AbortSignal) {
    return data<SetupTask>(
      await this.request(`/v1/sites/${encodeURIComponent(siteId)}/setup/tasks/${encodeURIComponent(taskId)}`, { method: "GET", signal }),
    );
  }

  /** Queue a task for the site box and wait for its result. */
  async runSetupTask<T>(siteId: string, type: SetupTaskType, payload: unknown, options: { signal?: AbortSignal; timeoutMs: number; intervalMs?: number }) {
    const created = await this.createSetupTask(siteId, type, payload);
    const deadline = Date.now() + options.timeoutMs;
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, options.intervalMs ?? 1500));
      if (options.signal?.aborted) throw new DOMException("Cancelled", "AbortError");
      const task = await this.setupTask(siteId, created.id, options.signal);
      if (task.status === "done") return task.result as T;
      if (task.status === "failed") throw new Error(task.error || "The site box could not finish this step.");
      if (Date.now() > deadline) {
        throw new Error(task.status === "pending" ? "The site box did not pick this up. Check that it is online." : "The site box is taking too long. Try again.");
      }
    }
  }

  /**
   * Ask the site's box to restart its cameras and AI. Resolves once the box
   * confirms it received the request; it is back within about a minute.
   */
  async restartSiteBox(siteId: string, signal?: AbortSignal) {
    try {
      await this.runSetupTask(siteId, "restart", undefined, { signal, timeoutMs: RESTART_TIMEOUT_MS, intervalMs: 1000 });
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") throw error;
      throw new Error(restartFailure(error instanceof Error ? error.message : undefined));
    }
  }

  /** A one-time 8-character code a new site box types to pair itself. */
  async createPairingCode(siteId: string) {
    return data<PairingCode>(
      await this.request("/v1/site-keys/pairing-codes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ site_id: siteId }),
      }),
    );
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

  /** The camera's reference still (the site box refreshes it every few minutes), with its capture time when known. */
  cameraStill(cameraId: string, signal?: AbortSignal) {
    return this.image(`/v1/cameras/${encodeURIComponent(cameraId)}/reference`, signal);
  }

  /** This workspace's live view allowance for the current month. */
  /** Owner admin screen: every workspace (platform operators only). */
  async adminWorkspaces(signal?: AbortSignal) {
    return data<AdminWorkspace[]>(await this.request("/v1/admin/workspaces", { method: "GET", signal }));
  }

  /** Owner admin screen: change one workspace's plan, trial end or limits. */
  async updateAdminWorkspace(tenantId: string, changes: AdminChanges) {
    await this.request(`/v1/admin/workspaces/${encodeURIComponent(tenantId)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Correlation-Id": crypto.randomUUID() },
      body: JSON.stringify(changes),
    });
  }

  /** Owner admin screen: set which features a workspace's plan includes (ADR-019). */
  async setAdminFeatures(tenantId: string, changes: Record<string, boolean>) {
    await this.request(`/v1/admin/workspaces/${encodeURIComponent(tenantId)}/features`, {
      method: "PUT",
      headers: { "Content-Type": "application/json", "X-Correlation-Id": crypto.randomUUID() },
      body: JSON.stringify(changes),
    });
  }

  /** Every AI feature with its plan, switch, cameras and recent activity. */
  async features(signal?: AbortSignal) {
    return data<Feature[]>(await this.request("/v1/features", { method: "GET", signal }));
  }

  /** Switch one feature on or off, or choose its cameras (null = all). Workspace owners only. */
  async updateFeature(key: FeatureKey, changes: FeatureChanges) {
    return data<Feature>(
      await this.request(`/v1/features/${encodeURIComponent(key)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json", "X-Correlation-Id": crypto.randomUUID() },
        body: JSON.stringify(changes),
      }),
    );
  }

  async liveViewUsage(signal?: AbortSignal) {
    return data<LiveViewUsage>(await this.request("/v1/live-view/usage", { method: "GET", signal }));
  }

  /**
   * Starts (or keeps alive) an audited near-live viewing session. Throws an
   * AlertApiError whose `code` is "LIVE_VIEW_LIMIT" when the workspace's
   * monthly minutes are used up or live view is off.
   */
  async startLiveView(cameraId: string, signal?: AbortSignal) {
    return data<LiveViewSession>(
      await this.request(`/v1/cameras/${encodeURIComponent(cameraId)}/live`, {
        method: "POST",
        signal,
        headers: { "X-Correlation-Id": crypto.randomUUID() },
      }),
    );
  }

  async stopLiveView(cameraId: string) {
    await this.request(`/v1/cameras/${encodeURIComponent(cameraId)}/live`, { method: "DELETE", keepalive: true }).catch(() => undefined);
  }

  /**
   * The newest live frame, or why there is none: "pending" before the site box
   * sends one, "expired" when the session lapsed (start a new one), "paused"
   * after 5 minutes of continuous viewing, "limit" when the monthly minutes
   * ran out. The server has already stopped the session for paused and limit.
   */
  async liveFrame(cameraId: string, signal?: AbortSignal): Promise<LiveFrame | LiveFrameProblem> {
    const accessToken = sessionStorage.getItem("syncam.access_token");
    const response = await fetch(`${this.config.apiBaseUrl}/v1/cameras/${encodeURIComponent(cameraId)}/live/frame`, {
      method: "GET",
      cache: "no-store",
      signal,
      headers: {
        Accept: "image/jpeg",
        Authorization: `Bearer ${accessToken ?? ""}`,
        "X-SentinelVision-Tenant-ID": this.config.tenantId,
      },
    });
    if (!response.ok) {
      const code = errorCode(await response.json().catch(() => null));
      const problem = frameProblem(response.status, code);
      if (problem) return problem;
      throw new AlertApiError(`Live frame request failed with ${response.status}.`, response.status, code);
    }
    return { blob: await response.blob(), capturedAt: response.headers.get("X-Frame-Captured-At") };
  }
}

export type LiveViewSession = {
  camera_id: string;
  viewer: string;
  audited: boolean;
  expires_in_seconds: number;
  poll_interval_ms: number;
};
export type LiveFrame = { blob: Blob; capturedAt: string | null };
