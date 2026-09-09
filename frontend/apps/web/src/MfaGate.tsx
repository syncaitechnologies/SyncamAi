import { useEffect, useRef, useState } from "react";
import type { Session } from "@supabase/supabase-js";

import { App } from "./App";
import { inspectSession, type SessionDecision } from "./auth-session";
import { syncAccessToken, type AuthClient } from "./supabase-auth";

type Enrollment = { id: string; qr: string; secret: string };

export function MfaGate({ client, session, onSignOut, notice, signingOut }: {
  client: AuthClient;
  session: Session;
  onSignOut: () => void;
  notice: string;
  signingOut: boolean;
}) {
  const [result, setResult] = useState<{ token: string; decision: SessionDecision } | null>(null);
  const [retry, setRetry] = useState(0);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    syncAccessToken(null);
    setResult(null);
    setError("");
    if (signingOut) return () => { active = false; syncAccessToken(null); };
    void inspectSession(client, session).then((decision) => {
      if (!active) return;
      if (decision.kind === "ready") syncAccessToken(session);
      setResult({ token: session.access_token, decision });
    }).catch(() => {
      if (active) setError("We could not verify your session securely. Check your connection and retry, or sign out.");
    });
    return () => { active = false; syncAccessToken(null); };
  }, [client, session.access_token, session.user.id, retry, signingOut]);

  const decision = result?.token === session.access_token ? result.decision : null;
  if (decision?.kind === "ready" && !signingOut) {
    return <>
      {notice && <p className="auth-session-notice" role="alert">{notice}</p>}
      <App key={`${session.user.id}:${decision.tenantId}`} userEmail={session.user.email} onSignOut={onSignOut} />
    </>;
  }
  return <main className="auth-shell">
    <section className="auth-card" aria-labelledby="mfa-title">
      <span className="auth-mark" aria-hidden="true">SC</span>
      <p className="eyebrow">Secure workspace access</p>
      <h1 id="mfa-title">{decision?.kind === "enroll" ? "Set up your authenticator" : decision?.kind === "challenge" ? "Verify your sign-in" : "Checking workspace access"}</h1>
      {(!decision && !error) || signingOut ? <p className="auth-copy" role="status">{signingOut ? "Signing out…" : "Verifying your session and security requirements…"}</p> : null}
      {decision?.kind === "blocked" && <p className="auth-notice" role="alert">{decision.message}</p>}
      {(decision?.kind === "enroll" || decision?.kind === "challenge") && !signingOut && <MfaForm
        key={session.user.id}
        client={client}
        decision={decision}
        onVerified={() => setRetry((value) => value + 1)}
      />}
      {error && <><p className="auth-notice" role="alert">{error}</p><button className="auth-primary" onClick={() => setRetry((value) => value + 1)}>Retry session check</button></>}
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
      <button className="auth-secondary auth-exit" disabled={signingOut} onClick={onSignOut}>Sign out</button>
    </section>
  </main>;
}

function MfaForm({ client, decision, onVerified }: {
  client: AuthClient;
  decision: Extract<SessionDecision, { kind: "enroll" | "challenge" }>;
  onVerified: () => void;
}) {
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const [selected, setSelected] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const factors = decision.kind === "challenge" ? decision.factors : [];
  const factorId = enrollment?.id ?? (factors.some((factor) => factor.id === selected) ? selected : factors[0]?.id);

  async function enroll() {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const { data, error: failure } = await client.auth.mfa.enroll({ factorType: "totp", issuer: "SyncCam AI" });
      if (failure) throw failure;
      if (alive.current) setEnrollment({ id: data.id, qr: data.totp.qr_code, secret: data.totp.secret });
    } catch {
      if (alive.current) setError("Authenticator setup failed. Retry or contact your administrator if your factor limit has been reached.");
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  }

  async function verify(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!factorId || inFlight.current || !/^\d{6}$/.test(code)) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const { error: failure } = await client.auth.mfa.challengeAndVerify({ factorId, code });
      if (failure) throw failure;
      // Supabase emits MFA_CHALLENGE_VERIFIED with the new token. The parent
      // checks that token again; a successful button handler never unlocks App.
      if (alive.current) { setCode(""); setEnrollment(null); onVerified(); }
    } catch {
      if (alive.current) { setCode(""); setError("Verification was not successful. Enter a fresh code from your authenticator and try again."); }
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  }

  return <>
    <p className="auth-copy">{decision.kind === "enroll" ? "Your role requires multi-factor authentication. Add SyncCam AI to your authenticator app, then verify a code to continue." : "Enter the current six-digit code from your registered authenticator. MFA cannot be skipped."}</p>
    {decision.kind === "enroll" && !enrollment && <button type="button" className="auth-primary auth-exit" disabled={busy} onClick={() => void enroll()}>{busy ? "Preparing…" : "Set up authenticator"}</button>}
    {enrollment && <div className="auth-enrollment">
      <img className="auth-qr" src={enrollment.qr} alt="Scan this private setup QR code with your authenticator app" />
      <details><summary>Can’t scan? Enter the setup key manually</summary><code className="auth-secret">{enrollment.secret}</code></details>
      <p className="auth-copy">Keep this setup key private. It is shown only during setup and is not saved by the web app.</p>
    </div>}
    {factorId && <form className="auth-form" onSubmit={verify}>
      {factors.length > 1 && <label>Authenticator<select value={factorId} disabled={busy} onChange={(event) => setSelected(event.target.value)}>{factors.map((factor, index) => <option key={factor.id} value={factor.id}>{factor.friendly_name || `Authenticator ${index + 1}`}</option>)}</select></label>}
      <label>Authenticator code<input autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={code} disabled={busy} onChange={(event) => setCode(event.target.value.replace(/\D/g, "").slice(0, 6))} required /></label>
      <button className="auth-primary" disabled={busy || code.length !== 6}>{busy ? "Verifying…" : "Verify and continue"}</button>
    </form>}
    {error && <p className="auth-notice" role="alert">{error}</p>}
    <p className="auth-notice">Lost your authenticator? Contact your organization administrator for identity-verified recovery. This screen cannot remove an existing factor.</p>
  </>;
}
