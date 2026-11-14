import { useCallback, useEffect, useMemo, useState } from "react";

import { readAlertRuntimeConfig } from "./alert-client";
import { SyncamApi, type Camera, type Site, type Zone } from "./syncam-api";

export type WorkspaceData = {
  api: SyncamApi | null;
  sites: Site[];
  site: Site | null;
  cameras: Camera[];
  zones: Zone[];
  names: { cameras: Record<string, string>; zones: Record<string, string> };
  loading: boolean;
  error: string;
  reload: () => void;
};

/** Live-mode sites, cameras and zones for the selected site. */
export function useWorkspace(enabled: boolean): WorkspaceData {
  const config = useMemo(readAlertRuntimeConfig, []);
  const api = useMemo(() => (enabled && config.mode === "live" ? new SyncamApi(config) : null), [config, enabled]);
  const [sites, setSites] = useState<Site[]>([]);
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [zones, setZones] = useState<Zone[]>([]);
  const [loading, setLoading] = useState(Boolean(api));
  const [error, setError] = useState("");
  const [version, setVersion] = useState(0);
  const reload = useCallback(() => setVersion((value) => value + 1), []);

  useEffect(() => {
    if (!api) return;
    const abort = new AbortController();
    setLoading(true);
    setError("");
    void (async () => {
      try {
        const listed = await api.listSites(abort.signal);
        setSites(listed);
        if (api.siteId) {
          const [siteCameras, siteZones] = await Promise.all([
            api.listCameras(api.siteId, abort.signal),
            api.listZones(api.siteId, abort.signal),
          ]);
          setCameras(siteCameras.filter((camera) => camera.lifecycle_status !== "retired"));
          setZones(siteZones);
        }
      } catch (caught) {
        if (!abort.signal.aborted) setError(caught instanceof Error ? caught.message : "Workspace could not be loaded.");
      } finally {
        if (!abort.signal.aborted) setLoading(false);
      }
    })();
    return () => abort.abort();
  }, [api, version]);

  const names = useMemo(
    () => ({
      cameras: Object.fromEntries(cameras.map((camera) => [camera.id, camera.name])),
      zones: Object.fromEntries(zones.map((zone) => [zone.id, zone.name])),
    }),
    [cameras, zones],
  );
  const site = sites.find((candidate) => candidate.id === api?.siteId) ?? null;
  return { api, sites, site, cameras, zones, names, loading, error, reload };
}
