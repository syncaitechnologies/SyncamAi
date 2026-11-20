import { useEffect, useRef, useState } from "react";

import { STILL_REFRESH_MS } from "./live-view-model";
import type { SyncamApi } from "./syncam-api";

/** A camera's reference still as an object URL. */
export type CameraStill = { url: string; capturedAt: string | null };

const CHECK_MS = 30_000;
const PARALLEL = 4;

/**
 * Reference stills for the given cameras, reloaded every 5 minutes while the
 * tab is visible (the site box uploads a new one about that often). A camera
 * maps to `undefined` until its first load and to `null` when the server has
 * no still for it yet. Stills cost one small JPEG per camera per refresh, not
 * a stream.
 */
export function useCameraStills(api: SyncamApi | null, cameraIds: readonly string[]) {
  const [stills, setStills] = useState<Record<string, CameraStill | null>>({});
  const urls = useRef(new Map<string, string>());
  // When each camera's still was last requested; drives the refresh.
  const requestedAt = useRef(new Map<string, number>());
  const idsKey = cameraIds.join("\n");

  useEffect(() => {
    if (!api || !idsKey) return;
    const ids = idsKey.split("\n");
    const abort = new AbortController();
    // Requested by this run but not finished; un-marked on cleanup so the next run fetches them.
    const unfinished = new Set<string>();

    const loadStale = async () => {
      if (document.hidden) return;
      const now = Date.now();
      const queue = ids.filter((id) => now - (requestedAt.current.get(id) ?? 0) >= STILL_REFRESH_MS);
      for (const id of queue) {
        requestedAt.current.set(id, now);
        unfinished.add(id);
      }
      const worker = async () => {
        for (let id = queue.shift(); id !== undefined; id = queue.shift()) {
          try {
            const still = await api.cameraStill(id, abort.signal);
            if (abort.signal.aborted) {
              if (still) URL.revokeObjectURL(still.url);
              return;
            }
            unfinished.delete(id);
            const previous = urls.current.get(id);
            if (previous) URL.revokeObjectURL(previous);
            if (still) urls.current.set(id, still.url);
            else urls.current.delete(id);
            setStills((current) => ({ ...current, [id]: still }));
          } catch {
            // Aborted: cleanup un-marks it. Other errors keep the old still until the next refresh.
            if (abort.signal.aborted) return;
            unfinished.delete(id);
          }
        }
      };
      await Promise.all(Array.from({ length: PARALLEL }, worker));
    };

    void loadStale();
    const timer = window.setInterval(() => void loadStale(), CHECK_MS);
    const onVisibility = () => void loadStale();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      abort.abort();
      for (const id of unfinished) requestedAt.current.delete(id);
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [api, idsKey]);

  useEffect(() => {
    const owned = urls.current;
    return () => {
      for (const url of owned.values()) URL.revokeObjectURL(url);
      owned.clear();
    };
  }, []);

  return stills;
}
