// Scripted product walkthroughs, never detector output or biometric decisions.
export type DemoFeature = "person" | "weapon" | "face";
export type DemoMode = "demo" | "live";
export type DemoReview = "pending" | "acknowledged" | "dismissed";
export type DemoSettings = {
  feature: DemoFeature;
  scenario: string;
  threshold: number;
  consent: boolean;
  liveness: boolean;
};
export type DemoState = { step: number; review: DemoReview };
export type DemoAction =
  | { type: "advance" }
  | { type: "reset" }
  | { type: "review"; decision: "acknowledged" | "dismissed" };

export const demoFeatures = [
  { id: "person", title: "Person detection", short: "Person", icon: "users",
    description: "See a person, follow a local track, review a zone entry.",
    camera: "Demo camera 01 · Loading bay",
    steps: ["Observe scene", "Check zone entry", "Review outcome"],
    blockers: "Approved person detector and weights, inference runtime, masked camera frames and track-to-alert delivery." },
  { id: "weapon", title: "Weapon detection", short: "Weapon", icon: "shield",
    description: "Confirm a candidate across three observations before human review.",
    camera: "Demo camera 02 · Site entrance",
    steps: ["First observation", "Second observation", "Third observation"],
    blockers: "Approved knife/firearm detector, tool-versus-weapon evaluation, site thresholds and camera-to-review integration." },
  { id: "face", title: "Face recognition", short: "Face", icon: "users",
    description: "Walk through opt-in, liveness and an attendance review.",
    camera: "Demo camera 03 · Staff entrance",
    steps: ["Check opt-in", "Check liveness", "Review outcome"],
    blockers: "Approved biometric purpose and opt-in process; licensed face and liveness models; enrollment, encrypted templates, withdrawal/deletion and attendance services." },
] as const;

export const demoScenarios: Record<DemoFeature, readonly { id: string; label: string }[]> = {
  person: [
    { id: "entry", label: "Person enters a restricted zone" },
    { id: "outside", label: "People remain outside the zone" },
    { id: "empty", label: "Empty scene" },
  ],
  weapon: [
    { id: "candidate", label: "Consistent firearm candidate" },
    { id: "tool", label: "Ordinary tool — no weapon alert" },
    { id: "uncertain", label: "Low-confidence candidate" },
  ],
  face: [
    { id: "opted-in", label: "Fictional opted-in demo subject" },
    { id: "unknown", label: "No enrolled candidate" },
    { id: "spoof", label: "Liveness check fails" },
  ],
};

export function initialDemoState(): DemoState {
  return { step: 0, review: "pending" };
}

export function defaultDemoSettings(feature: DemoFeature): DemoSettings {
  return { feature, scenario: demoScenarios[feature][0]!.id,
    threshold: 0.8, consent: false, liveness: false };
}

export function demoOutcome(mode: DemoMode, settings: DemoSettings, state: DemoState) {
  if (mode !== "demo") return { eligible: false, title: "Live AI unavailable",
    detail: "This walkthrough is available only in synthetic demo mode." };
  if (!demoScenarios[settings.feature]?.some(({ id }) => id === settings.scenario) ||
      !Number.isFinite(settings.threshold) || settings.threshold < 0.5 || settings.threshold > 0.9 ||
      !Number.isInteger(state.step) || state.step < 0 || state.step > 3 ||
      typeof settings.consent !== "boolean" || typeof settings.liveness !== "boolean") {
    throw new Error("Invalid synthetic walkthrough configuration");
  }
  if (state.step === 0) return { eligible: false, title: "Ready to demonstrate",
    detail: "Advance the scripted observations to see this workflow." };
  if (settings.feature === "face") {
    if (!settings.consent) return { eligible: false, title: "Blocked: no simulated opt-in",
      detail: "A real attendance workflow must verify recorded, current opt-in first." };
    if (!settings.liveness || settings.scenario === "spoof") return { eligible: false,
      title: "Blocked: liveness prerequisite",
      detail: "Failed or missing liveness prevents an attendance candidate." };
    if (settings.scenario === "unknown") return { eligible: false, title: "No candidate",
      detail: "An unknown subject does not become an attendance record." };
  }
  if (settings.feature === "person" && settings.scenario !== "entry") {
    return { eligible: false, title: settings.scenario === "empty" ? "No people in the fixture" : "People outside the zone",
      detail: "No restricted-zone review is queued for this scenario." };
  }
  if (settings.feature === "weapon") {
    if (settings.scenario === "tool") return { eligible: false, title: "Ordinary tool fixture",
      detail: "The scripted tool class does not create a weapon review." };
    const score = settings.scenario === "uncertain" ? 0.62 : 0.84;
    if (score < settings.threshold) return { eligible: false, title: "Below the demo threshold",
      detail: `Scripted score ${Math.round(score * 100)}% is below the selected threshold. No review is queued.` };
  }
  if (state.step < 3) return { eligible: false, title: "Observations in progress",
    detail: settings.feature === "weapon" ? `${state.step} of 3 scripted consistent observations. Human review follows confirmation.` :
      `${state.step} of 3 walkthrough steps complete.` };
  return { eligible: true,
    title: settings.feature === "face" ? "Simulated attendance candidate" :
      settings.feature === "weapon" ? "Simulated weapon review" : "Simulated zone-entry review",
    detail: "Fictional result for this presentation. A person reviews the candidate; no automatic action is taken." };
}

export function transitionDemo(mode: DemoMode, settings: DemoSettings, state: DemoState, action: DemoAction): DemoState {
  if (mode !== "demo" || action.type === "reset") return initialDemoState();
  demoOutcome(mode, settings, state);
  if (action.type === "advance") return { ...state, step: Math.min(3, state.step + 1) };
  if (!demoOutcome(mode, settings, state).eligible || state.review !== "pending") return state;
  return { ...state, review: action.decision };
}

export function initialDemoView(search: string, mode: DemoMode): "vision" | "dashboard" {
  return mode === "demo" && new URLSearchParams(search).get("demo") === "vision" ? "vision" : "dashboard";
}
