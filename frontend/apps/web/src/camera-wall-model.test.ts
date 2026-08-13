import assert from "node:assert/strict";
import test from "node:test";

import type { AlertItem } from "./alert-contracts.ts";
import {
  buildCameraWallTiles,
  filterCameraWallTiles,
  groupCameraWallAlerts,
  wallColumnCount,
} from "./camera-wall-model.ts";

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
    camera: "CAM-07 · Dock west",
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

test("camera wall never creates synthetic cameras in live mode", () => {
  assert.deepEqual(buildCameraWallTiles([alert({})], "live"), []);
});

test("camera wall attaches the highest-priority active alert", () => {
  const tiles = buildCameraWallTiles(
    [
      alert({ id: "high", severity: "High" }),
      alert({ id: "critical", severity: "Critical" }),
      alert({ id: "resolved", severity: "Critical", status: "resolved" }),
    ],
    "demo",
  );
  const dock = tiles.find(({ id }) => id === "CAM-07");

  assert.equal(dock?.alert?.id, "critical");
  assert.equal(dock?.status, "recording");
  assert.equal(dock?.synthetic, true);
});

test("camera wall filters by event, health, and searchable metadata", () => {
  const tiles = buildCameraWallTiles([alert({})], "demo");

  assert.deepEqual(
    filterCameraWallTiles(tiles, "", "active").map(({ id }) => id),
    ["CAM-07"],
  );
  assert.deepEqual(
    filterCameraWallTiles(tiles, "", "offline").map(({ id }) => id),
    ["CAM-11"],
  );
  assert.deepEqual(
    filterCameraWallTiles(tiles, "privacy", "all").map(({ id }) => id),
    ["CAM-09"],
  );
});

test("camera wall ordering puts active events before health-only tiles", () => {
  const tiles = buildCameraWallTiles(
    [alert({ camera: "CAM-03 · Fabrication", severity: "Critical" })],
    "demo",
  );

  assert.equal(filterCameraWallTiles(tiles, "", "all")[0]?.id, "CAM-03");
  assert.equal(wallColumnCount(1), 1);
  assert.equal(wallColumnCount(9), 3);
  assert.equal(wallColumnCount(25), 5);
});

test("camera wall groups repeated camera and event-type alerts for flood control", () => {
  const grouped = groupCameraWallAlerts([
    alert({ id: "old", occurredAt: "2026-08-13T13:00:00Z" }),
    alert({ id: "new", occurredAt: "2026-08-13T14:00:00Z" }),
    alert({ id: "critical", type: "Fall detection", severity: "Critical" }),
    alert({ id: "inactive", status: "dismissed" }),
  ]);

  assert.deepEqual(grouped.map(({ id }) => id), ["critical", "new"]);
});
