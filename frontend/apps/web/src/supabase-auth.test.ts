import assert from "node:assert/strict";
import test from "node:test";

import {
  parseAuthRuntimeConfig,
  ssoDomainFromEmail,
  syncAccessToken,
  syncamAccessTokenStorageKey,
} from "./supabase-auth.ts";

test("keeps demo mode independent of Supabase credentials", () => {
  assert.deepEqual(parseAuthRuntimeConfig({ VITE_SYNCAM_DATA_MODE: "demo" }), {
    mode: "demo",
  });
});

test("fails closed when live mode has incomplete or placeholder Supabase configuration", () => {
  const config = parseAuthRuntimeConfig({
    VITE_SYNCAM_DATA_MODE: "live",
    VITE_SUPABASE_URL: "https://your-project-ref.supabase.co",
    VITE_SUPABASE_PUBLISHABLE_KEY: "replace-with-publishable-key",
  });
  assert.equal(config.mode, "misconfigured");
});

test("accepts only an HTTPS project URL and a configured publishable key", () => {
  assert.deepEqual(
    parseAuthRuntimeConfig({
      VITE_SYNCAM_DATA_MODE: "live",
      VITE_SUPABASE_URL: "https://example.supabase.co/",
      VITE_SUPABASE_PUBLISHABLE_KEY: "sb_publishable_example",
    }),
    {
      mode: "configured",
      url: "https://example.supabase.co",
      publishableKey: "sb_publishable_example",
    },
  );
  assert.equal(
    parseAuthRuntimeConfig({
      VITE_SYNCAM_DATA_MODE: "live",
      VITE_SUPABASE_URL: "https://example.invalid",
      VITE_SUPABASE_PUBLISHABLE_KEY: "sb_publishable_example",
    }).mode,
    "misconfigured",
  );
});

test("derives a conservative SSO domain from a work email", () => {
  assert.equal(ssoDomainFromEmail(" operator@syncam.example "), "syncam.example");
  assert.equal(ssoDomainFromEmail("operator@localhost"), null);
  assert.equal(ssoDomainFromEmail("not-an-email"), null);
});

test("copies only the short-lived access token into the legacy API bridge", () => {
  const values = new Map<string, string>();
  const storage = {
    setItem(key: string, value: string) {
      values.set(key, value);
    },
    removeItem(key: string) {
      values.delete(key);
    },
  };
  syncAccessToken({ access_token: "short-lived-access-token" }, storage);
  assert.equal(values.get(syncamAccessTokenStorageKey), "short-lived-access-token");
  syncAccessToken(null, storage);
  assert.equal(values.has(syncamAccessTokenStorageKey), false);
});
