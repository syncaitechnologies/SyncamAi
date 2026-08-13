import {
  alertFromEvent,
  alertsFromSnapshot,
  parseAlertEnvelope,
  parseAlertListEnvelope,
  parseRealtimeEnvelope,
  parseTicketEnvelope,
  type ApiAlert,
} from "./alert-contracts";

export type DataMode = "demo" | "live";
export type ConnectionState =
  | "demo"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "offline"
  | "error";

export type AlertRuntimeConfig = {
  mode: DataMode;
  apiBaseUrl: string;
  tenantId: string;
  siteId: string;
};

export function readAlertRuntimeConfig(): AlertRuntimeConfig {
  const mode =
    import.meta.env.VITE_SYNCAM_DATA_MODE === "live" ? "live" : "demo";
  return {
    mode,
    apiBaseUrl: String(import.meta.env.VITE_SYNCAM_API_BASE_URL ?? "").replace(
      /\/$/,
      "",
    ),
    tenantId: String(import.meta.env.VITE_SYNCAM_TENANT_ID ?? ""),
    siteId: String(import.meta.env.VITE_SYNCAM_SITE_ID ?? ""),
  };
}

export function validateLiveConfig(config: AlertRuntimeConfig) {
  if (config.mode !== "live") return;
  if (!config.tenantId || !config.siteId) {
    throw new Error(
      "Live mode requires VITE_SYNCAM_TENANT_ID and VITE_SYNCAM_SITE_ID.",
    );
  }
  if (!sessionStorage.getItem("syncam.access_token")) {
    throw new Error(
      "Live mode requires an OIDC access token in sessionStorage under syncam.access_token.",
    );
  }
}

export class AlertApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
    this.name = "AlertApiError";
  }
}

export class AlertApiClient {
  private readonly acknowledgementKeys = new Map<string, string>();

  constructor(private readonly config: AlertRuntimeConfig) {}

  async listAlerts(signal?: AbortSignal): Promise<ApiAlert[]> {
    const payload = await this.request("/v1/alerts", { method: "GET", signal });
    return parseAlertListEnvelope(payload);
  }

  async acknowledgeAlert(
    alertId: string,
    signal?: AbortSignal,
  ): Promise<ApiAlert> {
    const idempotencyKey =
      this.acknowledgementKeys.get(alertId) ?? crypto.randomUUID();
    this.acknowledgementKeys.set(alertId, idempotencyKey);
    const payload = await this.request(
      `/v1/alerts/${encodeURIComponent(alertId)}/acknowledge`,
      {
        method: "POST",
        signal,
        headers: {
          "Idempotency-Key": idempotencyKey,
          "X-Correlation-Id": crypto.randomUUID(),
        },
      },
    );
    return parseAlertEnvelope(payload);
  }

