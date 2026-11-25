import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// React refuses to start (blank page) when react and react-dom differ by even
// a patch version, and the other tests never load react-dom in a browser.
test("react and react-dom are the exact same version", () => {
  const manifest = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
  const deps = { ...manifest.dependencies, ...manifest.devDependencies };
  assert.equal(deps.react, deps["react-dom"], "package.json pins different react and react-dom versions");
  const installed = (name: string) => JSON.parse(readFileSync(new URL(`../node_modules/${name}/package.json`, import.meta.url), "utf8")).version;
  assert.equal(installed("react"), installed("react-dom"), "the installed react and react-dom differ");
});
