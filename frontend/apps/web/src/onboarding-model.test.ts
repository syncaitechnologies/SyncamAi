import assert from "node:assert/strict";
import test from "node:test";

import { completedOnboardingSteps, initialOnboardingDraft, onboardingStepLabel, validateOnboardingStep } from "./onboarding-model.ts";

test("validates each onboarding step without making a provisioning decision", () => {
  assert.equal(validateOnboardingStep("organization", initialOnboardingDraft), "Enter an organization name with at least 2 characters.");
  assert.equal(validateOnboardingStep("site", initialOnboardingDraft), "Enter a first-site name with at least 2 characters.");
  assert.equal(validateOnboardingStep("invite", initialOnboardingDraft), "Enter a valid work email for the first team member.");
});

test("reports complete local request details only when all inputs are valid", () => {
  const complete = { ...initialOnboardingDraft, organizationName: "Northstar Operations", region: "Mumbai, India", siteName: "Pune Distribution Center", inviteEmail: "operator@northstar.example" };
  assert.equal(completedOnboardingSteps(complete), 3);
  assert.equal(validateOnboardingStep("review", complete), null);
  assert.equal(onboardingStepLabel("invite"), "Invite team member");
});
