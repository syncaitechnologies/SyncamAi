// Dev-only, synthetic UI fixture for metered live view. Not a Vite production
// entry point. Every API call goes to an in-page fake under /fixture-api; no
// camera, site box, Supabase or Go API is contacted, and every picture is
// drawn on a canvas here.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import type { ApiCamera } from "../src/camera-contracts";
import { CameraWall } from "../src/CameraWall";
import { SyncamApi } from "../src/syncam-api";
import "../src/styles.css";
import "../src/live.css";

type Scenario = {
  label: string;
  limitMinutes: number | null;
  usedSeconds: number;
  /** Fixture stand-in for the server's 5-minute pause, shortened so it can be watched. */
  pauseAfterMs?: number;
  /** Usage reports minutes left but the server refuses to start (stale usage). */
  refuseStart?: boolean;
  /** Metered seconds per real second of viewing, so the minutes visibly drop. */
  meterSpeed?: number;
};

const scenarios: Record<string, Scenario> = {
  metered: { label: "10 min plan, 3 used — plays normally", limitMinutes: 10, usedSeconds: 180, meterSpeed: 10 },
  paused: { label: "Pauses after 8 s (stands in for 5 min)", limitMinutes: 10, usedSeconds: 60, pauseAfterMs: 8_000 },
  "limit-mid-session": { label: "10 s of minutes left — limit hits while watching", limitMinutes: 10, usedSeconds: 590 },
  "used-up": { label: "Minutes used up — Watch live disabled", limitMinutes: 10, usedSeconds: 600 },
  "refused-start": { label: "Usage looks fine but start is refused (no retry)", limitMinutes: 10, usedSeconds: 120, refuseStart: true },
  off: { label: "Live view off (limit 0)", limitMinutes: 0, usedSeconds: 0 },
  unlimited: { label: "Owner workspace — unlimited", limitMinutes: null, usedSeconds: 7_200 },
};

const minute = 60_000;
// Still per camera: capture-time header (or none), or 404 when the box has not sent one.
const cameras: { camera: ApiCamera; still: { header?: "X-Frame-Captured-At" | "Last-Modified"; ageMs: number } | null; hue: number }[] = [
  ["Front gate", { header: "X-Frame-Captured-At" as const, ageMs: 2 * minute }, 200],
  ["Loading bay", { header: "X-Frame-Captured-At" as const, ageMs: 27 * minute }, 30],
  ["Stock room", { header: "Last-Modified" as const, ageMs: 4 * minute }, 120],
  ["Car park", { ageMs: 0 }, 260],
  ["Back door", null, 0],
].map(([name, still, hue], index) => ({
  camera: {
    id: `fixture-cam-${index + 1}`,
    tenant_id: "synthetic-tenant",
    site_id: "synthetic-site",
    serial_number: `SYN-${index + 1}`,
    name: name as string,
    tags: [],
    lifecycle_status: "active",
    config_version: 1,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
  },
  still: still as { header?: "X-Frame-Captured-At" | "Last-Modified"; ageMs: number } | null,
  hue: hue as number,
}));

function picture(title: string, subtitle: string, hue: number): Promise<Blob> {
  const canvas = document.createElement("canvas");
  canvas.width = 640;
  canvas.height = 360;
  const context = canvas.getContext("2d")!;
  const gradient = context.createLinearGradient(0, 0, 640, 360);
  gradient.addColorStop(0, `hsl(${hue} 35% 30%)`);
  gradient.addColorStop(1, `hsl(${hue + 40} 30% 12%)`);
  context.fillStyle = gradient;
  context.fillRect(0, 0, 640, 360);
  context.fillStyle = "rgba(255,255,255,.08)";
  for (let x = 0; x < 640; x += 40) context.fillRect(x, 0, 1, 360);
  context.fillStyle = "#fff";
  context.font = "bold 34px sans-serif";
  context.fillText(title, 32, 70);
  context.font = "20px monospace";
  context.fillText(subtitle, 32, 110);
  context.fillText("SYNTHETIC FIXTURE IMAGE", 32, 330);
  return new Promise((resolve) => canvas.toBlob((blob) => resolve(blob!), "image/jpeg", 0.8));
}

type Counts = { usage: number; start: number; refused: number; frame: number; stop: number; still: number };

