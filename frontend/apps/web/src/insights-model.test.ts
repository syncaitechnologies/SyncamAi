import assert from "node:assert/strict";
import test from "node:test";

import {
  averageDiningMinutes,
  barPercent,
  busiestHour,
  dailySeries,
  doorNames,
  durationLabel,
  hourLabel,
  hourSeries,
  tableRows,
  totalVisitors,
  turnover,
  type InsightsSummary,
} from "./insights-model.ts";

const summary: InsightsSummary = {
  days: [{ date: "2026-11-22", entries: 40, exits: 38 }, { date: "2026-11-24", entries: 60, exits: 55 }],
  hours: [{ hour: 12, entries: 30 }, { hour: 19, entries: 70 }],
  zones: [
    { zone_id: "d", name: "Front door", kind: "door_line", entries: 100, exits: 93, occupied_seconds: 0, sessions: 0, session_seconds: 0, peak_people: 0 },
    { zone_id: "t1", name: "Table 1", kind: "table", entries: 0, exits: 0, occupied_seconds: 18000, sessions: 6, session_seconds: 16200, peak_people: 4 },
    { zone_id: "t2", name: "Table 2", kind: "table", entries: 0, exits: 0, occupied_seconds: 3600, sessions: 0, session_seconds: 0, peak_people: 2 },
    { zone_id: "x", name: "Removed zone", kind: "", entries: 0, exits: 0, occupied_seconds: 600, sessions: 1, session_seconds: 1800, peak_people: 1 },
  ],
};

test("visitor totals and the daily series fill empty days", () => {
  assert.equal(totalVisitors(summary), 100);
  assert.deepEqual(dailySeries(summary, "2026-11-22", "2026-11-24"), [
    { date: "2026-11-22", entries: 40 }, { date: "2026-11-23", entries: 0 }, { date: "2026-11-24", entries: 60 },
  ]);
});

test("busiest hour and labels", () => {
  assert.equal(hourSeries(summary).length, 24);
  assert.deepEqual(busiestHour(summary), { hour: 19, entries: 70 });
  assert.equal(busiestHour({ days: [], hours: [], zones: [] }), null);
  assert.equal(hourLabel(19), "7 pm – 8 pm");
  assert.equal(hourLabel(11), "11 am – 12 pm");
  assert.equal(hourLabel(0), "12 am – 1 am");
});

test("tables: dining time, turnover and removed zones", () => {
  const rows = tableRows(summary, 3);
  assert.deepEqual(rows.map((row) => row.name), ["Table 1", "Removed zone", "Table 2"]);
  assert.equal(rows[0]!.averageMinutes, 45);
  assert.equal(rows[0]!.sessionsPerDay, 2);
  assert.equal(rows[2]!.averageMinutes, null);
  assert.equal(averageDiningMinutes(summary), 18000 / 7 / 60);
  assert.equal(turnover(summary, 3), Math.round((7 / 3 / 3) * 10) / 10);
  assert.deepEqual(doorNames(summary), ["Front door"]);
});

test("durations and bars", () => {
  assert.equal(durationLabel(45), "45 min");
  assert.equal(durationLabel(72.4), "1 h 12 min");
  assert.equal(durationLabel(null), "–");
  assert.equal(barPercent(0, 10), 0);
  assert.equal(barPercent(1, 1000), 2, "a small non-zero bar stays visible");
  assert.equal(barPercent(5, 10), 50);
  assert.equal(barPercent(3, 0), 0);
});
