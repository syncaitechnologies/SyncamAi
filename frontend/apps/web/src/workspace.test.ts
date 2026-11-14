import assert from "node:assert/strict";
import test from "node:test";
import { canManage, readWorkspace, storeWorkspace } from "./workspace.ts";

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
    removeItem: (key: string) => void values.delete(key),
  };
}

test("stores and clears the signed-in workspace", () => {
  const storage = memoryStorage();
  assert.equal(readWorkspace(storage), null);
  storeWorkspace({ tenantId: "t1", siteIds: ["s1"], roles: ["operator"] }, storage);
  assert.deepEqual(readWorkspace(storage), { tenantId: "t1", siteIds: ["s1"], roles: ["operator"] });
  storeWorkspace(null, storage);
  assert.equal(readWorkspace(storage), null);
});

test("ignores corrupt workspace storage", () => {
  const storage = memoryStorage();
  storage.setItem("syncam.workspace", "{not json");
  assert.equal(readWorkspace(storage), null);
  storage.setItem("syncam.workspace", JSON.stringify({ tenantId: 5 }));
  assert.equal(readWorkspace(storage), null);
});

test("only administrators manage sites, cameras and zones", () => {
  assert.equal(canManage({ tenantId: "t", siteIds: [], roles: ["super_admin"] }), true);
  assert.equal(canManage({ tenantId: "t", siteIds: ["s"], roles: ["site_admin"] }), true);
  assert.equal(canManage({ tenantId: "t", siteIds: ["s"], roles: ["operator"] }), false);
  assert.equal(canManage(null), false);
});
