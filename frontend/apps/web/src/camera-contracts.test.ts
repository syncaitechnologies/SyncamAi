import assert from "node:assert/strict";
import test from "node:test";

import { parseCameraListEnvelope } from "./camera-contracts.ts";

const camera = {
  id: "55555555-5555-4555-8555-555555555555",
  tenant_id: "11111111-1111-4111-8111-111111111111",
  site_id: "33333333-3333-4333-8333-333333333333",
  serial_number: "SN-01",
  name: "Front gate",
  group_name: "Perimeter",
  tags: ["gate"],
  lifecycle_status: "active",
  config_version: 2,
  created_at: "2026-08-13T12:00:00Z",
  updated_at: "2026-08-13T12:00:00Z",
};

test("camera list parser accepts the metadata-only v1 contract", () => {
  assert.deepEqual(parseCameraListEnvelope({ data: [camera], meta: { count: 1, next: null } }), [camera]);
});

test("camera list parser rejects invalid lifecycle, tags, and versions", () => {
  for (const invalid of [
    { ...camera, lifecycle_status: "recording" },
    { ...camera, tags: [""] },
    { ...camera, config_version: 0 },
  ]) {
    assert.throws(() => parseCameraListEnvelope({ data: [invalid] }), /v1 contract/);
  }
  assert.throws(() => parseCameraListEnvelope({ data: {} }), /v1 contract/);
});
