import assert from "node:assert/strict";
import test from "node:test";

import { removeZoneQuestion } from "./zones-model.ts";

test("removing a zone asks first and offers pausing instead", () => {
  const question = removeZoneQuestion("Desk area");
  assert.match(question, /^Remove "Desk area"\?/);
  assert.match(question, /Past alerts stay/);
  assert.match(question, /pause it/);
  assert.match(removeZoneQuestion("  "), /^Remove "this zone"\?/);
});
