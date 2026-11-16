import assert from "node:assert/strict";
import test from "node:test";

import { adoptPayload, channelChoices, deviceTitle, isPrivateIPv4, manualDevice, validChoices } from "./camera-discovery-model.ts";

test("only private LAN addresses can be entered by hand", () => {
  for (const ip of ["192.168.1.108", "10.0.0.5", "172.16.4.2", "172.31.255.1"]) assert.equal(isPrivateIPv4(ip), true, ip);
  for (const ip of ["8.8.8.8", "172.32.0.1", "127.0.0.1", "192.168.1", "192.168.1.300", "lorex.local", ""]) assert.equal(isPrivateIPv4(ip), false, ip);
});

test("manual devices and titles", () => {
  const device = manualDevice(" 192.168.1.108 ", 554);
  assert.equal(device.device_id, "192.168.1.108:554");
  assert.equal(device.family, null);
  assert.equal(deviceTitle({ ...device, name: "LOREX", model: "D871A8B" }), "LOREX D871A8B · 192.168.1.108");
  assert.match(deviceTitle(device), /Unknown brand/);
});

test("channels with a picture start selected and are named", () => {
  const choices = channelChoices({
    device_id: "192.168.1.108:554",
    family: "dahua",
    brand: "Dahua / Lorex",
    error: "",
    channels: [{ channel: 1, thumbnail: "AAA" }, { channel: 2 }, { channel: 3, thumbnail: "BBB" }],
  });
  assert.deepEqual(choices.map(({ channel, selected, name }) => ({ channel, selected, name })), [
    { channel: 1, selected: true, name: "Camera 1" },
    { channel: 2, selected: false, name: "Camera 2" },
    { channel: 3, selected: true, name: "Camera 3" },
  ]);
  choices[2]!.name = "  ";
  assert.deepEqual(validChoices(choices).map(({ channel }) => channel), [1], "unnamed channels are not added");
});

test("protection defaults follow what the site box saw", () => {
  const choices = channelChoices({
    device_id: "192.168.1.108:554",
    family: "dahua",
    brand: "Dahua / Lorex",
    error: "",
    channels: [{ channel: 1, thumbnail: "AAA", vehicles: 3 }, { channel: 2, thumbnail: "BBB", vehicles: 0 }, { channel: 3, thumbnail: "CCC" }, { channel: 4 }],
  });
  assert.deepEqual(choices.map(({ protection }) => protection), ["car-park", "night-intrusion", "night-intrusion", "none"]);
  assert.equal(choices[0]!.vehicles, 3);
});

test("adopt payload binds created cameras to channels", () => {
  assert.deepEqual(adoptPayload("192.168.1.108:554", [{ camera_id: "c1", channel: 1 }]), {
    device_id: "192.168.1.108:554",
    cameras: [{ camera_id: "c1", channel: 1 }],
  });
});
