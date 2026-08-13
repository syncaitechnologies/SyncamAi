import {
  AlertApiError,
  type AlertRuntimeConfig,
} from "./alert-client";
import { parseCameraListEnvelope } from "./camera-contracts";

export class CameraApiClient {
  constructor(private readonly config: AlertRuntimeConfig) {}

  async listCameras(signal?: AbortSignal) {
    const query = new URLSearchParams({ site_id: this.config.siteId });
    const accessToken = sessionStorage.getItem("syncam.access_token");
    const response = await fetch(
      `${this.config.apiBaseUrl}/v1/cameras?${query.toString()}`,
      {
        method: "GET",
        cache: "no-store",
        signal,
        headers: {
          Accept: "application/json",
          Authorization: `Bearer ${accessToken ?? ""}`,
          "X-SentinelVision-Tenant-ID": this.config.tenantId,
        },
      },
    );
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
          : `Camera API request failed with ${response.status}.`,
        response.status,
        typeof error?.code === "string" ? error.code : undefined,
      );
    }
    return parseCameraListEnvelope(payload);
  }
}
