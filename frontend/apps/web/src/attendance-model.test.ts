import assert from "node:assert/strict";
import test from "node:test";
import { attendanceCsv, dayBounds, summarizeAttendance } from "./attendance-model.ts";

const staff = [
  { id: "a", site_id: "s", display_name: "Asha", employee_code: "E1", consent_recorded_at: "", consent_recorded_by: "", active: true },
  { id: "b", site_id: "s", display_name: "Ben, Jr.", consent_recorded_at: "", consent_recorded_by: "", active: true },
];

test("summarizes first and last sightings and lists absentees", () => {
  const rows = summarizeAttendance(staff, [
    { id: "2", staff_id: "a", camera_id: "c", occurred_at: "2026-11-13T17:30:00Z", confidence: 0.8 },
    { id: "1", staff_id: "a", camera_id: "c", occurred_at: "2026-11-13T09:01:00Z", confidence: 0.9 },
  ]);
  assert.equal(rows[0]!.name, "Asha");
  assert.equal(rows[0]!.firstSeen, "2026-11-13T09:01:00Z");
  assert.equal(rows[0]!.lastSeen, "2026-11-13T17:30:00Z");
  assert.equal(rows[0]!.sightings, 2);
  assert.equal(rows[1]!.firstSeen, null);
});

test("exports CSV with escaping and absentees", () => {
  const csv = attendanceCsv(summarizeAttendance(staff, []), "2026-11-13", (iso) => iso);
  assert.match(csv, /"Ben, Jr\.",,Absent,,0/);
  assert.equal(csv.split("\n")[0], "Date,Name,Employee code,First seen,Last seen,Sightings");
});

test("computes local day bounds", () => {
  const { from, to } = dayBounds("2026-11-13");
  assert.equal(to.getTime() - from.getTime() >= 23 * 3600_000, true);
  assert.equal(from.getHours(), 0);
});
