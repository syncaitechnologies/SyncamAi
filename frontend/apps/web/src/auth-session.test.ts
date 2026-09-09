import assert from "node:assert/strict";
import test from "node:test";
import type { Session } from "@supabase/supabase-js";
import { decideSession, inspectSession, observeSession } from "./auth-session.ts";
import type { AuthClient } from "./supabase-auth.ts";

function claims(role = "operator", aal = "aal1") {
  return { sub: "user-1", aal, app_metadata: { syncam: {
    tenant_id: "tenant-1", roles: [role], scopes: ["alerts:read"],
    site_ids: role === "super_admin" ? [] : ["site-1"], data_class: ["metadata"],
  } } };
}
const totp = { id: "factor-1", factor_type: "totp", status: "verified" };
const session = { access_token: "test-token", user: { id: "user-1" } } as Session;

test("privileged seed roles enroll before accessing the app", () => {
  for (const role of ["super_admin", "auditor"]) {
    assert.deepEqual(decideSession(claims(role), []), { kind: "enroll" });
    assert.deepEqual(decideSession(claims(role, "aal2"), [totp]), { kind: "ready", tenantId: "tenant-1" });
  }
});

test("all users with a verified factor must challenge at aal1", () => {
  for (const role of ["super_admin", "auditor", "site_admin", "operator", "viewer"]) {
    assert.deepEqual(decideSession(claims(role), [totp]), { kind: "challenge", factors: [totp] });
  }
});

test("standard roles without enrolled MFA retain current server policy", () => {
  assert.equal(decideSession(claims(), []).kind, "ready");
  assert.equal(decideSession(claims(), [{ ...totp, status: "unverified" }]).kind, "ready");
  assert.equal(decideSession(claims("super_admin"), [{ ...totp, status: "unverified" }]).kind, "enroll");
});

test("unsupported verified factors cannot be bypassed by enrolling another factor", () => {
  assert.equal(decideSession(claims(), [{ ...totp, factor_type: "phone" }]).kind, "blocked");
});

test("multiple verified TOTP factors remain selectable and pending ones are excluded", () => {
  assert.deepEqual(decideSession(claims(), [totp, { ...totp, id: "second" }, { ...totp, id: "pending", status: "unverified" }]), {
    kind: "challenge", factors: [totp, { ...totp, id: "second" }],
  });
});

test("never trusts editable user metadata, top-level roles or an MFA flag", () => {
  const trusted = claims();
  assert.equal(decideSession({ user_metadata: trusted.app_metadata, roles: ["super_admin"], aal: "aal2" }, []).kind, "blocked");
  assert.equal(decideSession({ ...claims("super_admin"), mfa_complete: true }, []).kind, "enroll");
});

test("missing, malformed and unsupported membership or assurance fails closed", () => {
  for (const value of [null, {}, [], { ...claims().app_metadata.syncam, roles: ["hr_manager"] },
    { ...claims().app_metadata.syncam, tenant_id: " " },
    { ...claims().app_metadata.syncam, site_ids: [] },
    { ...claims().app_metadata.syncam, scopes: [] },
    { ...claims().app_metadata.syncam, data_class: [] },
    { ...claims().app_metadata.syncam, roles: "super_admin" }]) {
    assert.equal(decideSession({ ...claims(), app_metadata: { syncam: value } }, []).kind, "blocked");
  }
  assert.equal(decideSession(claims("operator", "unknown"), []).kind, "blocked");
  assert.equal(decideSession({ ...claims(), is_anonymous: true }, []).kind, "blocked");
});

function inspectionClient(options: { invalid?: boolean; wrongSubject?: boolean; factorError?: boolean; throws?: boolean } = {}) {
  const tokens: string[] = [];
  const client = { auth: {
    async getClaims(token: string) {
      tokens.push(token);
      if (options.throws) throw new Error("offline");
      return { error: options.invalid ? new Error("invalid signature") : null,
        data: { claims: { ...claims(), sub: options.wrongSubject ? "another-user" : "user-1" } } };
    },
    mfa: { async listFactors() { return { error: options.factorError ? new Error("offline") : null, data: { all: [] } }; } },
  } } as unknown as AuthClient;
  return { client, tokens };
}

test("checks the exact access token with the SDK before routing the session", async () => {
  const { client, tokens } = inspectionClient();
  assert.deepEqual(await inspectSession(client, session), { kind: "ready", tenantId: "tenant-1" });
  assert.deepEqual(tokens, ["test-token"]);
});

test("invalid signatures, mismatched users and provider failures never unlock App", async () => {
  for (const options of [{ invalid: true }, { wrongSubject: true }, { factorError: true }, { throws: true }]) {
    await assert.rejects(inspectSession(inspectionClient(options).client, session));
  }
});

function observerClient() {
  let emit: (event: string, value: Session | null) => void = () => {};
  let restore!: (value: { data: { session: Session | null }; error: Error | null }) => void;
  let reject!: (reason: Error) => void;
  let unsubscribed = false;
  const restored = new Promise<{ data: { session: Session | null }; error: Error | null }>((resolve, fail) => { restore = resolve; reject = fail; });
  const client = { auth: {
    onAuthStateChange(callback: typeof emit) { emit = callback; return { data: { subscription: { unsubscribe() { unsubscribed = true; } } } }; },
    getSession() { return restored; },
  } } as unknown as AuthClient;
  return { client, emit: (value: Session | null) => emit(value ? "SIGNED_IN" : "SIGNED_OUT", value),
    restore: (value: Session | null) => restore({ data: { session: value }, error: null }), reject,
    unsubscribed: () => unsubscribed };
}
const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

test("a late restoration cannot resurrect a signed-out session", async () => {
  const mock = observerClient();
  const seen: (Session | null)[] = [];
  const stop = observeSession(mock.client, (value) => seen.push(value), () => assert.fail("unexpected error"));
  mock.emit(null);
  mock.restore(session);
  await flush();
  assert.deepEqual(seen, [null]);
  stop();
  assert.equal(mock.unsubscribed(), true);
});

test("restoration is delivered without an auth event, but nothing after cleanup", async () => {
  const mock = observerClient();
  const seen: (Session | null)[] = [];
  const stop = observeSession(mock.client, (value) => seen.push(value), () => assert.fail("unexpected error"));
  mock.restore(session);
  await flush();
  stop();
  mock.emit(null);
  assert.deepEqual(seen, [session]);
});

test("network failure is recoverable and stale failures do not overwrite fresh login", async () => {
  for (const freshEvent of [false, true]) {
    const mock = observerClient();
    let errors = 0;
    const stop = observeSession(mock.client, () => {}, () => { errors += 1; });
    if (freshEvent) mock.emit(session);
    mock.reject(new Error("offline"));
    await flush();
    assert.equal(errors, freshEvent ? 0 : 1);
    stop();
  }
});
