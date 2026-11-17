import { useMemo, useRef, useState } from "react";
import type { Session } from "@supabase/supabase-js";

import { readAlertRuntimeConfig } from "./alert-client";
import { registerWorkspace, validateWorkspaceDraft, type WorkspaceDraft } from "./signup-model";
import type { AuthClient } from "./supabase-auth";

// First-run screen for a verified user with no workspace yet (ADR-016): name
// the company and first site, then the API creates the workspace and makes
// this user its administrator. A refreshed token then carries the new claims.

function timezones(): string[] {
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf?.("timeZone");
  return supported?.length ? supported : ["UTC"];
}

export function WorkspaceSetup({ client, session, emailVerified, onSignOut }: {
  client: AuthClient;
  session: Session;
  emailVerified: boolean;
  onSignOut: () => void;
}) {
  const zones = useMemo(timezones, []);
  const [draft, setDraft] = useState<WorkspaceDraft>(() => ({
    company: "",
    siteName: "",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  }));
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // One key per attempt series, so a double-click is one request.
  const idempotencyKey = useRef(crypto.randomUUID());

  function update(field: keyof WorkspaceDraft, value: string) {
    setDraft((current) => ({ ...current, [field]: value }));
    setNotice("");
  }

  async function create(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const issue = validateWorkspaceDraft(draft);
    if (issue) {
      setNotice(issue);
      return;
    }
    setSubmitting(true);
    setNotice("");
    try {
      await registerWorkspace({
        baseUrl: readAlertRuntimeConfig().apiBaseUrl,
        accessToken: session.access_token,
        draft,
        idempotencyKey: idempotencyKey.current,
      });
      // The refreshed token carries the workspace; the session listener then
      // continues to authenticator setup and the dashboard.
      const { error } = await client.auth.refreshSession();
      if (error) throw new Error("Your workspace is ready. Sign out and sign in again to open it.");
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "We could not create your workspace. Try again.");
      idempotencyKey.current = crypto.randomUUID();
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-shell">
      <section className="auth-card" aria-labelledby="workspace-title">
        <span className="auth-mark" aria-hidden="true">SC</span>
        <p className="eyebrow">Step 2 of 3 · Your workspace</p>
        <h1 id="workspace-title">Set up your company</h1>
        {!emailVerified ? (
          <>
            <p className="auth-notice" role="alert">Your email address is not verified yet. Sign out, sign in again and enter the code we emailed you.</p>
            <button type="button" className="auth-secondary auth-exit" onClick={onSignOut}>Sign out</button>
          </>
        ) : (
          <>
            <p className="auth-copy">Your 14-day free trial includes one site and up to 10 cameras. You can rename things later.</p>
            <form className="auth-form" onSubmit={create} noValidate>
              <label>Company name<input autoComplete="organization" value={draft.company} maxLength={120} onChange={(event) => update("company", event.target.value)} required /></label>
              <label>First site name<input value={draft.siteName} maxLength={120} placeholder="Head office" onChange={(event) => update("siteName", event.target.value)} required /></label>
              <label>Timezone of the site
                <select value={draft.timezone} onChange={(event) => update("timezone", event.target.value)}>
                  {!zones.includes(draft.timezone) && <option value={draft.timezone}>{draft.timezone}</option>}
                  {zones.map((zone) => <option key={zone} value={zone}>{zone}</option>)}
                </select>
              </label>
              <p className="auth-hint">Data region: Mumbai, India (ap-south-1).</p>
              <button type="submit" className="auth-primary" disabled={submitting}>{submitting ? "Creating your workspace…" : "Create workspace"}</button>
            </form>
            <p className="auth-hint">Next you will set up an authenticator app for secure sign-in, then add your cameras.</p>
            <button type="button" className="auth-secondary auth-exit" disabled={submitting} onClick={onSignOut}>Sign out</button>
          </>
        )}
        {notice && <p className="auth-notice" role="alert">{notice}</p>}
      </section>
    </main>
  );
}
