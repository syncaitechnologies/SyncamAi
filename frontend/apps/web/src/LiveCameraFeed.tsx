import { useEffect, useState } from "react";

import type { SyncamApi } from "./syncam-api";

type FeedState =
  | { kind: "starting" }
  | { kind: "waiting"; viewer: string }
  | { kind: "playing"; viewer: string; url: string; capturedAt: string | null }
  | { kind: "error"; message: string };

const POLL_MS = 500;

function frameAge(capturedAt: string | null, now: number) {
  if (!capturedAt) return "";
  const seconds = Math.max(0, (now - new Date(capturedAt).getTime()) / 1000);
  return seconds < 1 ? "now" : `${seconds.toFixed(1)} s ago`;
}

/**
 * Near-live frames from the site box (about two a second). Opening this
 * starts an audited viewing session; closing it, or hiding the tab, stops
 * the site box from sending frames. Frames are never stored.
 */
export function LiveCameraFeed({ api, cameraId, cameraName }: { api: SyncamApi; cameraId: string; cameraName: string }) {
  const [state, setState] = useState<FeedState>({ kind: "starting" });
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    let shownUrl: string | null = null;
    let viewer = "";
    let abort = new AbortController();

    const show = (next: FeedState) => {
      if (cancelled) return;
      setState((previous) => {
        if (previous.kind === "playing" && (next.kind !== "playing" || next.url !== previous.url)) {
          URL.revokeObjectURL(previous.url);
        }
        return next;
      });
    };

    const start = async () => {
      const session = await api.startLiveView(cameraId, abort.signal);
      viewer = session.viewer;
    };

    const poll = async () => {
      if (cancelled || document.hidden) return;
      try {
        if (!viewer) {
          await start();
          show({ kind: "waiting", viewer });
        }
        const frame = await api.liveFrame(cameraId, abort.signal);
        if (frame === "expired") {
          viewer = "";
        } else if (frame === "pending") {
          show({ kind: "waiting", viewer });
        } else {
          shownUrl = URL.createObjectURL(frame.blob);
          show({ kind: "playing", viewer, url: shownUrl, capturedAt: frame.capturedAt });
        }
      } catch (error) {
        if (cancelled || (error instanceof DOMException && error.name === "AbortError")) return;
        show({ kind: "error", message: error instanceof Error ? error.message : "Live view is unavailable." });
        viewer = "";
        timer = window.setTimeout(() => void poll(), 5000);
        return;
      }
      timer = window.setTimeout(() => void poll(), POLL_MS);
    };

    const onVisibility = () => {
      window.clearTimeout(timer);
      if (document.hidden) {
        abort.abort();
        void api.stopLiveView(cameraId);
        viewer = "";
      } else {
        abort = new AbortController();
        void poll();
      }
    };

    void poll();
    document.addEventListener("visibilitychange", onVisibility);
    const clock = window.setInterval(() => setNow(Date.now()), 500);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      window.clearInterval(clock);
      document.removeEventListener("visibilitychange", onVisibility);
      abort.abort();
      void api.stopLiveView(cameraId);
      if (shownUrl) URL.revokeObjectURL(shownUrl);
    };
  }, [api, cameraId]);

  const viewer = state.kind === "waiting" || state.kind === "playing" ? state.viewer : "";
  return (
    <div className="live-feed" role="region" aria-label={`Live view of ${cameraName}`}>
      {state.kind === "playing" ? (
        <img src={state.url} alt={`Live view of ${cameraName}`} />
      ) : (
        <div className="live-feed-message" role="status">
          {state.kind === "error"
            ? state.message
            : state.kind === "starting"
              ? "Starting live view…"
              : "Waiting for the site box… (it checks every 2 seconds)"}
        </div>
      )}
      {viewer && (
        <span className="live-feed-chip" title="Every live viewing session is recorded in the audit log.">
          <i /> LIVE · viewed by {viewer} · logged
        </span>
      )}
      {state.kind === "playing" && <span className="live-feed-age">{frameAge(state.capturedAt, now)}</span>}
    </div>
  );
}
