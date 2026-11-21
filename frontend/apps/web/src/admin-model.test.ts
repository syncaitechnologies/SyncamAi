import assert from "node:assert/strict";
import test from "node:test";
import { changesFrom, dayFromNow, draftFrom, liveMinutesLabel, planLabel, usageLabel, type AdminWorkspace } from "./admin-model.ts";

const trial: AdminWorkspace = {
  tenant_id: "t-1", name: "Client A", slug: "client-a", plan: "trial", trial_ends_at: "2026-11-30T12:00:00Z",
  site_limit: 1, camera_limit: 10, live_view_minutes: 10, live_view_seconds: 90, sites: 1, cameras: 3,
  created_at: "2026-11-16T12:00:00Z",
};

test("an unchanged form has nothing to save", () => {
  assert.deepEqual(changesFrom(trial, draftFrom(trial)), { error: "Nothing has changed." });
});

test("activating a client clears the trial and limits that were emptied", () => {
  const draft = { ...draftFrom(trial), plan: "active" as const, trialEndsOn: "", cameraLimit: "", liveViewMinutes: "300" };
  assert.deepEqual(changesFrom(trial, draft), { changes: { plan: "active", trial_ends_at: null, camera_limit: null, live_view_minutes: 300 } });
});

test("extending a trial sends the end of that day in UTC", () => {
  assert.deepEqual(changesFrom(trial, { ...draftFrom(trial), trialEndsOn: "2026-12-14" }), { changes: { trial_ends_at: "2026-12-14T23:59:59Z" } });
  assert.equal(dayFromNow(14, new Date("2026-11-21T10:00:00Z")), "2026-12-05");
});

test("invalid values are explained", () => {
  assert.match((changesFrom(trial, { ...draftFrom(trial), liveViewMinutes: "-5" }) as { error: string }).error, /whole number/);
  assert.match((changesFrom(trial, { ...draftFrom(trial), cameraLimit: "2.5" }) as { error: string }).error, /Camera limit/);
  assert.match((changesFrom(trial, { ...draftFrom(trial), trialEndsOn: "" }) as { error: string }).error, /trial needs an end date/);
  assert.match((changesFrom(trial, { ...draftFrom(trial), trialEndsOn: "soon" }) as { error: string }).error, /must be a date/);
});

test("labels describe plan and usage plainly", () => {
  const now = new Date("2026-11-21T12:00:00Z");
  assert.equal(planLabel(trial, now), "Trial · 9 days left");
  assert.equal(planLabel({ ...trial, trial_ends_at: "2026-11-20T00:00:00Z" }, now), "Trial ended");
  assert.equal(planLabel({ ...trial, plan: "active" }, now), "Active");
  assert.equal(usageLabel(3, 10, "cameras"), "3 of 10 cameras");
  assert.equal(usageLabel(3, null, "cameras"), "3 · unlimited");
  assert.equal(liveMinutesLabel(trial), "1.5 of 10 min");
  assert.equal(liveMinutesLabel({ ...trial, live_view_minutes: null }), "1.5 min · unlimited");
  assert.equal(liveMinutesLabel({ ...trial, live_view_minutes: 0 }), "Off");
});
