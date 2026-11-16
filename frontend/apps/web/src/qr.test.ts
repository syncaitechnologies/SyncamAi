import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { qrMatrix, qrSvgPath } from "./qr.ts";

const link = "https://t.me/SyncCamAlertsBot?start=K7Q4XM92";

test("encodes a Telegram link exactly as the output OpenCV decoded", () => {
  const modules = qrMatrix(link);
  assert.equal(modules.length, 33, "44 bytes need version 4 at level M");
  const fingerprint = createHash("sha256").update(modules.map((row) => row.map((dark) => (dark ? "1" : "0")).join("")).join("\n")).digest("hex");
  // Checked once with cv2.QRCodeDetector (versions 1, 4, 5, 8 and 10 all decoded).
  assert.equal(fingerprint, "df292a27c422f6cf64e022c07eeff6989a07b00c335581277fb30ce1fd91c294");
});

test("has finder patterns, timing pattern and the dark module", () => {
  const modules = qrMatrix(link);
  const size = modules.length;
  for (const [x, y] of [[0, 0], [size - 7, 0], [0, size - 7]] as const) {
    assert.ok(modules[y]![x] && modules[y + 6]![x + 6] && modules[y + 3]![x + 3], "finder corners and centre are dark");
    assert.ok(!modules[y + 1]![x + 1], "finder ring is light");
  }
  for (let i = 8; i < size - 8; i += 1) assert.equal(modules[6]![i], i % 2 === 0);
  assert.ok(modules[size - 8]![8]);
});

test("grows the version with the text and refuses what does not fit", () => {
  assert.equal(qrMatrix("hello").length, 21);
  assert.equal(qrMatrix("z".repeat(200)).length, 57);
  assert.throws(() => qrMatrix("z".repeat(300)), /too long/);
});

test("svg path draws one square per dark module inside a quiet zone", () => {
  const modules = qrMatrix("hello");
  const { path, size } = qrSvgPath(modules);
  assert.equal(size, 29);
  assert.equal(path.split("M").length - 1, modules.flat().filter(Boolean).length);
  assert.ok(path.startsWith("M4 4h1v1h-1z"));
});
