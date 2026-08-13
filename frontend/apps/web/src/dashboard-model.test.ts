import assert from "node:assert/strict";
import test from "node:test";

import type { AlertItem } from "./alert-contracts.ts";
import {
  buildDashboardSummary,
  filterAlertsForRange,
} from "./dashboard-model.ts";

function alert(overrides: Partial<AlertItem>): AlertItem {
  return {
    id: "alert-1",
    title: "Zone entry",
    summary: "Movement detected.",
    severity: "High",
    status: "unacknowledged",
    type: "Zone intrusion",
    site: "Northstar",
    zone: "Dock",
    camera: "CAM-01",
    occurredAt: "2026-08-13T14:00:00Z",
    confidence: 0.9,
    model: "model-1",
    threshold: 0.8,
    eventId: "event-1",
    color: "#f2a93b",
    evidence: "Snapshot ready",
    ...overrides,
  };
}

test("dashboard summary excludes inactive and placeholder alerts", () => {
  const summary = buildDashboardSummary([
    alert({ id: "critical", severity: "Critical", confidence: 0.96 }),
    alert({ id: "acked", status: "acknowledged", confidence: 0.84 }),
    alert({ id: "resolved", status: "resolved" }),
    alert({ id: "placeholder", placeholder: true }),
  ]);

  assert.equal(summary.activeIncidents, 2);
  assert.equal(summary.unacknowledged, 1);
  assert.equal(summary.criticalOpen, 1);
  assert.ok(
    summary.averageConfidence !== null &&
      Math.abs(summary.averageConfidence - 0.9) < 0.000_001,
  );
});

test("dashboard summary groups modules and orders recent alerts by severity then age", () => {
  const summary = buildDashboardSummary([
    alert({ id: "high-old", occurredAt: "2026-08-13T12:00:00Z" }),
    alert({ id: "critical", severity: "Critical", type: "Fall detection" }),
    alert({ id: "high-new", occurredAt: "2026-08-13T15:00:00Z" }),
  ]);

  assert.deepEqual(
    summary.recentAlerts.map((item) => item.id),
    ["critical", "high-new", "high-old"],
  );
  assert.deepEqual(summary.moduleCounts, [
    { label: "Zone intrusion", count: 2, percent: 67 },
    { label: "Fall detection", count: 1, percent: 33 },
  ]);
});

test("dashboard summary returns honest empty metrics", () => {
  const summary = buildDashboardSummary([]);

  assert.equal(summary.averageConfidence, null);
  assert.deepEqual(summary.moduleCounts, []);
  assert.deepEqual(summary.severityCounts, {
    Critical: 0,
    High: 0,
    Medium: 0,
  });
});

test("dashboard time windows filter queue-derived metrics deterministically", () => {
  const now = new Date("2026-08-13T16:00:00Z").getTime();
  const alerts = [
    alert({ id: "recent", occurredAt: "2026-08-13T15:00:00Z" }),
    alert({ id: "week", occurredAt: "2026-08-10T15:00:00Z" }),
    alert({ id: "month", occurredAt: "2026-07-25T15:00:00Z" }),
  ];

  assert.deepEqual(filterAlertsForRange(alerts, "24h", now).map(({ id }) => id), [
    "recent",
  ]);
  assert.deepEqual(filterAlertsForRange(alerts, "7d", now).map(({ id }) => id), [
    "recent",
    "week",
  ]);
  assert.equal(filterAlertsForRange(alerts, "30d", now).length, 3);
});
