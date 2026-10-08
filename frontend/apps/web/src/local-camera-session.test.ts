import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { bindCameraLifecycle, cameraErrorMessage, initialCameraState, initialLocalCameraView, LocalCameraSession } from "./local-camera-session.ts";
import type { CameraState } from "./local-camera-session.ts";

class FakeTrack extends EventTarget {
  readyState: "live" | "ended" = "live";
  stops = 0;
  stop() { ++this.stops; this.readyState = "ended"; }
  end() { this.readyState = "ended"; this.dispatchEvent(new Event("ended")); }
}
class FakeStream {
  readonly tracks: FakeTrack[];
  constructor(tracks: FakeTrack[] = [new FakeTrack()]) { this.tracks = tracks; }
  getTracks() { return this.tracks; }
  getVideoTracks() { return this.tracks; }
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}
function harness(options: { authorized?: boolean; secure?: boolean; supported?: boolean; visible?: boolean; request?: (constraints: MediaStreamConstraints) => Promise<FakeStream>; play?: () => Promise<void> } = {}) {
  const stream = new FakeStream();
  const states: CameraState[] = [];
  const constraints: MediaStreamConstraints[] = [];
  const attachments: (FakeStream | null)[] = [];
  const visibility = { visible: options.visible ?? true };
  const session = new LocalCameraSession<FakeStream>({
    secure: options.secure ?? true, supported: options.supported ?? true,
    visible: () => visibility.visible,
    request: (value) => { constraints.push(value); return options.request?.(value) ?? Promise.resolve(stream); },
    attach: (value) => attachments.push(value), play: options.play ?? (() => Promise.resolve()),
    onState: (value) => states.push(value),
  });
  return { session, stream, states, constraints, attachments, visibility };
}

test("camera state and direct entry are independent from scripted detections and cannot bypass live auth", () => {
  assert.equal(initialCameraState().phase, "idle");
  assert.match(initialCameraState().message, /Models are not connected/);
  assert.equal(initialLocalCameraView("?camera=local", "demo"), "local-camera");
  for (const search of ["", "?camera=other", "?demo=vision"]) assert.equal(initialLocalCameraView(search, "demo"), null);
  for (const search of ["?camera=local", "?camera=local&demo=vision"]) assert.equal(initialLocalCameraView(search, "live"), null);
});

test("unauthorized, insecure, unsupported and hidden starts do not request a camera", async () => {
  for (const options of [{ authorized: false }, { secure: false }, { supported: false }, { visible: false }]) {
    const h = harness(options);
    await h.session.start("environment", options.authorized ?? true);
    assert.equal(h.constraints.length, 0);
    assert.equal(h.states.at(-1)?.phase, "error");
    assert.equal(h.attachments.at(-1), null);
  }
});

test("actual media acquisition requests video only and reports connected only after playback", async () => {
  const h = harness();
  await h.session.start("environment", true);
  assert.deepEqual(h.constraints, [{ audio: false, video: { facingMode: { ideal: "environment" }, width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 15, max: 30 } } }]);
  assert.deepEqual(h.states.map((state) => state.phase), ["requesting", "starting", "live"]);
  assert.equal(h.attachments.at(-1), h.stream);
  assert.match(h.states.at(-1)!.message, /models are not connected/);
  h.session.stop();
  assert.equal(h.stream.tracks[0]!.stops, 1);
  assert.equal(h.attachments.at(-1), null);
  assert.equal(h.states.at(-1)?.phase, "stopped");
});

test("front-camera replacement releases the prior camera before acquisition", async () => {
  const first = new FakeStream(); const second = new FakeStream(); let requests = 0;
  const h = harness({ request: async () => requests++ === 0 ? first : second });
  await h.session.start("environment", true);
  await h.session.start("user", true);
  assert.equal(first.tracks[0]!.stops, 1);
  assert.deepEqual(h.constraints[1]?.video, { facingMode: { ideal: "user" }, width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 15, max: 30 } });
  first.tracks[0]!.end();
  assert.equal(h.attachments.at(-1), second);
  assert.equal(second.tracks[0]!.stops, 0);
  h.session.dispose();
});

