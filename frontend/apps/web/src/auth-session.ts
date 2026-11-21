import type { Session } from "@supabase/supabase-js";
import type { AuthClient } from "./supabase-auth.ts";

export type MfaFactor = { id: string; friendly_name?: string };
// Workspace plan from the token (ADR-016). Absent for workspaces made before plans.
export type WorkspacePlan = { plan: "trial" | "active" | "suspended"; trialEndsAt: string | null; siteLimit: number | null; cameraLimit: number | null };
export type SessionDecision =
  | { kind: "ready"; tenantId: string; siteIds: string[]; roles: string[]; plan?: WorkspacePlan; operator?: true }
  | { kind: "needs_workspace"; emailVerified: boolean }
  | { kind: "enroll" }
  | { kind: "challenge"; factors: MfaFactor[] }
  | { kind: "blocked"; message: string };

const roles = new Set(["super_admin", "site_admin", "operator", "auditor", "viewer"]);
const missingMembership = "Your account has no supported workspace membership. Contact your organization administrator.";

function record(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown> : null;
}

function strings(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string" && item.trim() !== "");
}

function limit(value: unknown): number | null | undefined {
  if (value === null || value === undefined) return null;
  return typeof value === "number" && Number.isInteger(value) && value >= 0 ? value : undefined;
}

// The plan claims, or undefined when absent; null when present but malformed.
function workspacePlan(membership: Record<string, unknown>): WorkspacePlan | undefined | null {
  if (membership.plan === undefined) return undefined;
  const plan = membership.plan;
  if (plan !== "trial" && plan !== "active" && plan !== "suspended") return null;
  const trialEndsAt = membership.trial_ends_at ?? null;
  if (trialEndsAt !== null && (typeof trialEndsAt !== "string" || Number.isNaN(Date.parse(trialEndsAt)))) return null;
  const siteLimit = limit(membership.site_limit);
  const cameraLimit = limit(membership.camera_limit);
  if (siteLimit === undefined || cameraLimit === undefined) return null;
  return { plan, trialEndsAt, siteLimit, cameraLimit };
}

// UI routing only. Call with getClaims-verified claims, never session.user metadata.
// The Go API independently verifies the JWT and enforces authorization and MFA.
export function decideSession(
  claims: Record<string, unknown>,
  factors: { id: string; factor_type: string; status: string; friendly_name?: string }[],
): SessionDecision {
  if (claims.is_anonymous === true) return { kind: "blocked", message: missingMembership };
  const appMetadata = record(claims.app_metadata);
  // The token hook omits the syncam claim only for a user with no membership at
  // all (ADR-016): they may create their first workspace. A present but
  // malformed claim still fails closed.
  if (!appMetadata || appMetadata.syncam === undefined) {
    return { kind: "needs_workspace", emailVerified: record(appMetadata?.syncam_registration)?.email_verified === true };
  }
  const membership = record(appMetadata.syncam);
  const plan = membership ? workspacePlan(membership) : null;
  if (!membership || plan === null || typeof membership.tenant_id !== "string" || !membership.tenant_id.trim()
    || !strings(membership.roles) || !membership.roles.length
    || membership.roles.some((role) => !roles.has(role))
    || !strings(membership.scopes) || !membership.scopes.length
    || !strings(membership.data_class) || !membership.data_class.length
    || !strings(membership.site_ids)
    || (!membership.roles.includes("super_admin") && !membership.site_ids.length)) {
    return { kind: "blocked", message: missingMembership };
  }
  if (claims.aal !== "aal1" && claims.aal !== "aal2") {
    return { kind: "blocked", message: "Your session assurance could not be verified. Sign out and try again." };
  }
  const verified = factors.filter((factor) => factor.status === "verified");
  const requiresMfa = membership.roles.some((role) => role === "super_admin" || role === "auditor") || verified.length > 0;
  if (requiresMfa && claims.aal !== "aal2") {
    if (!verified.length) return { kind: "enroll" };
    const totp = verified.filter((factor) => factor.factor_type === "totp");
    if (totp.length) return { kind: "challenge", factors: totp };
    return { kind: "blocked", message: "This account uses a factor not supported by this screen. Contact your administrator; MFA cannot be skipped." };
  }
  // The owner admin screen is shown only for the hook's operator flag; the API
  // and database check it again on every admin call.
  const operator = appMetadata.syncam_operator === true;
  return {
    kind: "ready", tenantId: membership.tenant_id, siteIds: membership.site_ids, roles: membership.roles,
    ...(plan ? { plan } : {}), ...(operator ? { operator: true as const } : {}),
  };
}

export async function inspectSession(client: AuthClient, session: Session): Promise<SessionDecision> {
  const { data, error } = await client.auth.getClaims(session.access_token);
  if (error || !data || data.claims.sub !== session.user.id) {
    throw new Error("Session verification failed");
  }
  const factors = await client.auth.mfa.listFactors();
  if (factors.error) throw new Error("Factor verification failed");
  return decideSession(data.claims, factors.data.all);
}

// Defer async verification to React effects, outside Supabase's auth lock.
// A late restore result must never resurrect a session after a sign-out event.
export function observeSession(
  client: AuthClient,
  update: (session: Session | null) => void,
  failed: () => void,
) {
  let active = true;
  let events = 0;
  const { data } = client.auth.onAuthStateChange((_event, session) => {
    events += 1;
    if (active) update(session);
  });
  const beforeRestore = events;
  void client.auth.getSession().then((result) => {
    if (!active || events !== beforeRestore) return;
    if (result.error) failed();
    else update(result.data.session);
  }).catch(() => {
    if (active && events === beforeRestore) failed();
  });
  return () => {
    active = false;
    data.subscription.unsubscribe();
  };
}
