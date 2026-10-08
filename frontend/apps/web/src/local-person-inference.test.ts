import assert from "node:assert/strict";
import test from "node:test";
import { localPersonInferenceEndpoint, parseLocalPersonResponse } from "./local-person-inference.ts";

test("local person inference requires a clean HTTPS endpoint", () => {
  assert.equal(localPersonInferenceEndpoint(undefined), null);
  assert.equal(localPersonInferenceEndpoint("http://127.0.0.1:8443"), null);
  assert.equal(localPersonInferenceEndpoint("https://user@example.test"), null);
  assert.equal(localPersonInferenceEndpoint("https://example.test/path"), null);
  assert.equal(
    localPersonInferenceEndpoint("https://inference.example.test:8443/")?.href,
    "https://inference.example.test:8443/local-dev/v1/person-detections",
  );
});

test("local person responses are bounded person boxes only", () => {
  assert.deepEqual(
    parseLocalPersonResponse({ detections: [{ confidence: 0.9, left: 1, top: 2, right: 3, bottom: 4 }] }, 10, 10),
    [{ confidence: 0.9, left: 1, top: 2, right: 3, bottom: 4 }],
  );
  for (const payload of [
    {},
    { detections: [{ confidence: 1.2, left: 1, top: 2, right: 3, bottom: 4 }] },
    { detections: [{ confidence: 0.8, left: 1.5, top: 2, right: 3, bottom: 4 }] },
    { detections: [{ confidence: 0.8, left: 3, top: 2, right: 3, bottom: 4 }] },
    { detections: [{ confidence: 0.8, left: 1, top: 2, right: 30, bottom: 4 }] },
  ]) assert.throws(() => parseLocalPersonResponse(payload, 10, 10));
});
