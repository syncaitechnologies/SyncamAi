import { useEffect, useMemo, useState } from "react";

import {
  readAlertRuntimeConfig,
  validateLiveConfig,
} from "./alert-client";
import type { AlertItem } from "./alert-contracts";
import { CameraApiClient } from "./camera-client";
import type { ApiCamera } from "./camera-contracts";
import {
  buildCameraWallTiles,
  buildLiveCameraWallTiles,
} from "./camera-wall-model";

export function useCameraInventory(
  alerts: AlertItem[],
  dataMode: "demo" | "live",
) {
  const config = useMemo(readAlertRuntimeConfig, []);
  const [cameras, setCameras] = useState<ApiCamera[]>([]);
  const [loaded, setLoaded] = useState(dataMode === "demo");
  const [error, setError] = useState("");

  useEffect(() => {
    if (dataMode === "demo") return;
    const abort = new AbortController();
    try {
      validateLiveConfig(config);
      const client = new CameraApiClient(config);
      void client
        .listCameras(abort.signal)
        .then(setCameras)
        .catch((reason: unknown) => {
          if (reason instanceof DOMException && reason.name === "AbortError")
            return;
          setError(
            reason instanceof Error
              ? reason.message
              : "Camera inventory could not be loaded.",
          );
        })
        .finally(() => setLoaded(true));
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "Live camera mode could not start.",
      );
      setLoaded(true);
    }
    return () => abort.abort();
  }, [config, dataMode]);

  const tiles = useMemo(
    () =>
      dataMode === "demo"
        ? buildCameraWallTiles(alerts, dataMode)
        : buildLiveCameraWallTiles(cameras, alerts),
    [alerts, cameras, dataMode],
  );

  return { tiles, loaded, error };
}