function json(status: number, body: unknown) {
  return new Response(body === null ? null : JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

/** An in-page fake of the live view endpoints for one scenario. */
function fakeServer(scenario: Scenario, onCount: (counts: Counts) => void) {
  const counts: Counts = { usage: 0, start: 0, refused: 0, frame: 0, stop: 0, still: 0 };
  let settled = scenario.usedSeconds;
  let session: { cameraId: string; startedAt: number } | null = null;
  const bump = (key: keyof Counts) => {
    counts[key] += 1;
    onCount({ ...counts });
  };
  const used = () => settled + (session ? ((Date.now() - session.startedAt) / 1000) * (scenario.meterSpeed ?? 1) : 0);
  const end = () => {
    settled = used();
    session = null;
  };
  const exhausted = () => scenario.limitMinutes !== null && used() >= scenario.limitMinutes * 60;
  const limitBody = { error: { code: "LIVE_VIEW_LIMIT", message: "Live view minutes for this month are used up." } };

  return async (path: string, init?: RequestInit): Promise<Response> => {
    const method = init?.method ?? "GET";
    await new Promise((resolve) => setTimeout(resolve, 60));
    if (path === "/v1/live-view/usage") {
      bump("usage");
      return json(200, { data: { limit_minutes: scenario.limitMinutes, used_seconds: Math.floor(used()), month: "2026-11-01", resets_at: "2026-12-01T00:00:00Z" } });
    }
    const match = /^\/v1\/cameras\/([^/]+)\/(live|live\/frame|reference)$/.exec(path);
    if (!match) return json(404, { error: { code: "NOT_FOUND", message: "Not in the fixture." } });
    const [, cameraId, route] = match as unknown as [string, string, string];
    const entry = cameras.find(({ camera }) => camera.id === cameraId);
    if (route === "reference") {
      bump("still");
      if (!entry?.still) return json(404, { error: { code: "NOT_FOUND", message: "No reference image yet." } });
      const capturedAt = new Date(Date.now() - entry.still.ageMs);
      const headers: Record<string, string> = { "Content-Type": "image/jpeg" };
      if (entry.still.header === "X-Frame-Captured-At") headers["X-Frame-Captured-At"] = capturedAt.toISOString();
      if (entry.still.header === "Last-Modified") headers["Last-Modified"] = capturedAt.toUTCString();
      return new Response(await picture(entry.camera.name, `Still picture · ${capturedAt.toISOString().slice(11, 19)} UTC`, entry.hue), { headers });
    }
    if (route === "live" && method === "POST") {
      if (scenario.refuseStart || exhausted()) {
        bump("refused");
        return json(403, limitBody);
      }
      bump("start");
      if (session) end();
      session = { cameraId, startedAt: Date.now() };
      return json(200, { data: { camera_id: cameraId, viewer: "fixture-operator", audited: true, expires_in_seconds: 30, poll_interval_ms: 500 } });
    }
    if (route === "live" && method === "DELETE") {
      bump("stop");
      if (session?.cameraId === cameraId) end();
      return json(204, null);
    }
    bump("frame");
    if (!session || session.cameraId !== cameraId) return json(409, { error: { code: "LIVE_SESSION_REQUIRED", message: "Start a session." } });
    if (exhausted()) {
      end();
      return json(403, limitBody);
    }
    const elapsed = Date.now() - session.startedAt;
    if (scenario.pauseAfterMs !== undefined && elapsed > scenario.pauseAfterMs) {
      end();
      return json(409, { error: { code: "LIVE_VIEW_PAUSED", message: "Paused after 5 minutes." } });
    }
    if (elapsed < 800) return json(404, { error: { code: "LIVE_FRAME_PENDING", message: "Waiting for the site box." } });
    const now = new Date();
    return new Response(await picture(entry?.camera.name ?? cameraId, `LIVE ${now.toISOString().slice(11, 22)} UTC`, entry?.hue ?? 0), {
      headers: { "Content-Type": "image/jpeg", "X-Frame-Captured-At": now.toISOString() },
    });
  };
}

let handler: ((path: string, init?: RequestInit) => Promise<Response>) | null = null;
const realFetch = window.fetch.bind(window);
window.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input instanceof Request ? input.url : input);
  const path = url.startsWith("/fixture-api") ? url.slice("/fixture-api".length) : null;
  return path !== null && handler ? handler(path, init) : realFetch(input, init);
}) as typeof fetch;
sessionStorage.setItem("syncam.access_token", "synthetic-fixture-token");

function Scenario({ scenario, contact }: { scenario: Scenario; contact: boolean }) {
  const [counts, setCounts] = useState<Counts | null>(null);
  const api = useMemo(() => {
    handler = fakeServer(scenario, setCounts);
    return new SyncamApi({ mode: "live", apiBaseUrl: "/fixture-api", tenantId: "synthetic-tenant", siteId: "synthetic-site" });
  }, [scenario]);
  return (
    <>
      <p role="status" data-fixture-counts style={{ margin: "0 16px 8px", font: "12px monospace", color: "#9fb3bd" }}>
        Requests — usage {counts?.usage ?? 0} · start {counts?.start ?? 0} · refused {counts?.refused ?? 0} · frames {counts?.frame ?? 0} · stop {counts?.stop ?? 0} · stills {counts?.still ?? 0}
      </p>
      <CameraWall
        alerts={[]}
        dataMode="live"
        site="Synthetic site"
        api={api}
        fixtureCameras={cameras.map(({ camera }) => camera)}
        contactEmail={contact ? "sales@example.invalid" : ""}
        onOpenAlertCenter={() => undefined}
        onNotify={() => undefined}
      />
    </>
  );
}

function Fixture() {
  const [name, setName] = useState(() => new URLSearchParams(location.search).get("scenario") ?? "metered");
  const [contact, setContact] = useState(true);
  const scenario = scenarios[name] ?? scenarios.metered!;
  return (
    <main style={{ minHeight: "100vh", padding: 16, background: "#0b1116" }}>
      <aside style={{ display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center", margin: "0 16px 12px", color: "#dce6ec", font: "13px sans-serif" }}>
        <strong>Synthetic live view fixture — fake API, no cameras</strong>
        <label style={{ maxWidth: "100%" }}>
          Scenario{" "}
          <select value={name} style={{ maxWidth: "100%" }} onChange={(event) => setName(event.target.value)}>
            {Object.entries(scenarios).map(([key, value]) => (
              <option key={key} value={key}>{key} — {value.label}</option>
            ))}
          </select>
        </label>
        <label><input type="checkbox" checked={contact} onChange={(event) => setContact(event.target.checked)} /> Contact email set</label>
      </aside>
      <Scenario key={`${name}-${contact}`} scenario={scenario} contact={contact} />
    </main>
  );
}

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
