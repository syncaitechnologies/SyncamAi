import { createClient, type Session, type SupabaseClient } from "@supabase/supabase-js";

export const syncamAccessTokenStorageKey = "syncam.access_token";

export type AuthRuntimeConfig =
  | { mode: "demo" }
  | { mode: "configured"; url: string; publishableKey: string }
  | { mode: "misconfigured"; message: string };

type RuntimeEnvironment = Record<string, string | boolean | undefined>;

function readString(value: string | boolean | undefined) {
  return typeof value === "string" ? value.trim() : "";
}

function isPlaceholder(value: string) {
  return value === "" || value.includes("your-project-ref") || value.startsWith("replace-with-");
}

export function parseAuthRuntimeConfig(environment: RuntimeEnvironment): AuthRuntimeConfig {
  const dataMode = readString(environment.VITE_SYNCAM_DATA_MODE);
  if (dataMode !== "live") return { mode: "demo" };

  const url = readString(environment.VITE_SUPABASE_URL).replace(/\/$/, "");
  const publishableKey = readString(environment.VITE_SUPABASE_PUBLISHABLE_KEY);
  if (isPlaceholder(url) || isPlaceholder(publishableKey)) {
    return {
      mode: "misconfigured",
      message:
        "Live mode requires VITE_SUPABASE_URL and VITE_SUPABASE_PUBLISHABLE_KEY.",
    };
  }

  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" || !parsed.hostname.endsWith(".supabase.co")) {
      throw new Error("not a hosted Supabase project URL");
    }
  } catch {
    return {
      mode: "misconfigured",
      message: "VITE_SUPABASE_URL must be an HTTPS hosted Supabase project URL.",
    };
  }

  return { mode: "configured", url, publishableKey };
}

export function readAuthRuntimeConfig() {
  return parseAuthRuntimeConfig(import.meta.env);
}

export function createSupabaseAuthClient(config: Extract<AuthRuntimeConfig, { mode: "configured" }>) {
  return createClient(config.url, config.publishableKey, {
    auth: {
      autoRefreshToken: true,
      detectSessionInUrl: true,
      flowType: "pkce",
      persistSession: true,
    },
  });
}

export function syncAccessToken(
  session: Pick<Session, "access_token"> | null,
  storage: Pick<Storage, "setItem" | "removeItem"> = sessionStorage,
) {
  if (session?.access_token) {
    storage.setItem(syncamAccessTokenStorageKey, session.access_token);
    return;
  }
  storage.removeItem(syncamAccessTokenStorageKey);
}

export function ssoDomainFromEmail(email: string) {
  const normalized = email.trim().toLowerCase();
  const at = normalized.lastIndexOf("@");
  const domain = at > 0 ? normalized.slice(at + 1) : "";
  return /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/.test(
    domain,
  )
    ? domain
    : null;
}

export function currentAuthRedirect() {
  return window.location.origin;
}

export type AuthClient = Pick<
  SupabaseClient,
  "auth"
>;
