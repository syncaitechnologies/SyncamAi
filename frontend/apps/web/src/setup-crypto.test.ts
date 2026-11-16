import assert from "node:assert/strict";
import test from "node:test";

import { encryptCredentials, pemToDer } from "./setup-crypto.ts";

// Shared with ai-services/tests/test_site_worker_setup.py. Test-only keys
// derived from fixed numbers; the box decrypts this exact envelope there.
const BOX_PUBLIC_PEM = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE1cygAED+/R7rlIkPm3b3/gox2+1e
qHgrKULe9IEOCEg0Jk5iU+VUELU4tnhjzJ/lT5TSfA04YavTUDpEnR07FQ==
-----END PUBLIC KEY-----`;
const EPHEMERAL_JWK: JsonWebKey = {
  kty: "EC",
  crv: "P-256",
  d: "6eAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAI",
  x: "LRLSPbGiiqo64NwG0ZOKQa7_KXn2cQw3b-auR8gy-wU",
  y: "Z0ojzTmmMJRlFJVNxZkKMENMDzlCK_eaqf0nUys7kiw",
};
const VECTOR = {
  v: 1,
  epk: "BC0S0j2xooqqOuDcBtGTikGu/yl59nEMN2/mrkfIMvsFZ0ojzTmmMJRlFJVNxZkKMENMDzlCK/eaqf0nUys7kiw=",
  salt: "AAECAwQFBgcICQoLDA0ODw==",
  iv: "ZGVmZ2hpamtsbW5v",
  ct: "cjpxB9qinL5xD8T/FBt6msUcQK2+WOG/rVIZ46PHjBCfeEjCSeS3ydzSZJrznponsGEOuolc2Tf5uNI4tYFM",
};

test("browser encryption matches the site box test vector byte for byte", async () => {
  const salt = Uint8Array.from({ length: 16 }, (_, index) => index);
  const iv = Uint8Array.from({ length: 12 }, (_, index) => 100 + index);
  const envelope = await encryptCredentials(BOX_PUBLIC_PEM, { username: "syncam", password: "Pa:ss@w/rd#1" }, { ephemeralJwk: EPHEMERAL_JWK, salt, iv });
  assert.deepEqual(envelope, VECTOR);
});

test("every real encryption uses a fresh key, salt and IV", async () => {
  const first = await encryptCredentials(BOX_PUBLIC_PEM, { username: "u", password: "p" });
  const second = await encryptCredentials(BOX_PUBLIC_PEM, { username: "u", password: "p" });
  assert.notEqual(first.epk, second.epk);
  assert.notEqual(first.iv, second.iv);
  assert.notEqual(first.ct, second.ct);
});

test("a malformed box key is rejected", () => {
  assert.throws(() => pemToDer("-----BEGIN PUBLIC KEY-----\n***\n-----END PUBLIC KEY-----"));
  assert.throws(() => pemToDer(""));
});
