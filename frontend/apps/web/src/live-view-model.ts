// Pure rules for metered live view: usage wording, minutes left, and how API
// errors map to what the live feed shows. No DOM or network access here.

/** GET /v1/live-view/usage. `limit_minutes` null = unlimited, 0 = live view off. */
export type LiveViewUsage = {
  limit_minutes: number | null;
  used_seconds: number;
  month: string;
  resets_at: string;
};

/** Why a live frame request returned no picture. */
export type LiveFrameProblem = "pending" | "expired" | "paused" | "limit" | "off";

export const LIVE_VIEW_LIMIT = "LIVE_VIEW_LIMIT";
export const LIVE_VIEW_PAUSED = "LIVE_VIEW_PAUSED";
/** The workspace's admins switched live view off on the Features page (ADR-019). */
export const LIVE_VIEW_OFF = "LIVE_VIEW_OFF";
/** Continuous viewing before the server pauses a session. */
export const PAUSE_AFTER_MINUTES = 5;
/** How often the camera wall reloads stills; matches the site box upload interval. */
export const STILL_REFRESH_MS = 5 * 60 * 1000;

/** The `error.code` of a v1 error body, if any. */
export function errorCode(payload: unknown): string | undefined {
  if (typeof payload !== "object" || payload === null || !("error" in payload)) return undefined;
  const error = (payload as { error: unknown }).error;
  if (typeof error !== "object" || error === null || !("code" in error)) return undefined;
  const code = (error as { code: unknown }).code;
  return typeof code === "string" ? code : undefined;
}

/**
 * Maps a failed GET /live/frame response. Returns null for failures that are
 * real errors (and should be shown and retried).
 */
export function frameProblem(status: number, code: string | undefined): LiveFrameProblem | null {
  if (code === LIVE_VIEW_LIMIT && (status === 403 || status === 409)) return "limit";
  if (code === LIVE_VIEW_OFF && status === 403) return "off";
  if (code === LIVE_VIEW_PAUSED && status === 409) return "paused";
  if (status === 409) return "expired";
  if (status === 404) return "pending";
  return null;
}

/** Whether an error from POST /live means the monthly allowance is used up (or live view is off). */
export function isLimitError(error: unknown) {
  return typeof error === "object" && error !== null && "code" in error && (error as { code: unknown }).code === LIVE_VIEW_LIMIT;
}

/** Whether an error from POST /live means the workspace switched live view off. */
export function isSwitchedOffError(error: unknown) {
  return typeof error === "object" && error !== null && "code" in error && (error as { code: unknown }).code === LIVE_VIEW_OFF;
}

/** What the feed says when the workspace has switched live view off. */
export const SWITCHED_OFF_MESSAGE = "Live view is switched off for this workspace";

export function isUnlimited(usage: LiveViewUsage) {
  return usage.limit_minutes === null;
}

export function isOff(usage: LiveViewUsage) {
  return usage.limit_minutes === 0;
}

/** Seconds of live view left this month; null when unlimited. */
export function secondsLeft(usage: LiveViewUsage): number | null {
  if (usage.limit_minutes === null) return null;
  return Math.max(0, usage.limit_minutes * 60 - Math.max(0, usage.used_seconds));
}

/** Whole minutes left this month (rounded down); null when unlimited. */
export function minutesLeft(usage: LiveViewUsage): number | null {
  const seconds = secondsLeft(usage);
  return seconds === null ? null : Math.floor(seconds / 60);
}

/** Minutes used, rounded up so "used" plus "left" adds up to the limit. */
export function minutesUsed(usage: LiveViewUsage) {
  const used = Math.ceil(Math.max(0, usage.used_seconds) / 60);
  return usage.limit_minutes === null ? used : Math.min(usage.limit_minutes, used);
}

/** Whether a new live session can start. Unknown usage (not loaded) lets the server decide. */
export function canStartLive(usage: LiveViewUsage | null | undefined) {
  if (!usage) return true;
  const seconds = secondsLeft(usage);
  return seconds === null || seconds > 0;
}

/** One-line summary for the camera wall banner. */
export function usageLabel(usage: LiveViewUsage) {
  if (isUnlimited(usage)) return "Unlimited live view";
  if (isOff(usage)) return "Live view is off for this workspace";
  return `${minutesUsed(usage)} of ${usage.limit_minutes} min used this month`;
}

/** Short label next to the LIVE chip. */
export function minutesLeftLabel(usage: LiveViewUsage) {
  const seconds = secondsLeft(usage);
  if (seconds === null) return "unlimited";
  if (seconds <= 0) return "no minutes left";
  if (seconds < 60) return "under 1 min left";
  return `${Math.floor(seconds / 60)} min left`;
}

/** "Resets 1 Dec" in UTC, the month boundary the server meters on. */
export function resetLabel(usage: LiveViewUsage, locale = "en-GB") {
  const date = new Date(usage.resets_at);
  if (Number.isNaN(date.getTime())) return "";
  return `Resets ${date.toLocaleDateString(locale, { day: "numeric", month: "short", timeZone: "UTC" })}`;
}

/** What the feed says once the server refuses or stops a session for the allowance. */
export function limitMessage(usage: LiveViewUsage | null | undefined) {
  return usage && isOff(usage) ? "Live view is off for this workspace" : "Live view minutes for this month are used up";
}

export const PAUSED_MESSAGE = `Paused after ${PAUSE_AFTER_MINUTES} minutes`;

/** A mailto link for the contact address, or null when the address is missing or malformed. */
export function contactHref(email: string | undefined, subject: string) {
  const address = (email ?? "").trim();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address)) return null;
  return `mailto:${address}?subject=${encodeURIComponent(subject)}`;
}

/**
 * Age of a camera still. With a capture time from the server: "Still · 3 min
 * ago". Without one, say how often stills refresh rather than invent an age.
 */
export function stillAgeLabel(capturedAt: string | null, now: number) {
  const captured = capturedAt ? new Date(capturedAt).getTime() : Number.NaN;
  if (Number.isNaN(captured)) return "Still · refreshed about every 5 min";
  const seconds = Math.max(0, (now - captured) / 1000);
  if (seconds < 60) return "Still · under 1 min ago";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `Still · ${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `Still · ${hours} h ago`;
  return `Still · ${Math.floor(hours / 24)} days ago`;
}
