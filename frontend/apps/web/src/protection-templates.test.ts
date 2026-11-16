import assert from "node:assert/strict";
import test from "node:test";

import { allDays, hasTemplate, protectionTemplates, suggestTemplate, templateById, templateZone, wholeView } from "./protection-templates.ts";
import { vehicleClasses } from "./subject-classes.ts";
import type { Zone } from "./syncam-api.ts";

const target = { siteId: "site-1", cameraId: "cam-1", timezone: "Asia/Kolkata" };

test("night intrusion covers the whole view for people, 22:00-06:00 every day", () => {
  const zone = templateZone(templateById("night-intrusion")!, target);
  assert.equal(zone.kind, "intrusion");
  assert.deepEqual(zone.subject_classes, ["person"]);
  assert.deepEqual(zone.schedule, { timezone: "Asia/Kolkata", windows: [{ days: allDays, start: "22:00", end: "06:00" }] });
  assert.equal("loiter_seconds" in zone, false);
  assert.equal(zone.name, "Night intrusion (whole view)");
  assert.deepEqual([zone.site_id, zone.camera_id], ["site-1", "cam-1"]);
});

test("car park watch is loitering near vehicles for 90 s overnight", () => {
  const zone = templateZone(templateById("car-park")!, target);
  assert.equal(zone.kind, "loitering");
  assert.deepEqual(zone.subject_classes, ["person", ...vehicleClasses]);
  assert.equal(zone.loiter_seconds, 90);
  assert.deepEqual(zone.schedule.windows[0], { days: allDays, start: "20:00", end: "07:00" });
});

test("the whole-view polygon is closed and inside the picture", () => {
  const geometry = wholeView();
  const ring = (geometry.coordinates as number[][][])[0]!;
  assert.deepEqual(ring[0], ring[ring.length - 1]);
  assert.ok(ring.flat().every((value) => value > 0 && value < 1));
});

test("an existing template zone on the same camera is recognised", () => {
  const template = protectionTemplates[0]!;
  const zone = { id: "z1", site_id: "site-1", camera_id: "cam-1", name: template.zoneName, kind: "intrusion", geometry: wholeView(), enabled: true, config_version: 1 } satisfies Zone;
  assert.equal(hasTemplate([zone], "cam-1", template), true);
  assert.equal(hasTemplate([zone], "cam-2", template), false);
  assert.equal(hasTemplate([zone], "cam-1", protectionTemplates[1]!), false);
});

test("vehicles in the picture suggest car park watch", () => {
  assert.equal(suggestTemplate({ vehicles: 2 }), "car-park");
  assert.equal(suggestTemplate({ vehicles: 0 }), "night-intrusion");
  assert.equal(suggestTemplate({}), "night-intrusion");
  assert.equal(templateById("nope"), undefined);
});
