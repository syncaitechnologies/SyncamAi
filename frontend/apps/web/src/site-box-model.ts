/** Restarting a site box from the dashboard (box version 0.1.3 and later). */

/** How long to wait for the box to confirm it received the restart. */
export const RESTART_TIMEOUT_MS = 20_000;

const OLD_BOX =
  "This site box runs an older version that can't be restarted from here yet. It updates itself within a day; until then, restart the computer it runs on.";

/** A plain-language reason a restart did not go through. */
export function restartFailure(message: string | undefined): string {
  const text = (message ?? "").trim();
  // Boxes before 0.1.3 answer that they do not know the task, or (without
  // "Find cameras") never answer at all.
  if (/unknown setup task/i.test(text) || /taking too long/i.test(text)) return OLD_BOX;
  if (/did not pick this up/i.test(text)) {
    return "The site box did not pick up the restart. It may have just gone offline: check that the computer is on, awake and connected.";
  }
  return text || "The site box could not be restarted. Try again in a minute.";
}

/** Whether to offer the button: managers only, and only while the box is online. */
export function canRestart(manage: boolean, boxOnline: boolean): boolean {
  return manage && boxOnline;
}
