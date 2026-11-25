import assert from "node:assert/strict";
import test from "node:test";
import { attendanceCsv, dailyRows, quickRanges, rangeDays, rangeLabel, rangeProblem, staffTotals, type DayRecord } from "./attendance-model.ts";

const staff = [
  { id: "a", site_id: "s", display_name: "Asha", employee_code: "E1", consent_recorded_at: "", consent_recorded_by: "", active: true },
  { id: "b", site_id: "s", display_name: "Ben, Jr.", consent_recorded_at: "", consent_recorded_by: "", active: true },
];

function day(date: string, staffId: string, first: string, last: string): DayRecord {
  return { date, staff_id: staffId, staff_name: staffId === "a" ? "Asha" : "Old", first_seen: first, last_seen: last, sightings: 3, camera_id: "cam" };
}

test("ranges include both ends, newest first", () => {
  assert.deepEqual(rangeDays("2026-11-22", "2026-11-24"), ["2026-11-24", "2026-11-23", "2026-11-22"]);
  assert.deepEqual(rangeDays("2026-11-24", "2026-11-24"), ["2026-11-24"]);
  assert.equal(rangeDays("2026-11-01", "2026-11-30").length, 30);
});

test("range problems", () => {
  assert.equal(rangeProblem("2026-11-01", "2026-11-24"), null);
  assert.match(rangeProblem("2026-11-24", "2026-11-01") ?? "", /on or after/);
  assert.match(rangeProblem("2026-01-01", "2026-12-31") ?? "", /at most 92 days/);
  assert.match(rangeProblem("", "2026-11-24") ?? "", /Choose a start/);
});

test("quick ranges", () => {
  const ranges = Object.fromEntries(quickRanges("2026-11-24").map((range) => [range.label, [range.from, range.to]]));
  assert.deepEqual(ranges["Today"], ["2026-11-24", "2026-11-24"]);
  assert.deepEqual(ranges["Yesterday"], ["2026-11-23", "2026-11-23"]);
  assert.deepEqual(ranges["Last 7 days"], ["2026-11-18", "2026-11-24"]);
  assert.deepEqual(ranges["This month"], ["2026-11-01", "2026-11-24"]);
});

test("one row per staff member per day, with absentees and former staff", () => {
  const records = [
    day("2026-11-24", "a", "2026-11-24T06:28:00Z", "2026-11-24T22:09:00Z"),
    day("2026-11-22", "a", "2026-11-22T09:00:00Z", "2026-11-22T17:00:00Z"),
    day("2026-11-23", "x", "2026-11-23T10:00:00Z", "2026-11-23T11:00:00Z"),
  ];
  const rows = dailyRows(staff, records, rangeDays("2026-11-22", "2026-11-24"));
  assert.deepEqual(rows.map((row) => `${row.date} ${row.name} ${row.firstSeen ? "in" : "absent"}`), [
    "2026-11-24 Asha in", "2026-11-24 Ben, Jr. absent",
    "2026-11-23 Old in", "2026-11-23 Asha absent", "2026-11-23 Ben, Jr. absent",
    "2026-11-22 Asha in", "2026-11-22 Ben, Jr. absent",
  ]);
  const totals = staffTotals(rows);
  assert.deepEqual(totals.map((total) => `${total.name}:${total.daysPresent}`), ["Asha:2", "Ben, Jr.:0", "Old:1"]);
});

test("CSV export covers every day and escapes names", () => {
  const rows = dailyRows(staff, [day("2026-11-24", "a", "2026-11-24T06:28:00Z", "2026-11-24T22:09:00Z")], rangeDays("2026-11-23", "2026-11-24"));
  const csv = attendanceCsv(rows, (iso) => iso.slice(11, 16));
  const lines = csv.trim().split("\n");
  assert.equal(lines[0], "Date,Name,Employee code,First seen,Last seen,Sightings");
  assert.equal(lines[1], "2026-11-24,Asha,E1,06:28,22:09,3");
  assert.match(csv, /2026-11-23,"Ben, Jr\.",,Absent,,0/);
  assert.equal(lines.length, 5);
});

test("labels", () => {
  assert.match(rangeLabel("2026-11-24", "2026-11-24", "en-US"), /Tuesday, November 24/);
  assert.equal(rangeLabel("2026-11-01", "2026-11-24", "en-US"), "November 1 – November 24");
});
