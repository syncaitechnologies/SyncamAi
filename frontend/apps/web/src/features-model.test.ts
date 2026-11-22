import assert from "node:assert/strict";
import test from "node:test";

import {
  activityLabel,
  askToAddSubject,
  cameraSummary,
  canSwitch,
  cardState,
  featureError,
  groupFeatures,
  headline,
  offConfirmation,
  optimistic,
  toggleCamera,
  type Feature,
} from "./features-model.ts";

function feature(overrides: Partial<Feature>): Feature {
  return {
    key: "zone_alerts", status: "available", per_camera: true, included: true, enabled: true, on: true,
    cameras: null, box_ready: null, activity_30d: 0, updated_at: null, ...overrides,
  };
}

test("card states follow plan, switch and box", () => {
  assert.equal(cardState(feature({})), "on");
  assert.equal(cardState(feature({ enabled: false, on: false })), "off");
  assert.equal(cardState(feature({ key: "weapon", status: "preview", included: false, on: false })), "not_in_plan");
  assert.equal(cardState(feature({ box_ready: false })), "needs_box");
  assert.equal(cardState(feature({ box_ready: null })), "on", "an older box that does not report is not blamed");
  assert.equal(cardState(feature({ key: "camera_health", status: "always_on" })), "always_on");
  assert.equal(cardState(feature({ key: "fall", status: "coming_soon", included: false })), "coming_soon");
});

test("only included, switchable features have a switch", () => {
  assert.equal(canSwitch(feature({})), true);
  assert.equal(canSwitch(feature({ status: "preview" })), true);
  assert.equal(canSwitch(feature({ included: false })), false);
  assert.equal(canSwitch(feature({ status: "always_on" })), false);
  assert.equal(canSwitch(feature({ status: "coming_soon" })), false);
});

test("the value line shows what each feature caught", () => {
  assert.equal(activityLabel(feature({ activity_30d: 12 })), "Caught 12 alerts in the last 30 days");
  assert.equal(activityLabel(feature({ activity_30d: 1 })), "Caught 1 alert in the last 30 days");
  assert.equal(activityLabel(feature({ activity_30d: 0 })), "Nothing caught in the last 30 days, all quiet");
  assert.equal(activityLabel(feature({ activity_30d: 0, on: false })), null);
  assert.equal(activityLabel(feature({ key: "attendance", activity_30d: 1520 })), "1,520 sightings in the last 30 days");
  assert.equal(activityLabel(feature({ key: "live_view", per_camera: false, activity_30d: null, live_view: { limit_minutes: 10, used_seconds: 190 } })), "4 of 10 min used this month");
  assert.equal(activityLabel(feature({ key: "live_view", per_camera: false, activity_30d: null, live_view: { limit_minutes: null, used_seconds: 0 } })), "0 min watched this month · unlimited");
  assert.equal(activityLabel(feature({ key: "fall", status: "coming_soon", activity_30d: null })), null);
});

test("camera choice", () => {
  const cameras = ["a", "b", "c"];
  assert.equal(cameraSummary(feature({}), cameras), "All cameras");
  assert.equal(cameraSummary(feature({ cameras: ["a"] }), cameras), "1 of 3 cameras");
  assert.equal(cameraSummary(feature({ cameras: [] }), cameras), "No cameras chosen");
  assert.equal(cameraSummary(feature({ per_camera: false, cameras: ["a"] }), cameras), "All cameras");
  assert.deepEqual(toggleCamera(null, cameras, "b"), ["a", "c"], "unticking from all keeps the others");
  assert.equal(toggleCamera(["a", "c"], cameras, "b"), null, "ticking every camera means all cameras again");
  assert.deepEqual(toggleCamera(["a"], cameras, "a"), []);
  assert.deepEqual(toggleCamera(["x", "a"], cameras, "b"), ["x", "a", "b"], "cameras at other sites are kept");
});

test("switching off a security feature asks first", () => {
  assert.match(offConfirmation(feature({})) ?? "", /^Zone alerts will stop on all cameras\./);
  assert.match(offConfirmation(feature({ cameras: ["a"] })) ?? "", /the chosen cameras/);
  assert.equal(offConfirmation(feature({ key: "attendance" })), null);
  assert.equal(offConfirmation(feature({ enabled: false, on: false })), null);
});

test("sections, headline and optimistic updates", () => {
  const list = [
    feature({}),
    feature({ key: "loitering", enabled: false, on: false }),
    feature({ key: "weapon", status: "preview", included: false, on: false }),
    feature({ key: "camera_health", status: "always_on", per_camera: false }),
    feature({ key: "fire_smoke", status: "coming_soon", included: false, on: false }),
  ];
  const groups = groupFeatures(list);
  assert.deepEqual(groups.yours.map((item) => item.key), ["zone_alerts", "loitering", "camera_health"]);
  assert.deepEqual(groups.add.map((item) => item.key), ["weapon"]);
  assert.deepEqual(groups.soon.map((item) => item.key), ["fire_smoke"]);
  assert.equal(headline(list), "2 of 3 features on");
  const off = optimistic(list[0], { enabled: false });
  assert.equal(off.on, false);
  assert.deepEqual(optimistic(list[0], { cameras: ["a"] }).cameras, ["a"]);
  assert.equal(optimistic({ ...list[0], cameras: ["a"] }, { cameras: null }).cameras, null);
});

test("messages and contact subjects", () => {
  assert.equal(askToAddSubject("weapon"), "Add Possible weapon to my SyncCam plan");
  assert.match(featureError({ code: "FEATURE_NOT_INCLUDED", status: 403 }), /not in your plan/);
  assert.match(featureError({ code: "FORBIDDEN", status: 403 }), /Only the workspace owner/);
  assert.equal(featureError(new Error("Network down")), "Network down");
});
