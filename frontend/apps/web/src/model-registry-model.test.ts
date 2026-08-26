import assert from "node:assert/strict";
import test from "node:test";

import {
  modelRegistryCatalog,
  REGISTRY_CATALOG_SCHEMA_VERSION,
  SYNTHETIC_READ_ONLY_CATALOG_MODE,
  filterRegistryCapabilities,
  registryCapabilities,
  validateRegistryCatalog,
} from "./model-registry-model.ts";

test("the synthetic registry contains every planned capability and no eligible release", () => {
  assert.equal(modelRegistryCatalog.schemaVersion, REGISTRY_CATALOG_SCHEMA_VERSION);
  assert.equal(modelRegistryCatalog.mode, SYNTHETIC_READ_ONLY_CATALOG_MODE);
  assert.equal(registryCapabilities.length, 24);
  assert.equal(new Set(registryCapabilities.map(({ id }) => id)).size, 24);
  assert.ok(registryCapabilities.every(({ status }) => status === "blocked"));
});

test("registry catalog rejects live, incomplete, or releasable planning data", () => {
  assert.throws(() =>
    validateRegistryCatalog({ ...modelRegistryCatalog, mode: "live" as never }),
  );
  assert.throws(() =>
    validateRegistryCatalog({
      ...modelRegistryCatalog,
      capabilities: modelRegistryCatalog.capabilities.slice(1),
    }),
  );
  assert.throws(() =>
    validateRegistryCatalog({
      ...modelRegistryCatalog,
      capabilities: [
        { ...modelRegistryCatalog.capabilities[0]!, status: "planned" as const },
        ...modelRegistryCatalog.capabilities.slice(1),
      ],
    }),
  );
});

test("registry search matches capability metadata without inventing a result", () => {
  assert.deepEqual(
    filterRegistryCapabilities(registryCapabilities, "camera health").map(({ id }) => id),
    ["camera_health"],
  );
  assert.deepEqual(filterRegistryCapabilities(registryCapabilities, "unknown"), []);
});
