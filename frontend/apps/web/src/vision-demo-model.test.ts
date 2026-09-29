import assert from "node:assert/strict";
import test from "node:test";
import { defaultDemoSettings, demoOutcome, initialDemoState, initialDemoView, transitionDemo } from "./vision-demo-model.ts";

test("live mode cannot be enabled or supplied simulated outcomes by the demo URL or controls", () => {
  const settings = defaultDemoSettings("weapon");
  assert.equal(initialDemoView("?demo=vision", "live"), "dashboard");
  assert.equal(initialDemoView("?demo=vision", "demo"), "vision");
  assert.equal(initialDemoView("?demo=other", "demo"), "dashboard");
  const finished = { step: 3, review: "pending" as const };
  assert.equal(demoOutcome("live", settings, finished).eligible, false);
  assert.deepEqual(transitionDemo("live", settings, finished, { type: "review", decision: "acknowledged" }), initialDemoState());
  assert.deepEqual(transitionDemo("live", settings, finished, { type: "advance" }), initialDemoState());
});

test("weapon walkthrough requires three observations, respects thresholds and excludes tools", () => {
  const settings = defaultDemoSettings("weapon");
  let state = initialDemoState();
  for (let observation = 1; observation <= 3; observation++) {
    state = transitionDemo("demo", settings, state, { type: "advance" });
    assert.equal(demoOutcome("demo", settings, state).eligible, observation === 3);
  }
  assert.deepEqual(transitionDemo("demo", settings, state, { type: "advance" }), state);
  assert.equal(demoOutcome("demo", { ...settings, threshold: 0.9 }, state).eligible, false);
  assert.equal(demoOutcome("demo", { ...settings, scenario: "tool", threshold: 0.5 }, state).eligible, false);
  assert.equal(demoOutcome("demo", { ...settings, scenario: "uncertain" }, state).eligible, false);
  assert.equal(demoOutcome("demo", { ...settings, scenario: "uncertain", threshold: 0.5 }, state).eligible, true);
});

test("fictional attendance remains blocked with absent opt-in, absent/failed liveness or an unknown subject", () => {
  const settings = defaultDemoSettings("face");
  const state = { step: 3, review: "pending" as const };
  assert.equal(demoOutcome("demo", settings, state).eligible, false);
  assert.equal(demoOutcome("demo", { ...settings, consent: true }, state).eligible, false);
  assert.equal(demoOutcome("demo", { ...settings, liveness: true }, state).eligible, false);
  const simulatedChecks = { ...settings, consent: true, liveness: true };
  assert.equal(demoOutcome("demo", simulatedChecks, state).eligible, true);
  for (const scenario of ["unknown", "spoof"]) {
    assert.equal(demoOutcome("demo", { ...simulatedChecks, scenario }, state).eligible, false);
  }
});

test("empty and outside-zone scenes never create reviews; reset removes review decisions", () => {
  const settings = defaultDemoSettings("person");
  const finished = { step: 3, review: "pending" as const };
  for (const scenario of ["empty", "outside"]) {
    const alternative = { ...settings, scenario };
    assert.equal(demoOutcome("demo", alternative, finished).eligible, false);
    assert.deepEqual(transitionDemo("demo", alternative, finished, { type: "review", decision: "acknowledged" }), finished);
  }
  const reviewed = transitionDemo("demo", settings, finished, { type: "review", decision: "acknowledged" });
  assert.equal(reviewed.review, "acknowledged");
  assert.equal(finished.review, "pending");
  assert.deepEqual(transitionDemo("demo", settings, reviewed, { type: "review", decision: "dismissed" }), reviewed);
  assert.deepEqual(transitionDemo("demo", settings, reviewed, { type: "reset" }), initialDemoState());
  assert.equal(demoOutcome("demo", settings, initialDemoState()).eligible, false);
});

test("unsupported scenarios, invalid steps and unbounded thresholds fail closed", () => {
  const settings = defaultDemoSettings("weapon");
  for (const threshold of [NaN, Infinity, 0.49, 0.91]) {
    assert.throws(() => demoOutcome("demo", { ...settings, threshold }, initialDemoState()));
  }
  assert.throws(() => demoOutcome("demo", { ...settings, scenario: "unknown-class" }, initialDemoState()));
  for (const step of [-1, 4, 1.5, NaN]) {
    assert.throws(() => demoOutcome("demo", settings, { step, review: "pending" }));
  }
});