test("permission/device/playback errors are bounded and never expose raw vendor details", async () => {
  for (const name of ["NotAllowedError", "SecurityError", "NotFoundError", "NotReadableError", "OverconstrainedError", "AbortError"]) {
    const h = harness({ request: async () => { throw { name, message: "raw vendor detail" }; } });
    await h.session.start("environment", true);
    assert.equal(h.states.at(-1)?.phase, "error");
    assert.doesNotMatch(h.states.at(-1)!.message, /raw vendor detail/);
    assert.ok(h.states.at(-1)!.message.length < 220);
    assert.equal(h.attachments.at(-1), null);
  }
  assert.equal(cameraErrorMessage(null), cameraErrorMessage("raw vendor detail"));
  const h = harness({ play: async () => { throw new Error("raw vendor detail"); } });
  await h.session.start("environment", true);
  assert.equal(h.stream.tracks[0]!.stops, 1);
  assert.equal(h.attachments.at(-1), null);
  assert.equal(h.states.at(-1)?.phase, "error");
});

test("empty or ended video is rejected and every acquired track released", async () => {
  for (const stream of [new FakeStream([]), new FakeStream()]) {
    stream.tracks.forEach((track) => { track.readyState = "ended"; });
    const h = harness({ request: async () => stream });
    await h.session.start("environment", true);
    assert.equal(h.states.at(-1)?.phase, "error");
    assert.equal(h.attachments.at(-1), null);
    stream.tracks.forEach((track) => assert.equal(track.stops, 1));
  }
});

test("Stop invalidates a pending permission prompt and disposes its late stream", async () => {
  const request = deferred<FakeStream>(); const h = harness({ request: () => request.promise });
  const starting = h.session.start("environment", true);
  assert.equal(h.states.at(-1)?.phase, "requesting");
  h.session.stop();
  const late = new FakeStream(); request.resolve(late); await starting;
  assert.equal(late.tracks[0]!.stops, 1);
  assert.equal(h.states.at(-1)?.phase, "stopped");
  assert.ok(!h.attachments.includes(late));
});

test("late old acquisition cannot replace a newer camera", async () => {
  const oldRequest = deferred<FakeStream>(); const newer = new FakeStream(); let calls = 0;
  const h = harness({ request: () => calls++ === 0 ? oldRequest.promise : Promise.resolve(newer) });
  const oldStart = h.session.start("environment", true);
  await h.session.start("user", true);
  const oldStream = new FakeStream(); oldRequest.resolve(oldStream); await oldStart;
  assert.equal(oldStream.tracks[0]!.stops, 1);
  assert.equal(newer.tracks[0]!.stops, 0);
  assert.equal(h.attachments.at(-1), newer);
  assert.equal(h.states.at(-1)?.phase, "live");
  h.session.dispose();
});

test("late rejected acquisition cannot downgrade a newer live camera", async () => {
  const oldRequest = deferred<FakeStream>(); let calls = 0;
  const h = harness({ request: () => calls++ === 0 ? oldRequest.promise : Promise.resolve(new FakeStream()) });
  const oldStart = h.session.start("environment", true); await h.session.start("user", true);
  oldRequest.reject(new Error("old error")); await oldStart;
  assert.equal(h.states.at(-1)?.phase, "live"); h.session.dispose();
});

test("Stop or disposal during playback never publishes a later connected state", async () => {
  for (const dispose of [false, true]) {
    const playback = deferred<void>(); const h = harness({ play: () => playback.promise });
    const start = h.session.start("environment", true); await Promise.resolve();
    assert.equal(h.states.at(-1)?.phase, "starting");
    if (dispose) h.session.dispose(); else h.session.stop();
    const count = h.states.length; playback.resolve(); await start;
    assert.equal(h.states.length, count);
    assert.equal(h.stream.tracks[0]!.stops, 1);
    assert.equal(h.attachments.at(-1), null);
  }
});

