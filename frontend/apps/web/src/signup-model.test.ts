import assert from "node:assert/strict";
import test from "node:test";
import {
  needsAuthenticator,
  normalizeCode,
  passwordUpdateMessage,
  registerWorkspace,
  RegistrationError,
  trialStatus,
  validateEmail,
  validatePassword,
  validateSignUp,
  validateWorkspaceDraft,
  validCode,
} from "./signup-model.ts";

test("sign-up needs a valid email, a 15-character password and explicit consent", () => {
  assert.equal(validateEmail("owner@acme.example"), null);
  assert.ok(validateEmail("owner@acme"));
  assert.ok(validateEmail(" "));
  assert.ok(validatePassword("short password"));
  assert.equal(validatePassword("correct horse battery"), null);
  assert.ok(validatePassword("x".repeat(73)), "bcrypt ignores bytes after 72");
  assert.ok(validatePassword("é".repeat(40)), "the limit counts bytes, not characters");
  assert.match(validateSignUp({ email: "owner@acme.example", password: "correct horse battery", consent: false }) ?? "", /Terms/);
  assert.equal(validateSignUp({ email: "owner@acme.example", password: "correct horse battery", consent: true }), null);
});

test("verification codes are six digits", () => {
  assert.equal(normalizeCode(" 12-34 56 7"), "123456");
  assert.equal(validCode("123456"), true);
  assert.equal(validCode("12345"), false);
  assert.equal(validCode("12345a"), false);
});

test("a password reset on an account with an authenticator asks for its code", () => {
  const aal = { code: "insufficient_aal", message: "AAL2 session is required to update email or password when MFA is enabled." };
  assert.equal(needsAuthenticator(aal), true);
  assert.equal(needsAuthenticator({ code: "same_password" }), false);
  assert.equal(needsAuthenticator(new Error("offline")), false);
  assert.equal(needsAuthenticator(null), false);
  assert.match(passwordUpdateMessage(aal), /authenticator app/);
  assert.match(passwordUpdateMessage({ code: "same_password" }), /current password/);
  assert.match(passwordUpdateMessage({ code: "weak_password" }), /too easy to guess/);
  assert.match(passwordUpdateMessage(new Error("offline")), /could not be saved/);
});

test("workspace setup needs a company, a site and a timezone", () => {
  const draft = { company: "Acme Security", siteName: "Head office", timezone: "America/Toronto" };
  assert.equal(validateWorkspaceDraft(draft), null);
  assert.match(validateWorkspaceDraft({ ...draft, company: " A " }) ?? "", /company/);
  assert.match(validateWorkspaceDraft({ ...draft, siteName: "x".repeat(121) }) ?? "", /site/);
  assert.match(validateWorkspaceDraft({ ...draft, timezone: "" }) ?? "", /timezone/);
});

test("registration posts to the API with the token and no tenant header", async () => {
  let seen: { url: string; init: RequestInit } | undefined;
  const fetcher = (async (url: string, init: RequestInit) => {
    seen = { url, init };
    return new Response(JSON.stringify({ data: { tenant_id: "t-1", site_id: "s-1", created: true } }), { status: 201 });
  }) as unknown as typeof fetch;
  const result = await registerWorkspace({
    baseUrl: "https://api.example.test", accessToken: "token-1", idempotencyKey: "key-1", fetcher,
    draft: { company: " Acme ", siteName: "Head office", timezone: "UTC" },
  });
  assert.deepEqual(result, { tenantId: "t-1", siteId: "s-1", created: true });
  assert.equal(seen?.url, "https://api.example.test/v1/tenants");
  const headers = seen?.init.headers as Record<string, string>;
  assert.equal(headers.Authorization, "Bearer token-1");
  assert.equal(headers["Idempotency-Key"], "key-1");
  assert.equal(headers["X-SentinelVision-Tenant-ID"], undefined);
  assert.deepEqual(JSON.parse(String(seen?.init.body)), { company: "Acme", site_name: "Head office", timezone: "UTC" });
});

test("registration errors become plain messages", async () => {
  const failing = (status: number, code: string) => (async () => new Response(JSON.stringify({ error: { code } }), { status })) as unknown as typeof fetch;
  const options = { baseUrl: "", accessToken: "t", idempotencyKey: "k", draft: { company: "Acme", siteName: "Office", timezone: "UTC" } };
  await assert.rejects(registerWorkspace({ ...options, fetcher: failing(403, "EMAIL_NOT_VERIFIED") }),
    (error: unknown) => error instanceof RegistrationError && error.code === "EMAIL_NOT_VERIFIED" && /Verify your email/.test(error.message));
  await assert.rejects(registerWorkspace({ ...options, fetcher: failing(429, "RATE_LIMITED") }), /Too many attempts/);
  await assert.rejects(registerWorkspace({ ...options, fetcher: failing(503, "SERVICE_UNAVAILABLE") }), /could not create your workspace/);
  const malformed = (async () => new Response(JSON.stringify({ data: { tenant_id: 1 } }), { status: 201 })) as unknown as typeof fetch;
  await assert.rejects(registerWorkspace({ ...options, fetcher: malformed }), /unexpected answer/);
});

test("the trial banner counts days and cameras", () => {
  const now = new Date("2026-11-17T12:00:00Z");
  const trial = { plan: "trial", trialEndsAt: "2026-11-30T12:00:00.000000+00:00", cameraLimit: 10 };
  assert.deepEqual(trialStatus(trial, 3, now), { label: "Free trial: 13 days left", ended: false, daysLeft: 13, cameraText: "3 of 10 cameras", atCameraLimit: false });
  assert.equal(trialStatus({ ...trial, trialEndsAt: "2026-11-18T11:00:00Z" }, 0, now)?.label, "Free trial: 1 day left");
  assert.equal(trialStatus(trial, 10, now)?.atCameraLimit, true);
  assert.deepEqual(trialStatus({ ...trial, trialEndsAt: "2026-11-17T11:59:59Z" }, 0, now)?.ended, true);
  assert.equal(trialStatus({ ...trial, plan: "suspended" }, 0, now)?.ended, true);
  assert.equal(trialStatus({ plan: "active", trialEndsAt: null, cameraLimit: null }, 40, now), null);
  assert.equal(trialStatus(undefined, 40, now), null);
});