  async issueRealtimeTicket(signal?: AbortSignal) {
    const payload = await this.request("/v1/auth/ws-ticket", {
      method: "POST",
      signal,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ site_id: this.config.siteId }),
    });
    return parseTicketEnvelope(payload);
  }

  realtimeUrl() {
    const base = this.config.apiBaseUrl || window.location.origin;
    const url = new URL("/ws/v1/alerts", base);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.toString();
  }

  private async request(path: string, init: RequestInit): Promise<unknown> {
    const accessToken = sessionStorage.getItem("syncam.access_token");
    const response = await fetch(`${this.config.apiBaseUrl}${path}`, {
      ...init,
      cache: "no-store",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${accessToken ?? ""}`,
        "X-SentinelVision-Tenant-ID": this.config.tenantId,
        ...init.headers,
      },
    });
    const payload = (await response.json().catch(() => null)) as unknown;
    if (!response.ok) {
      const error =
        typeof payload === "object" &&
        payload !== null &&
        "error" in payload &&
        typeof payload.error === "object" &&
        payload.error !== null
          ? (payload.error as Record<string, unknown>)
          : undefined;
      throw new AlertApiError(
        typeof error?.message === "string"
          ? error.message
          : `Alert API request failed with ${response.status}.`,
        response.status,
        typeof error?.code === "string" ? error.code : undefined,
      );
    }
    return payload;
  }
}

type RealtimeCallbacks = {
  onState: (state: ConnectionState) => void;
  onSnapshot: (alerts: ApiAlert[]) => void;
  onAlert: (alert: ApiAlert, isNew: boolean) => void;
  onGap: () => void;
  onError: (message: string) => void;
};

export class AlertRealtimeClient {
  private socket?: WebSocket;
  private reconnectTimer?: number;
  private stopped = true;
  private attempts = 0;
  private ticketAbort?: AbortController;

  constructor(
    private readonly api: AlertApiClient,
    private readonly config: AlertRuntimeConfig,
    private readonly callbacks: RealtimeCallbacks,
  ) {}

  start() {
    this.stopped = false;
    void this.connect(false);
  }

  stop() {
    this.stopped = true;
    this.ticketAbort?.abort();
    if (this.reconnectTimer !== undefined)
      window.clearTimeout(this.reconnectTimer);
    if (this.socket?.readyState === WebSocket.OPEN)
      this.socket.send(JSON.stringify({ type: "unsubscribe" }));
    this.socket?.close(1000, "client stopped");
  }

  private sequenceKey() {
    return `syncam.realtime.last_seq.${this.config.tenantId}.${this.config.siteId}`;
  }

  private async connect(reconnecting: boolean) {
    if (this.stopped) return;
    this.callbacks.onState(reconnecting ? "reconnecting" : "connecting");
    this.ticketAbort = new AbortController();
    try {
      const issued = await this.api.issueRealtimeTicket(
        this.ticketAbort.signal,
      );
      if (this.stopped) return;
      const socket = new WebSocket(this.api.realtimeUrl(), [
        issued.protocol,
        `ticket.${issued.ticket}`,
      ]);
      this.socket = socket;
      socket.addEventListener("open", () => {
        this.attempts = 0;
        this.callbacks.onState("connected");
        const stored = sessionStorage.getItem(this.sequenceKey());
        socket.send(
          stored === null
            ? JSON.stringify({ type: "subscribe", topic: "alerts.*" })
            : JSON.stringify({ type: "resume", last_seq: Number(stored) || 0 }),
        );
      });
      socket.addEventListener("message", (event) =>
        this.handleMessage(event.data),
      );
      socket.addEventListener("error", () =>
        this.callbacks.onError(
          "Realtime connection encountered a transport error.",
        ),
      );
      socket.addEventListener("close", () => {
        if (!this.stopped) this.scheduleReconnect();
      });
    } catch (error) {
      if (
        this.stopped ||
        (error instanceof DOMException && error.name === "AbortError")
      )
        return;
      this.callbacks.onError(
        error instanceof Error
          ? error.message
          : "Realtime ticket request failed.",
      );
      this.scheduleReconnect();
    }
  }

  private handleMessage(raw: unknown) {
    try {
      const value =
        typeof raw === "string" ? (JSON.parse(raw) as unknown) : raw;
      const envelope = parseRealtimeEnvelope(value);
      sessionStorage.setItem(this.sequenceKey(), String(envelope.seq));
      if (envelope.type === "snapshot")
        this.callbacks.onSnapshot(alertsFromSnapshot(envelope));
      if (envelope.type === "event") {
        const alert = alertFromEvent(envelope);
        if (alert)
          this.callbacks.onAlert(alert, envelope.topic === "alerts.created");
      }
      if (envelope.type === "gap") this.callbacks.onGap();
    } catch (error) {
      this.callbacks.onError(
        error instanceof Error
          ? error.message
          : "Realtime message could not be processed.",
      );
    }
  }

  private scheduleReconnect() {
    if (this.stopped) return;
    this.attempts += 1;
    this.callbacks.onState(navigator.onLine ? "reconnecting" : "offline");
    const delay =
      Math.min(15_000, 500 * 2 ** Math.min(this.attempts, 5)) +
      Math.round(Math.random() * 250);
    this.reconnectTimer = window.setTimeout(
      () => void this.connect(true),
      delay,
    );
  }
}
