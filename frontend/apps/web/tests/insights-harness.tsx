// Dev-only, synthetic UI fixture for Insights. It never contacts the real API.
// ?empty=1 shows the page before any counter zones exist.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { InsightsWorkspace } from "../src/InsightsWorkspace";
import type { InsightsSummary } from "../src/insights-model";
import type { SyncamApi } from "../src/syncam-api";
import type { WorkspaceData } from "../src/use-workspace";
import "../src/styles.css";
import "../src/live.css";

const empty = new URLSearchParams(window.location.search).has("empty");

function fakeSummary(from: string, to: string): InsightsSummary {
  const days = [];
  for (let day = new Date(`${from}T00:00:00Z`); day <= new Date(`${to}T00:00:00Z`); day = new Date(day.getTime() + 86_400_000)) {
    const date = day.toISOString().slice(0, 10);
    const weekend = [0, 6].includes(day.getUTCDay());
    days.push({ date, entries: weekend ? 140 : 90, exits: weekend ? 136 : 87 });
  }
  const hours = [11, 12, 13, 14, 18, 19, 20, 21].map((hour) => ({ hour, entries: hour === 19 ? 220 : hour === 13 ? 160 : 60 }));
  return {
    days, hours,
    open_hours: days.length * 11,
    zones: [
      { zone_id: "door", name: "Front door", kind: "door_line", entries: days.reduce((total, day) => total + day.entries, 0), exits: 0, occupied_seconds: 0, sessions: 0, session_seconds: 0, peak_people: 0 },
      { zone_id: "t1", name: "Table 1 (window)", kind: "table", entries: 0, exits: 0, occupied_seconds: 90000, sessions: days.length * 6, session_seconds: days.length * 6 * 3000, peak_people: 4 },
      { zone_id: "t2", name: "Table 2", kind: "table", entries: 0, exits: 0, occupied_seconds: 54000, sessions: days.length * 4, session_seconds: days.length * 4 * 2400, peak_people: 6 },
    ],
  };
}

const api = {
  siteId: "fixture-site",
  async insights(_site: string, from: string, to: string) {
    document.body.dataset.lastRequest = `GET /v1/analytics/insights?from=${from}&to=${to}`;
    return empty ? { days: [], hours: [], zones: [] } : fakeSummary(from, to);
  },
} as unknown as SyncamApi;

const workspace = { api, site: { id: "fixture-site", name: "Cafe", timezone: "America/Toronto", status: "active" } } as unknown as WorkspaceData;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <p style={{ padding: 16 }}><strong>Synthetic Insights fixture — no live API requests</strong></p>
    <InsightsWorkspace workspace={workspace} />
  </StrictMode>,
);
