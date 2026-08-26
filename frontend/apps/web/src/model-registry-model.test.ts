import assert from "node:assert/strict";
import test from "node:test";

import {
  filterRegistryCapabilities,
  registryCapabilities,
} from "./model-registry-model.ts";

test("the synthetic registry contains every planned capability and no eligible release", () => {
  assert.equal(registryCapabilities.length, 24);
  assert.equal(new Set(registryCapabilities.map(({ id }) => id)).size, 24);
  assert.ok(registryCapabilities.every(({ status }) => status === "blocked"));
});

test("registry search matches capability metadata without inventing a result", () => {
  assert.deepEqual(
    filterRegistryCapabilities(registryCapabilities, "camera health").map(({ id }) => id),
    ["camera_health"],
  );
  assert.deepEqual(filterRegistryCapabilities(registryCapabilities, "unknown"), []);
});
