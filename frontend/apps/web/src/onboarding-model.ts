export type OnboardingStep = "organization" | "site" | "invite" | "review";

export type OnboardingDraft = {
  organizationName: string;
  region: string;
  siteName: string;
  timezone: string;
  inviteEmail: string;
  inviteRole: string;
};

export const onboardingSteps: readonly OnboardingStep[] = ["organization", "site", "invite", "review"];

export const initialOnboardingDraft: OnboardingDraft = {
  organizationName: "",
  region: "",
  siteName: "",
  timezone: "Asia/Kolkata",
  inviteEmail: "",
  inviteRole: "Site administrator",
};

const labels: Record<OnboardingStep, string> = {
  organization: "Organization",
  site: "First site",
  invite: "Invite team member",
  review: "Review",
};
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function onboardingStepLabel(step: OnboardingStep): string { return labels[step]; }

export function validateOnboardingStep(step: OnboardingStep, draft: OnboardingDraft): string | null {
  if (step === "organization") {
    if (draft.organizationName.trim().length < 2) return "Enter an organization name with at least 2 characters.";
    if (!draft.region) return "Choose the development region for this request.";
  }
  if (step === "site") {
    if (draft.siteName.trim().length < 2) return "Enter a first-site name with at least 2 characters.";
    if (!draft.timezone) return "Choose an IANA timezone for the first site.";
  }
  if (step === "invite" && !emailPattern.test(draft.inviteEmail.trim())) return "Enter a valid work email for the first team member.";
  return null;
}

export function completedOnboardingSteps(draft: OnboardingDraft): number {
  return onboardingSteps.slice(0, -1).filter((step) => !validateOnboardingStep(step, draft)).length;
}
