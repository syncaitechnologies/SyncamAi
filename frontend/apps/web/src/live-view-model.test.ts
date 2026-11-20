import assert from "node:assert/strict";
import test from "node:test";

import {
  canStartLive,
  contactHref,
  errorCode,
  frameProblem,
  isLimitError,
  limitMessage,
  minutesLeft,
  minutesLeftLabel,
  minutesUsed,
  resetLabel,
  secondsLeft,
  stillAgeLabel,
  usageLabel,
  type LiveViewUsage,
} from "./live-view-model.ts";

const usage = (limit_minutes: number | null, used_seconds: number): LiveViewUsage => ({
  limit_minutes,
  used_seconds,
  month: "2026-11-01",
  resets_at: "2026-12-01T00:00:00Z",
});

test("usage label covers metered, unlimited and off workspaces", () => {
  assert.equal(usageLabel(usage(10, 180)), "3 of 10 min used this month");
  assert.equal(usageLabel(usage(10, 0)), "0 of 10 min used this month");
  assert.equal(usageLabel(usage(null, 9_999)), "Unlimited live view");
  assert.equal(usageLabel(usage(0, 0)), "Live view is off for this workspace");
});

test("used rounds up and left rounds down so they add up to the limit", () => {
  const partial = usage(10, 210); // 3.5 minutes
  assert.equal(minutesUsed(partial), 4);
  assert.equal(minutesLeft(partial), 6);
  assert.equal(minutesUsed(usage(10, 900)), 10, "never shows more used than the limit");
});

test("minutes left is null when unlimited and never negative", () => {
  assert.equal(minutesLeft(usage(null, 100)), null);
  assert.equal(secondsLeft(usage(null, 100)), null);
  assert.equal(minutesLeft(usage(10, 0)), 10);
  assert.equal(minutesLeft(usage(10, 1_000)), 0);
  assert.equal(secondsLeft(usage(10, 1_000)), 0);
  assert.equal(minutesLeft(usage(0, 0)), 0);
});

test("minutes left label next to the LIVE chip", () => {
  assert.equal(minutesLeftLabel(usage(10, 180)), "7 min left");
  assert.equal(minutesLeftLabel(usage(10, 570)), "under 1 min left");
  assert.equal(minutesLeftLabel(usage(10, 600)), "no minutes left");
  assert.equal(minutesLeftLabel(usage(null, 0)), "unlimited");
});

test("a session can start unless the allowance is used up or off", () => {
  assert.equal(canStartLive(null), true, "unknown usage lets the server decide");
  assert.equal(canStartLive(usage(null, 1e9)), true);
  assert.equal(canStartLive(usage(10, 599)), true);
  assert.equal(canStartLive(usage(10, 600)), false);
  assert.equal(canStartLive(usage(0, 0)), false);
});

test("reset date is the UTC month boundary", () => {
  assert.equal(resetLabel(usage(10, 0)), "Resets 1 Dec");
  assert.equal(resetLabel({ ...usage(10, 0), resets_at: "not a date" }), "");
});

test("frame errors map to feed states by code, not just status", () => {
  assert.equal(frameProblem(409, "LIVE_VIEW_PAUSED"), "paused");
  assert.equal(frameProblem(403, "LIVE_VIEW_LIMIT"), "limit");
  assert.equal(frameProblem(409, "LIVE_VIEW_LIMIT"), "limit");
  assert.equal(frameProblem(409, "LIVE_SESSION_REQUIRED"), "expired");
  assert.equal(frameProblem(409, undefined), "expired", "an old server's bare 409 still restarts");
  assert.equal(frameProblem(404, "LIVE_FRAME_PENDING"), "pending");
  assert.equal(frameProblem(403, "FORBIDDEN"), null, "other 403s are real errors");
  assert.equal(frameProblem(500, undefined), null);
});

test("error codes are read from the v1 error body", () => {
  assert.equal(errorCode({ error: { code: "LIVE_VIEW_LIMIT", message: "x" } }), "LIVE_VIEW_LIMIT");
  assert.equal(errorCode({ error: { message: "x" } }), undefined);
  assert.equal(errorCode(null), undefined);
  assert.equal(errorCode("LIVE_VIEW_LIMIT"), undefined);
  assert.equal(isLimitError({ code: "LIVE_VIEW_LIMIT" }), true);
  assert.equal(isLimitError(new Error("LIVE_VIEW_LIMIT")), false);
  assert.equal(isLimitError(null), false);
});

test("limit message says off when the workspace has no live view", () => {
  assert.equal(limitMessage(usage(0, 0)), "Live view is off for this workspace");
  assert.equal(limitMessage(usage(10, 600)), "Live view minutes for this month are used up");
  assert.equal(limitMessage(null), "Live view minutes for this month are used up");
});

test("contact link only for a plausible address", () => {
  assert.equal(contactHref("sales@example.com", "More minutes"), "mailto:sales@example.com?subject=More%20minutes");
  assert.equal(contactHref("", "x"), null);
  assert.equal(contactHref(undefined, "x"), null);
  assert.equal(contactHref("not-an-email", "x"), null);
});

test("still age uses the capture time when the server gives one", () => {
  const now = Date.parse("2026-11-20T12:00:00Z");
  assert.equal(stillAgeLabel("2026-11-20T11:59:30Z", now), "Still · under 1 min ago");
  assert.equal(stillAgeLabel("2026-11-20T11:57:00Z", now), "Still · 3 min ago");
  assert.equal(stillAgeLabel("Fri, 20 Nov 2026 09:00:00 GMT", now), "Still · 3 h ago");
  assert.equal(stillAgeLabel("2026-11-15T12:00:00Z", now), "Still · 5 days ago");
  assert.equal(stillAgeLabel(null, now), "Still · refreshed about every 5 min");
  assert.equal(stillAgeLabel("garbage", now), "Still · refreshed about every 5 min");
});
