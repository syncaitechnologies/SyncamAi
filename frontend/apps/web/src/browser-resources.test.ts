import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("application styling does not contact an external font provider", () => {
  const css = readFileSync(new URL("./styles.css", import.meta.url), "utf8");
  const html = readFileSync(new URL("../index.html", import.meta.url), "utf8");
  assert.doesNotMatch(css, /@import\s+(?:url\(\s*)?["']?(?:https?:)?\/\//i);
  assert.doesNotMatch(css, /url\(\s*["']?(?:https?:)?\/\//i);
  assert.doesNotMatch(html, /fonts\.(?:googleapis|gstatic)\.com/i);
});
