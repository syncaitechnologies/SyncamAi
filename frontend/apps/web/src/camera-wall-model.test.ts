import assert from "node:assert/strict";
import test from "node:test";

import type { AlertItem } from "./alert-contracts.ts";
import {
  buildCameraWallTiles,
  buildLiveCameraWallTiles,
  filterCameraWallTiles,
  groupCameraWallAlerts,
  wallColumnCount,
} from "./camera-wall-model.ts";
import type { ApiCamera } from "./camera-contracts.ts";

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

test("camera wall maps live registry metadata without inventing footage", () => {
  const camera: ApiCamera = {
    id: "55555555-5555-4555-8555-555555555555",
    tenant_id: "11111111-1111-4111-8111-111111111111",
    site_id: "33333333-3333-4333-8333-333333333333",
    serial_number: "SN-01",
    name: "Front gate",
    group_name: "Perimeter",
    tags: ["gate", "north"],
    lifecycle_status: "active",
    config_version: 1,
    created_at: "2026-08-13T12:00:00Z",
    updated_at: "2026-08-13T12:00:00Z",
  };
  const [tile] = buildLiveCameraWallTiles(
    [camera],
    [alert({ cameraId: camera.id })],
  );

  assert.equal(tile?.synthetic, false);
  assert.equal(tile?.status, "online");
  assert.equal(tile?.alert?.id, "alert-1");
  assert.equal(tile?.location, "gate · north");
  assert.deepEqual(tile?.activity, [0, 0, 0, 0, 0, 0, 0, 0]);
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
