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

  if (!/^sb_publishable_[A-Za-z0-9_-]+$/.test(publishableKey)) {
    return { mode: "misconfigured", message: "Use a Supabase publishable key (sb_publishable_), never a secret or service-role key." };
  }

  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" || !/^[a-z0-9-]+\.supabase\.co$/.test(parsed.hostname)
      || parsed.username || parsed.password || parsed.port || parsed.search || parsed.hash || parsed.pathname !== "/") {
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

let browserClient: { url: string; publishableKey: string; client: SupabaseClient } | undefined;

export function createSupabaseAuthClient(config: Extract<AuthRuntimeConfig, { mode: "configured" }>) {
  // One client per page, including React StrictMode remounts. Configuration
  // changes require a page reload rather than competing auth refresh loops.
  if (browserClient) {
    if (browserClient.url !== config.url || browserClient.publishableKey !== config.publishableKey) {
      throw new Error("Authentication configuration changed; reload this page.");
    }
    return browserClient.client;
  }
  const client = createClient(config.url, config.publishableKey, {
    auth: {
      autoRefreshToken: true,
      detectSessionInUrl: true,
      flowType: "pkce",
      persistSession: true,
    },
  });
  browserClient = { url: config.url, publishableKey: config.publishableKey, client };
  return client;
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
