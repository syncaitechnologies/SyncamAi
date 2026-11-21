// Owner admin screen (ADR-018): pure rules for showing a workspace and turning
// the edit form into a change. The API and database validate again.

export type Plan = "trial" | "active" | "suspended";

export type AdminWorkspace = {
  tenant_id: string;
  name: string;
  slug: string;
  plan: Plan;
  trial_ends_at: string | null;
  site_limit: number | null;
  camera_limit: number | null;
  live_view_minutes: number | null;
  live_view_seconds: number;
  sites: number;
  cameras: number;
  created_at: string;
};

export type AdminChanges = Partial<{
  plan: Plan;
  trial_ends_at: string | null;
  site_limit: number | null;
  camera_limit: number | null;
  live_view_minutes: number | null;
}>;

// The edit form keeps text fields; "" means unlimited for the limits.
export type AdminDraft = {
  plan: Plan;
  trialEndsOn: string; // YYYY-MM-DD, end of that day UTC
  siteLimit: string;
  cameraLimit: string;
  liveViewMinutes: string;
};

const DAY = 86_400_000;

function limitText(value: number | null) {
  return value === null ? "" : String(value);
}

export function draftFrom(workspace: AdminWorkspace): AdminDraft {
  return {
    plan: workspace.plan,
    trialEndsOn: workspace.trial_ends_at ? workspace.trial_ends_at.slice(0, 10) : "",
    siteLimit: limitText(workspace.site_limit),
    cameraLimit: limitText(workspace.camera_limit),
    liveViewMinutes: limitText(workspace.live_view_minutes),
  };
}

function parseLimit(text: string, label: string, maximum: number): number | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  if (!/^\d+$/.test(trimmed) || Number(trimmed) > maximum) {
    throw new Error(`${label} must be a whole number from 0 to ${maximum.toLocaleString("en-US")}, or empty for unlimited.`);
  }
  return Number(trimmed);
}

// The changes between the saved workspace and the form, or an error message.
export function changesFrom(workspace: AdminWorkspace, draft: AdminDraft): { changes: AdminChanges } | { error: string } {
  try {
    const changes: AdminChanges = {};
    const siteLimit = parseLimit(draft.siteLimit, "Site limit", 100_000);
    const cameraLimit = parseLimit(draft.cameraLimit, "Camera limit", 100_000);
    const liveViewMinutes = parseLimit(draft.liveViewMinutes, "Live-view minutes", 1_000_000);
    let trialEndsAt: string | null = null;
    if (draft.trialEndsOn.trim()) {
      if (!/^\d{4}-\d{2}-\d{2}$/.test(draft.trialEndsOn) || Number.isNaN(Date.parse(`${draft.trialEndsOn}T23:59:59Z`))) {
        return { error: "Trial end must be a date." };
      }
      trialEndsAt = `${draft.trialEndsOn}T23:59:59Z`;
    }
    if (draft.plan === "trial" && !trialEndsAt) return { error: "A trial needs an end date." };
    if (draft.plan !== workspace.plan) changes.plan = draft.plan;
    const savedTrialDay = workspace.trial_ends_at ? workspace.trial_ends_at.slice(0, 10) : "";
    if (draft.trialEndsOn.trim() !== savedTrialDay) changes.trial_ends_at = trialEndsAt;
    if (siteLimit !== workspace.site_limit) changes.site_limit = siteLimit;
    if (cameraLimit !== workspace.camera_limit) changes.camera_limit = cameraLimit;
    if (liveViewMinutes !== workspace.live_view_minutes) changes.live_view_minutes = liveViewMinutes;
    if (Object.keys(changes).length === 0) return { error: "Nothing has changed." };
    return { changes };
  } catch (error) {
    return { error: error instanceof Error ? error.message : "That change is not valid." };
  }
}

// A date a number of days from today, for the "+14 days" trial button.
export function dayFromNow(days: number, now = new Date()): string {
  return new Date(now.getTime() + days * DAY).toISOString().slice(0, 10);
}

export function planLabel(workspace: AdminWorkspace, now = new Date()): string {
  if (workspace.plan === "active") return "Active";
  if (workspace.plan === "suspended") return "Suspended";
  if (!workspace.trial_ends_at) return "Trial";
  const left = Math.ceil((Date.parse(workspace.trial_ends_at) - now.getTime()) / DAY);
  if (left <= 0) return "Trial ended";
  return `Trial · ${left} ${left === 1 ? "day" : "days"} left`;
}

export function usageLabel(used: number, limit: number | null, unit: string): string {
  return limit === null ? `${used} · unlimited` : `${used} of ${limit} ${unit}`;
}

export function liveMinutesLabel(workspace: AdminWorkspace): string {
  const used = Math.round((workspace.live_view_seconds / 60) * 10) / 10;
  if (workspace.live_view_minutes === null) return `${used} min · unlimited`;
  if (workspace.live_view_minutes === 0) return "Off";
  return `${used} of ${workspace.live_view_minutes} min`;
}
