// Dev-only, synthetic UI fixture for Attendance. It never contacts the real
// API: the "api" below returns two staff and a few days of fake attendance.
// The last request is shown in document.body.dataset.lastRequest.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AttendanceWorkspace } from "../src/AttendanceWorkspace";
import type { DayRecord } from "../src/attendance-model";
import type { SyncamApi } from "../src/syncam-api";
import type { WorkspaceData } from "../src/use-workspace";
import { storeWorkspace } from "../src/workspace";
import "../src/styles.css";
import "../src/live.css";

storeWorkspace({ tenantId: "fixture-tenant", siteIds: ["fixture-site"], roles: ["site_admin"] });

const staff = [
  { id: "s1", site_id: "fixture-site", display_name: "Anil botta", employee_code: "0001", consent_recorded_at: "2026-11-14T10:00:00Z", consent_recorded_by: "admin", active: true },
  { id: "s2", site_id: "fixture-site", display_name: "Samyuktha", employee_code: "0002", consent_recorded_at: "2026-11-20T10:00:00Z", consent_recorded_by: "admin", active: true },
];

const records: DayRecord[] = [
  { date: "2026-11-24", staff_id: "s1", staff_name: "Anil botta", employee_code: "0001", first_seen: "2026-11-24T11:28:00Z", last_seen: "2026-11-25T03:09:00Z", sightings: 40, camera_id: "cam-1" },
  { date: "2026-11-23", staff_id: "s1", staff_name: "Anil botta", employee_code: "0001", first_seen: "2026-11-23T14:02:00Z", last_seen: "2026-11-23T22:45:00Z", sightings: 31, camera_id: "cam-1" },
  { date: "2026-11-23", staff_id: "s2", staff_name: "Samyuktha", employee_code: "0002", first_seen: "2026-11-23T13:30:00Z", last_seen: "2026-11-23T21:10:00Z", sightings: 22, camera_id: "cam-1" },
  { date: "2026-11-22", staff_id: "s1", staff_name: "Anil botta", employee_code: "0001", first_seen: "2026-11-22T15:00:00Z", last_seen: "2026-11-22T19:30:00Z", sightings: 12, camera_id: "cam-1" },
];

// Created once, like the real dashboard's API client.
const api = {
  siteId: "fixture-site",
  async listStaff() { return staff; },
  async dailyAttendance(_site: string, from: string, to: string) {
    document.body.dataset.lastRequest = `GET /v1/attendance/daily?from=${from}&to=${to}`;
    return records.filter((record) => record.date >= from && record.date <= to);
  },
} as unknown as SyncamApi;

const workspace = {
  api, site: { id: "fixture-site", name: "SyncAi office", timezone: "America/Toronto", status: "active" },
  names: { cameras: { "cam-1": "laptop webcam" }, zones: {} }, cameras: [], zones: [], sites: [], loading: false, error: "", reload: () => undefined,
} as unknown as WorkspaceData;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <p style={{ padding: 16 }}><strong>Synthetic Attendance fixture — no live API requests</strong></p>
    <AttendanceWorkspace workspace={workspace} onNotify={() => undefined} />
  </StrictMode>,
);
