// Pure rules for self-service sign-up, workspace setup and the trial banner
// (ADR-016). UI help only: Supabase Auth and the Go API enforce the real rules.

export const MIN_PASSWORD_LENGTH = 15;
// Supabase hashes passwords with bcrypt, which uses at most 72 bytes.
export const MAX_PASSWORD_BYTES = 72;

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function validateEmail(email: string): string | null {
  const value = email.trim();
  if (!emailPattern.test(value) || value.length > 254) return "Enter a valid email address.";
  return null;
}

export function validatePassword(password: string): string | null {
  if (password.length < MIN_PASSWORD_LENGTH) return `Use at least ${MIN_PASSWORD_LENGTH} characters. A short sentence or a few random words works well.`;
  if (new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES) return "That password is too long. Use at most 72 characters.";
  return null;
}

export function validateSignUp(input: { email: string; password: string; consent: boolean }): string | null {
  return validateEmail(input.email)
    ?? validatePassword(input.password)
    ?? (input.consent ? null : "Please agree to the Terms and Privacy Policy to create an account.");
}

export function normalizeCode(value: string): string {
  return value.replace(/\D/g, "").slice(0, 6);
}

export function validCode(code: string): boolean {
  return /^\d{6}$/.test(code);
}

// Supabase refuses a password change from an email-code session when the
// account has an authenticator app (error code insufficient_aal): the
// authenticator code must be entered too.
export function needsAuthenticator(error: unknown): boolean {
  return typeof error === "object" && error !== null && (error as { code?: unknown }).code === "insufficient_aal";
}

export function passwordUpdateMessage(error: unknown): string {
  const code = typeof error === "object" && error !== null ? (error as { code?: unknown }).code : undefined;
  if (code === "insufficient_aal") return "This account uses an authenticator app. Enter its 6-digit code as well, then save again.";
  if (code === "same_password") return "That is your current password. Choose a different one.";
  if (code === "weak_password") return "That password is too easy to guess. Choose a longer one that you do not use anywhere else.";
  return "Your new password could not be saved. Try again, or sign out and request a new code.";
}

export type WorkspaceDraft = { company: string; siteName: string; timezone: string };

export function validateWorkspaceDraft(draft: WorkspaceDraft): string | null {
  const company = draft.company.trim();
  const site = draft.siteName.trim();
  if ([...company].length < 2 || [...company].length > 120) return "Enter your company name (2 to 120 characters).";
  if ([...site].length < 2 || [...site].length > 120) return "Enter a name for your first site, for example Head office (2 to 120 characters).";
  if (!draft.timezone.trim()) return "Choose the timezone of your first site.";
  return null;
}

export type RegistrationResult = { tenantId: string; siteId: string; created: boolean };

export class RegistrationError extends Error {
  readonly status: number;
  readonly code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = "RegistrationError";
    this.status = status;
    this.code = code;
  }
}

const registrationMessages: Record<string, string> = {
  EMAIL_NOT_VERIFIED: "Verify your email address first, then try again.",
  RATE_LIMITED: "Too many attempts from this network. Try again in an hour.",
  VALIDATION_FAILED: "Check the company name, site name and timezone, then try again.",
  AUTH_REQUIRED: "Your session has expired. Sign in again.",
};

// Creates the user's first workspace through the Go API (POST /v1/tenants).
export async function registerWorkspace(options: {
  baseUrl: string;
  accessToken: string;
  draft: WorkspaceDraft;
  idempotencyKey: string;
  fetcher?: typeof fetch;
}): Promise<RegistrationResult> {
  const fetcher = options.fetcher ?? fetch;
  const response = await fetcher(`${options.baseUrl}/v1/tenants`, {
    method: "POST",
    cache: "no-store",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      Authorization: `Bearer ${options.accessToken}`,
      "Idempotency-Key": options.idempotencyKey,
    },
    body: JSON.stringify({
      company: options.draft.company.trim(),
      site_name: options.draft.siteName.trim(),
      timezone: options.draft.timezone.trim(),
    }),
  });
  const payload = (await response.json().catch(() => null)) as { data?: Record<string, unknown>; error?: { code?: unknown } } | null;
  if (!response.ok) {
    const code = typeof payload?.error?.code === "string" ? payload.error.code : undefined;
    throw new RegistrationError(
      (code && registrationMessages[code]) || "We could not create your workspace. Check your connection and try again.",
      response.status,
      code,
    );
  }
  const data = payload?.data;
  if (!data || typeof data.tenant_id !== "string" || typeof data.site_id !== "string" || typeof data.created !== "boolean") {
    throw new RegistrationError("The server sent an unexpected answer. Try again.", response.status);
  }
  return { tenantId: data.tenant_id, siteId: data.site_id, created: data.created };
}

export type TrialStatus = {
  label: string;
  ended: boolean;
  daysLeft: number | null;
  cameraText: string | null;
  atCameraLimit: boolean;
};

// The trial banner text, or null when the workspace has no trial.
export function trialStatus(
  plan: { plan: string; trialEndsAt: string | null; cameraLimit: number | null } | undefined,
  cameraCount: number,
  now: Date = new Date(),
): TrialStatus | null {
  if (!plan || plan.plan === "active") return null;
  const cameraText = plan.cameraLimit === null ? null : `${cameraCount} of ${plan.cameraLimit} cameras`;
  const atCameraLimit = plan.cameraLimit !== null && cameraCount >= plan.cameraLimit;
  if (plan.plan === "suspended") {
    return { label: "Your workspace is paused", ended: true, daysLeft: null, cameraText, atCameraLimit };
  }
  const ends = plan.trialEndsAt ? Date.parse(plan.trialEndsAt) : Number.NaN;
  if (Number.isNaN(ends)) return { label: "Free trial", ended: false, daysLeft: null, cameraText, atCameraLimit };
  const remaining = ends - now.getTime();
  if (remaining <= 0) return { label: "Your free trial has ended", ended: true, daysLeft: 0, cameraText, atCameraLimit };
  const daysLeft = Math.ceil(remaining / 86_400_000);
  return { label: `Free trial: ${daysLeft} ${daysLeft === 1 ? "day" : "days"} left`, ended: false, daysLeft, cameraText, atCameraLimit };
}
