import { useEffect, useRef, useState } from "react";

import {
  contactHref,
  isLimitError,
  isSwitchedOffError,
  SWITCHED_OFF_MESSAGE,
  isUnlimited,
  limitMessage,
  minutesLeftLabel,
  PAUSED_MESSAGE,
  type LiveViewUsage,
} from "./live-view-model";
import type { SyncamApi } from "./syncam-api";

type FeedState =
  | { kind: "starting" }
  | { kind: "waiting"; viewer: string }
  | { kind: "playing"; viewer: string; url: string; capturedAt: string | null }
  | { kind: "paused" }
  | { kind: "limit" }
  | { kind: "off" }
  | { kind: "error"; message: string };

const POLL_MS = 500;
const RETRY_MS = 5000;

function frameAge(capturedAt: string | null, now: number) {
  if (!capturedAt) return "";
  const seconds = Math.max(0, (now - new Date(capturedAt).getTime()) / 1000);
  return seconds < 1 ? "now" : `${seconds.toFixed(1)} s ago`;
}

/**
 * Near-live frames from the site box (about two a second). Opening this
 * starts an audited viewing session; closing it, or hiding the tab, stops
 * the site box from sending frames. Frames are never stored.
 *
 * Live view is metered per workspace. After 5 minutes of continuous viewing
 * the server pauses the session and this asks "Still watching?"; when the
 * month's minutes run out it stops and never retries on its own.
 */
export function LiveCameraFeed({
  api,
  cameraId,
  cameraName,
  usage = null,
  contactEmail,
  onSessionChange,
}: {
  api: SyncamApi;
  cameraId: string;
  cameraName: string;
  /** This month's allowance, for the minutes-left label. */
  usage?: LiveViewUsage | null;
  /** Shown as a contact link when the minutes are used up. */
  contactEmail?: string;
  /** Called when a session starts or stops, so the caller can reload usage. */
  onSessionChange?: () => void;
}) {
  const [state, setState] = useState<FeedState>({ kind: "starting" });
  const [now, setNow] = useState(() => Date.now());
  // Bumped by "Still watching?" to start a fresh session.
  const [run, setRun] = useState(0);
  const notify = useRef(onSessionChange);
  useEffect(() => {
    notify.current = onSessionChange;
  }, [onSessionChange]);

  useEffect(() => {
    let cancelled = false;
    let halted = false;
    let timer: number | undefined;
    let shownUrl: string | null = null;
    let viewer = "";
    let abort = new AbortController();
    setState({ kind: "starting" });

    const show = (next: FeedState) => {
      if (cancelled) return;
      setState((previous) => {
        if (previous.kind === "playing" && (next.kind !== "playing" || next.url !== previous.url)) {
          URL.revokeObjectURL(previous.url);
        }
        return next;
      });
    };

    // The server already stopped the session; stay put until the user acts.
    const halt = (next: FeedState) => {
      halted = true;
      viewer = "";
      window.clearTimeout(timer);
      show(next);
      notify.current?.();
    };

    const start = async () => {
      const session = await api.startLiveView(cameraId, abort.signal);
      viewer = session.viewer;
      notify.current?.();
    };

    const poll = async () => {
      if (cancelled || halted || document.hidden) return;
      try {
        if (!viewer) {
          await start();
          show({ kind: "waiting", viewer });
        }
        const frame = await api.liveFrame(cameraId, abort.signal);
        if (frame === "paused") return halt({ kind: "paused" });
        if (frame === "limit") return halt({ kind: "limit" });
        if (frame === "off") return halt({ kind: "off" });
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
        if (isLimitError(error)) return halt({ kind: "limit" });
        if (isSwitchedOffError(error)) return halt({ kind: "off" });
        show({ kind: "error", message: error instanceof Error ? error.message : "Live view is unavailable." });
        viewer = "";
        timer = window.setTimeout(() => void poll(), RETRY_MS);
        return;
      }
      timer = window.setTimeout(() => void poll(), POLL_MS);
    };

    const onVisibility = () => {
      if (halted) return;
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
      if (!halted) {
        void api.stopLiveView(cameraId);
        notify.current?.();
      }
      if (shownUrl) URL.revokeObjectURL(shownUrl);
    };
  }, [api, cameraId, run]);

  const viewer = state.kind === "waiting" || state.kind === "playing" ? state.viewer : "";
  const contact = state.kind === "limit" ? contactHref(contactEmail, "More SyncCam live view minutes") : null;
  return (
    <div className="live-feed" role="region" aria-label={`Live view of ${cameraName}`}>
      {state.kind === "playing" ? (
        <img src={state.url} alt={`Live view of ${cameraName}`} />
      ) : state.kind === "paused" ? (
        <div className="live-feed-message live-feed-halted" role="status">
          <strong>{PAUSED_MESSAGE}</strong>
          <small>Live view pauses so a forgotten tab does not use up the month's minutes.</small>
          <button type="button" className="wall-live-action" onClick={() => setRun((value) => value + 1)}>
            Still watching?
          </button>
        </div>
      ) : state.kind === "off" ? (
        <div className="live-feed-message live-feed-halted" role="status">
          <strong>{SWITCHED_OFF_MESSAGE}</strong>
          <small>Still pictures keep updating on the camera wall. A workspace admin can switch live view back on in Features.</small>
        </div>
      ) : state.kind === "limit" ? (
        <div className="live-feed-message live-feed-halted" role="alert">
          <strong>{limitMessage(usage)}</strong>
          <small>
            Still pictures keep updating on the camera wall.{" "}
            {contact ? (
              <>To add minutes, contact <a href={contact}>{contactEmail?.trim()}</a>.</>
            ) : (
              "Ask your SyncCam contact to add minutes."
            )}
          </small>
        </div>
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
        <div className="live-feed-chips">
          <span className="live-feed-chip" title="Every live viewing session is recorded in the audit log.">
            <i /> LIVE · viewed by {viewer} · logged
          </span>
          {usage && !isUnlimited(usage) && (
            <span className="live-feed-minutes" title="Live view minutes left for this workspace this month.">
              {minutesLeftLabel(usage)}
            </span>
          )}
        </div>
      )}
      {state.kind === "playing" && <span className="live-feed-age">{frameAge(state.capturedAt, now)}</span>}
    </div>
  );
}
