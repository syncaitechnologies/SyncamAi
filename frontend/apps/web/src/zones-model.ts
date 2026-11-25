// Pure wording for zone actions in Zones & rules.

/** Asked before a zone is removed; removal cannot be undone from the dashboard. */
export function removeZoneQuestion(name: string) {
  const label = name.trim() || "this zone";
  return `Remove "${label}"? Alerts from it stop on the site box within a minute. ` +
    "Past alerts stay in the Alert Center. To stop it for a while instead, pause it.";
}
