/**
 * End-to-end encryption of camera passwords for one site box.
 *
 * The dashboard encrypts `{username, password}` with the paired box's public
 * key (ECDH P-256 -> HKDF-SHA256 -> AES-256-GCM, browser WebCrypto only), so
 * the SyncCam API relays ciphertext it cannot read and no other box can open
 * it. The box side is ai-services/src/syncam_ai/site_worker/setup_crypto.py;
 * both are checked against the same fixed test vector.
 */

const INFO = new TextEncoder().encode("syncam-camera-credentials-v1");

export type CredentialEnvelope = { v: 1; epk: string; salt: string; iv: string; ct: string };
export type CameraCredentials = { username: string; password: string };

function base64(bytes: ArrayBuffer | Uint8Array) {
  const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let binary = "";
  for (const byte of view) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/** The DER bytes of a PEM "PUBLIC KEY" block. */
export function pemToDer(pem: string): ArrayBuffer {
  const body = pem.replace(/-----(BEGIN|END) PUBLIC KEY-----/g, "").replace(/\s+/g, "");
  if (!body || /[^A-Za-z0-9+/=]/.test(body)) throw new Error("The site box public key is not valid.");
  const binary = atob(body);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes.buffer;
}

export async function encryptCredentials(
  boxPublicPem: string,
  credentials: CameraCredentials,
  options: { ephemeralJwk?: JsonWebKey; salt?: Uint8Array<ArrayBuffer>; iv?: Uint8Array<ArrayBuffer>; subtle?: SubtleCrypto } = {},
): Promise<CredentialEnvelope> {
  const subtle = options.subtle ?? globalThis.crypto.subtle;
  const curve = { name: "ECDH", namedCurve: "P-256" };
  const boxKey = await subtle.importKey("spki", pemToDer(boxPublicPem), curve, false, []);
  let privateKey: CryptoKey;
  let publicRaw: ArrayBuffer;
  if (options.ephemeralJwk) {
    // Fixed keys are for the shared test vector only.
    privateKey = await subtle.importKey("jwk", options.ephemeralJwk, curve, false, ["deriveBits"]);
    const { d: _private, ...publicJwk } = options.ephemeralJwk;
    publicRaw = await subtle.exportKey("raw", await subtle.importKey("jwk", { ...publicJwk, key_ops: [] }, curve, true, []));
  } else {
    const pair = (await subtle.generateKey(curve, true, ["deriveBits"])) as CryptoKeyPair;
    privateKey = pair.privateKey;
    publicRaw = await subtle.exportKey("raw", pair.publicKey);
  }
  const salt: Uint8Array<ArrayBuffer> = options.salt ?? globalThis.crypto.getRandomValues(new Uint8Array(16));
  const iv: Uint8Array<ArrayBuffer> = options.iv ?? globalThis.crypto.getRandomValues(new Uint8Array(12));
  const secret = await subtle.deriveBits({ name: "ECDH", public: boxKey }, privateKey, 256);
  const hkdf = await subtle.importKey("raw", secret, "HKDF", false, ["deriveKey"]);
  const aes = await subtle.deriveKey({ name: "HKDF", hash: "SHA-256", salt, info: INFO }, hkdf, { name: "AES-GCM", length: 256 }, false, ["encrypt"]);
  const plaintext = new TextEncoder().encode(JSON.stringify({ username: credentials.username, password: credentials.password }));
  const ciphertext = await subtle.encrypt({ name: "AES-GCM", iv }, aes, plaintext);
  return { v: 1, epk: base64(publicRaw), salt: base64(salt), iv: base64(iv), ct: base64(ciphertext) };
}