test("disposal blocks late grants and any later Start, Stop or ended callbacks", async () => {
  const request = deferred<FakeStream>(); const h = harness({ request: () => request.promise });
  const start = h.session.start("environment", true); h.session.dispose();
  const count = h.states.length; const late = new FakeStream(); request.resolve(late); await start;
  await h.session.start("user", true); h.session.stop(); h.session.dispose(); late.tracks[0]!.end();
  assert.equal(h.states.length, count);
  assert.equal(h.constraints.length, 1);
  assert.equal(late.tracks[0]!.stops, 1);
});

test("camera disconnect releases every track and detaches video", async () => {
  const stream = new FakeStream([new FakeTrack(), new FakeTrack()]);
  const h = harness({ request: async () => stream }); await h.session.start("environment", true);
  stream.tracks[0]!.end();
  stream.tracks.forEach((track) => assert.equal(track.stops, 1));
  assert.equal(h.states.at(-1)?.phase, "stopped");
  assert.match(h.states.at(-1)!.message, /disconnected/);
  assert.equal(h.attachments.at(-1), null);
});

test("hidden page at acquisition or playback completion releases camera even without a visibility event", async () => {
  const request = deferred<FakeStream>(); const h = harness({ request: () => request.promise });
  const start = h.session.start("environment", true); h.visibility.visible = false;
  const stream = new FakeStream(); request.resolve(stream); await start;
  assert.equal(stream.tracks[0]!.stops, 1);
  assert.equal(h.states.at(-1)?.phase, "stopped");
  const playback = deferred<void>(); const p = harness({ play: () => playback.promise });
  const playing = p.session.start("environment", true); await Promise.resolve();
  p.visibility.visible = false; playback.resolve(); await playing;
  assert.equal(p.stream.tracks[0]!.stops, 1);
  assert.equal(p.states.at(-1)?.phase, "stopped");
});

test("page lifecycle stops hidden and pagehide sessions, never auto-restarts and unregisters", async () => {
  class FakeDocument extends EventTarget { visibilityState: DocumentVisibilityState = "visible"; }
  const doc = new FakeDocument(); const win = new EventTarget(); const h = harness();
  const cleanup = bindCameraLifecycle(h.session, doc, win);
  await h.session.start("environment", true);
  doc.dispatchEvent(new Event("visibilitychange")); assert.equal(h.states.at(-1)?.phase, "live");
  doc.visibilityState = "hidden"; doc.dispatchEvent(new Event("visibilitychange"));
  assert.equal(h.stream.tracks[0]!.stops, 1);
  doc.visibilityState = "visible"; doc.dispatchEvent(new Event("visibilitychange"));
  assert.equal(h.states.at(-1)?.phase, "stopped"); assert.equal(h.constraints.length, 1);
  win.dispatchEvent(new Event("pagehide")); assert.match(h.states.at(-1)!.message, /left the page/);
  cleanup(); const count = h.states.length;
  doc.visibilityState = "hidden"; doc.dispatchEvent(new Event("visibilitychange")); win.dispatchEvent(new Event("pagehide"));
  assert.equal(h.states.length, count); h.session.dispose();
});

test("camera session owns capture lifecycle while the workspace keeps inference opt-in and non-persistent", () => {
  const source = readFileSync(new URL("./LocalCameraWorkspace.tsx", import.meta.url), "utf8") + readFileSync(new URL("./local-camera-session.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, /\b(?:XMLHttpRequest|WebSocket|MediaRecorder|localStorage|sessionStorage|indexedDB|toDataURL|toBlob|face recognition|weapon detection)\b/i);
  assert.match(source, /muted playsInline autoPlay/);
  assert.match(source, /Enable person detection/);
  assert.doesNotMatch(source, /demoFeatures|advanceDemo|DemoState/);
  const app = readFileSync(new URL("./App.tsx", import.meta.url), "utf8");
  assert.match(app, /initialLocalCameraView\(window.location.search, dataMode\)/);
  assert.match(app, /activeView === "local-camera"[\s\S]*?<LocalCameraWorkspace/);
});
