import assert from "node:assert/strict";
import test from "node:test";
import { canRestart, restartFailure } from "./site-box-model.ts";

test("older boxes get a clear explanation instead of a raw error", () => {
  assert.match(restartFailure("unknown setup task 'restart'"), /older version/);
  assert.match(restartFailure("The site box is taking too long. Try again."), /older version/);
});

test("offline and server messages are passed on in plain words", () => {
  assert.match(restartFailure("The site box did not pick this up. Check that it is online."), /on, awake and connected/);
  const offline = "The site box is offline, so it cannot be restarted from here. Check that the computer is switched on, awake and connected to the internet.";
  assert.equal(restartFailure(offline), offline);
  assert.match(restartFailure(""), /could not be restarted/);
  assert.match(restartFailure(undefined), /could not be restarted/);
});

test("only managers see the button, and only while the box is online", () => {
  assert.equal(canRestart(true, true), true);
  assert.equal(canRestart(true, false), false);
  assert.equal(canRestart(false, true), false);
});
