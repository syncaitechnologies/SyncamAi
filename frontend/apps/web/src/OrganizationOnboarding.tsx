import { useState } from "react";

import { Icon } from "./icon";
import {
  completedOnboardingSteps,
  initialOnboardingDraft,
  onboardingStepLabel,
  onboardingSteps,
  type OnboardingDraft,
  validateOnboardingStep,
} from "./onboarding-model";

type OrganizationOnboardingProps = { dataMode: "demo" | "live" };

const regions = ["Mumbai, India", "Singapore", "Frankfurt, Germany", "Virginia, USA"];
const timezones = ["Asia/Kolkata", "Asia/Singapore", "Europe/Berlin", "America/New_York"];

export function OrganizationOnboarding({ dataMode }: OrganizationOnboardingProps) {
  const [draft, setDraft] = useState<OnboardingDraft>(initialOnboardingDraft);
  const [stepIndex, setStepIndex] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const currentStep = onboardingSteps[stepIndex] ?? "organization";
  const completed = completedOnboardingSteps(draft);

  function update(field: keyof OnboardingDraft, value: string) {
    setDraft((current) => ({ ...current, [field]: value }));
    setMessage(null);
  }

  function next() {
    const issue = validateOnboardingStep(currentStep, draft);
    if (issue) {
      setMessage(issue);
      return;
    }
    if (currentStep === "review") {
      setMessage("Request reviewed locally. No organization, site, role, or invitation has been created.");
      return;
    }
    setStepIndex((index) => index + 1);
    setMessage(null);
  }

  function back() {
    setStepIndex((index) => Math.max(0, index - 1));
    setMessage(null);
  }

  return (
    <section className="onboarding-page" aria-labelledby="onboarding-title">
      <header className="onboarding-heading">
        <div>
          <p className="dashboard-overline">Organization setup · {completed}/3 details ready</p>
          <h1 id="onboarding-title">Prepare your first workspace</h1>
          <p>Capture the organization, first site, and first administrator needed for a controlled provisioning request.</p>
        </div>
        <span className={dataMode === "live" ? "onboarding-mode live" : "onboarding-mode"}>
          <Icon name="shield" size={14} />
          {dataMode === "live" ? "Live safeguards" : "Demo safeguards"}
        </span>
      </header>
      <aside className="onboarding-boundary" role="note">
        <Icon name="shield" size={16} />
        <div>
          <strong>Provisioning is intentionally not connected yet.</strong>
          <p>
            This browser flow does not create a tenant, site, user, role, or invitation.
            Those changes need a dedicated Go provisioning API with audit and authorization controls.
          </p>
        </div>
      </aside>
      <div className="onboarding-layout">
        <ol className="onboarding-steps" aria-label="Organization setup steps">
          {onboardingSteps.map((step, index) => (
            <li key={step} className={index === stepIndex ? "active" : index < stepIndex ? "complete" : ""}>
              <span>{index < stepIndex ? <Icon name="check" size={13} /> : index + 1}</span>
              <strong>{onboardingStepLabel(step)}</strong>
            </li>
          ))}
        </ol>
        <form className="onboarding-card" onSubmit={(event) => { event.preventDefault(); next(); }}>
          {currentStep === "organization" && (
            <fieldset>
              <legend>Organization details</legend>
              <p>Use the legal or operating name that the provisioning team should verify.</p>
              <label htmlFor="onboarding-organization">Organization name</label>
              <input id="onboarding-organization" autoComplete="organization" value={draft.organizationName} onChange={(event) => update("organizationName", event.target.value)} />
              <label htmlFor="onboarding-region">Development region</label>
              <select id="onboarding-region" value={draft.region} onChange={(event) => update("region", event.target.value)}>
                <option value="">Choose a region</option>
                {regions.map((region) => <option key={region} value={region}>{region}</option>)}
              </select>
            </fieldset>
          )}
          {currentStep === "site" && (
            <fieldset>
              <legend>First site</legend>
              <p>A site is the first tenant-scoped operational location. It will be created only by the approved backend workflow.</p>
              <label htmlFor="onboarding-site">Site name</label>
              <input id="onboarding-site" autoComplete="off" value={draft.siteName} onChange={(event) => update("siteName", event.target.value)} />
              <label htmlFor="onboarding-timezone">Timezone</label>
              <select id="onboarding-timezone" value={draft.timezone} onChange={(event) => update("timezone", event.target.value)}>
                {timezones.map((timezone) => <option key={timezone} value={timezone}>{timezone}</option>)}
              </select>
            </fieldset>
          )}
          {currentStep === "invite" && (
            <fieldset>
              <legend>First team member</legend>
              <p>The email and role stay in this page until an audited invitation service is available.</p>
              <label htmlFor="onboarding-invite-email">Work email</label>
              <input id="onboarding-invite-email" autoComplete="email" type="email" value={draft.inviteEmail} onChange={(event) => update("inviteEmail", event.target.value)} />
              <label htmlFor="onboarding-invite-role">Requested role</label>
              <select id="onboarding-invite-role" value={draft.inviteRole} onChange={(event) => update("inviteRole", event.target.value)}>
                <option>Site administrator</option>
                <option>Operator</option>
                <option>Auditor</option>
              </select>
            </fieldset>
          )}
          {currentStep === "review" && (
            <section className="onboarding-review" aria-labelledby="onboarding-review-title">
              <p className="dashboard-overline">Review request</p>
              <h2 id="onboarding-review-title">Ready for controlled provisioning</h2>
              <dl>
                <div><dt>Organization</dt><dd>{draft.organizationName}</dd></div>
                <div><dt>Development region</dt><dd>{draft.region}</dd></div>
                <div><dt>First site</dt><dd>{draft.siteName} · {draft.timezone}</dd></div>
                <div><dt>First team member</dt><dd>{draft.inviteEmail} · {draft.inviteRole}</dd></div>
              </dl>
            </section>
          )}
          {message && <p className="onboarding-message" role="alert">{message}</p>}
          <footer>
            <button className="onboarding-secondary" type="button" disabled={stepIndex === 0} onClick={back}>Back</button>
            <button className="onboarding-primary" type="submit">
              {currentStep === "review" ? "Review locally" : "Continue"}
              <Icon name={currentStep === "review" ? "check" : "arrow"} size={14} />
            </button>
          </footer>
        </form>
      </div>
    </section>
  );
}
