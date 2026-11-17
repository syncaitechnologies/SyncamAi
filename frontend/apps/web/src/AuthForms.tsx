import { useEffect, useState } from "react";

import { currentAuthRedirect, ssoDomainFromEmail, type AuthClient } from "./supabase-auth";
import {
  MIN_PASSWORD_LENGTH,
  needsAuthenticator,
  normalizeCode,
  passwordUpdateMessage,
  validateEmail,
  validatePassword,
  validateSignUp,
  validCode,
} from "./signup-model";

// Sign-in, sign-up, email-code and password-reset screens (ADR-016). Supabase
// Auth does the work; these screens never see or store anything but what the
// person types.

export type AuthView =
  | { name: "sign-in"; notice?: string }
  | { name: "sign-up" }
  | { name: "verify"; email: string }
  | { name: "forgot" }
  | { name: "reset-code"; email: string };

type FormProps = { client: AuthClient; go: (view: AuthView) => void };

function AuthCard({ eyebrow, title, children }: { eyebrow: string; title: string; children: React.ReactNode }) {
  return (
    <main className="auth-shell">
      <section className="auth-card" aria-labelledby="auth-title">
        <span className="auth-mark" aria-hidden="true">SC</span>
        <p className="eyebrow">{eyebrow}</p>
        <h1 id="auth-title">{title}</h1>
        {children}
      </section>
    </main>
  );
}

function LinkButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
  return <button type="button" className="auth-link" onClick={onClick}>{children}</button>;
}

function legalLink(url: string | undefined, label: string) {
  const href = typeof url === "string" ? url.trim() : "";
  return /^https:\/\//.test(href) ? <a href={href} target="_blank" rel="noopener noreferrer">{label}</a> : label;
}

export function SignInForm({ client, go, notice: initialNotice }: FormProps & { notice?: string }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [notice, setNotice] = useState(initialNotice ?? "");
  const [submitting, setSubmitting] = useState(false);

  async function signIn(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setNotice("");
    try {
      const { error } = await client.auth.signInWithPassword({ email: email.trim(), password });
      if (error) {
        if (/not confirmed/i.test(error.message)) {
          go({ name: "verify", email: email.trim() });
          return;
        }
        throw error;
      }
    } catch {
      setNotice("Sign-in was not successful. Check your email and password, then try again.");
    } finally {
      setPassword("");
      setSubmitting(false);
    }
  }

  async function signInWithSso() {
    const domain = ssoDomainFromEmail(email);
    if (!domain) {
      setNotice("Enter your work email to continue with single sign-on.");
      return;
    }
    setSubmitting(true);
    setNotice("");
    try {
      const { error } = await client.auth.signInWithSSO({ domain, options: { redirectTo: currentAuthRedirect() } });
      if (error) throw error;
    } catch {
      setNotice("Single sign-on could not be started. Check your connection or contact your administrator.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="SyncCam AI" title="Sign in to your workspace">
      <p className="auth-copy">Sign in with the email and password you used to create your SyncCam account.</p>
      <form onSubmit={signIn} className="auth-form">
        <label>Email<input autoComplete="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
        <label>Password<input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
        <button type="submit" disabled={submitting} className="auth-primary">{submitting ? "Signing in…" : "Sign in"}</button>
      </form>
      <LinkButton onClick={() => go({ name: "forgot" })}>Forgot password?</LinkButton>
      <div className="auth-divider" aria-hidden="true"><span>or</span></div>
      <button type="button" disabled={submitting} onClick={() => void signInWithSso()} className="auth-secondary">Continue with single sign-on</button>
      <p className="auth-switch">New to SyncCam? <LinkButton onClick={() => go({ name: "sign-up" })}>Create an account</LinkButton></p>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}

export function SignUpForm({ client, go }: FormProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [consent, setConsent] = useState(false);
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const env = import.meta.env;

  async function signUp(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const issue = validateSignUp({ email, password, consent });
    if (issue) {
      setNotice(issue);
      return;
    }
    setSubmitting(true);
    setNotice("");
    try {
      const { data, error } = await client.auth.signUp({
        email: email.trim(),
        password,
        options: { emailRedirectTo: currentAuthRedirect() },
      });
      if (error) throw error;
      // With email confirmation off (local development) Supabase signs the
      // user in at once; the session listener then shows workspace setup.
      if (!data.session) go({ name: "verify", email: email.trim() });
    } catch (error) {
      const message = error instanceof Error ? error.message : "";
      setNotice(/password/i.test(message)
        ? "That password was not accepted. Choose a longer one that you do not use anywhere else."
        : "Your account could not be created. Check your connection and try again.");
    } finally {
      setPassword("");
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="SyncCam AI" title="Create your account">
      <p className="auth-copy">Start a free 14-day trial: one site and up to 10 cameras. No card needed.</p>
      <form onSubmit={signUp} className="auth-form" noValidate>
        <label>Work email<input autoComplete="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
        <label>Password
          <input autoComplete="new-password" type="password" minLength={MIN_PASSWORD_LENGTH} value={password} onChange={(event) => setPassword(event.target.value)} required />
          <small className="auth-hint">At least {MIN_PASSWORD_LENGTH} characters. A short sentence or a few random words is easy to remember.</small>
        </label>
        <label className="auth-check">
          <input type="checkbox" checked={consent} onChange={(event) => setConsent(event.target.checked)} />
          <span>I agree to the {legalLink(env.VITE_SYNCAM_TERMS_URL as string | undefined, "Terms of Service")} and the {legalLink(env.VITE_SYNCAM_PRIVACY_URL as string | undefined, "Privacy Policy")}.</span>
        </label>
        <button type="submit" disabled={submitting} className="auth-primary">{submitting ? "Creating account…" : "Create account"}</button>
      </form>
      <p className="auth-switch">Already have an account? <LinkButton onClick={() => go({ name: "sign-in" })}>Sign in</LinkButton></p>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}

export function VerifyEmailCode({ client, go, email }: FormProps & { email: string }) {
  const [code, setCode] = useState("");
  const [notice, setNotice] = useState("");
  const [status, setStatus] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function verify(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validCode(code)) return;
    setSubmitting(true);
    setNotice("");
    try {
      const { error } = await client.auth.verifyOtp({ email, token: code, type: "signup" });
      if (error) throw error;
      // The new session moves the person on to workspace setup.
    } catch {
      setCode("");
      setNotice("That code did not work. It may have expired: request a new one and enter the latest code.");
    } finally {
      setSubmitting(false);
    }
  }

  async function resend() {
    setSubmitting(true);
    setNotice("");
    setStatus("");
    try {
      const { error } = await client.auth.resend({ type: "signup", email, options: { emailRedirectTo: currentAuthRedirect() } });
      if (error) throw error;
      setStatus("A new code is on its way. Check your inbox and spam folder.");
    } catch {
      setNotice("A new code could not be sent yet. Wait a minute and try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="Check your email" title="Enter your 6-digit code">
      <p className="auth-copy">We sent a code to <strong>{email}</strong>. If you already have an account with this email, sign in instead.</p>
      <form onSubmit={verify} className="auth-form">
        <label>Verification code<input autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={code} disabled={submitting} onChange={(event) => setCode(normalizeCode(event.target.value))} required /></label>
        <button type="submit" disabled={submitting || !validCode(code)} className="auth-primary">{submitting ? "Checking…" : "Verify email"}</button>
      </form>
      <LinkButton onClick={() => void resend()}>Send a new code</LinkButton>
      <p className="auth-switch"><LinkButton onClick={() => go({ name: "sign-up" })}>Use a different email</LinkButton> · <LinkButton onClick={() => go({ name: "sign-in" })}>Sign in</LinkButton></p>
      {status && <p className="auth-copy" role="status">{status}</p>}
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}

export function ForgotPassword({ client, go }: FormProps) {
  const [email, setEmail] = useState("");
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function request(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const issue = validateEmail(email);
    if (issue) {
      setNotice(issue);
      return;
    }
    setSubmitting(true);
    setNotice("");
    try {
      const { error } = await client.auth.resetPasswordForEmail(email.trim(), { redirectTo: currentAuthRedirect() });
      if (error) throw error;
      go({ name: "reset-code", email: email.trim() });
    } catch {
      setNotice("The reset email could not be sent yet. Wait a minute and try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="Reset password" title="Forgot your password?">
      <p className="auth-copy">Enter your account email. If it belongs to a SyncCam account, we will email you a 6-digit code.</p>
      <form onSubmit={request} className="auth-form" noValidate>
        <label>Email<input autoComplete="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
        <button type="submit" disabled={submitting} className="auth-primary">{submitting ? "Sending…" : "Email me a code"}</button>
      </form>
      <p className="auth-switch"><LinkButton onClick={() => go({ name: "sign-in" })}>Back to sign in</LinkButton></p>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}

export function ResetCode({ client, go, email, onRecovering }: FormProps & { email: string; onRecovering: (active: boolean) => void }) {
  const [code, setCode] = useState("");
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function verify(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validCode(code)) return;
    setSubmitting(true);
    setNotice("");
    // Mark recovery first: the session that verifyOtp creates must lead to the
    // new-password screen, not straight into the dashboard.
    onRecovering(true);
    try {
      const { error } = await client.auth.verifyOtp({ email, token: code, type: "recovery" });
      if (error) throw error;
    } catch {
      onRecovering(false);
      setCode("");
      setNotice("That code did not work. It may have expired: go back and request a new one.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="Reset password" title="Enter your reset code">
      <p className="auth-copy">If <strong>{email}</strong> has a SyncCam account, a 6-digit code is on its way.</p>
      <form onSubmit={verify} className="auth-form">
        <label>Reset code<input autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={code} disabled={submitting} onChange={(event) => setCode(normalizeCode(event.target.value))} required /></label>
        <button type="submit" disabled={submitting || !validCode(code)} className="auth-primary">{submitting ? "Checking…" : "Continue"}</button>
      </form>
      <p className="auth-switch"><LinkButton onClick={() => go({ name: "forgot" })}>Request a new code</LinkButton> · <LinkButton onClick={() => go({ name: "sign-in" })}>Back to sign in</LinkButton></p>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}

export function NewPasswordForm({ client, onDone, onSignOut }: { client: AuthClient; onDone: () => void; onSignOut: () => void }) {
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  // The account's authenticator, when it has one: Supabase then needs its code
  // before it accepts a new password.
  const [factorId, setFactorId] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function loadAuthenticator(): Promise<string | null> {
    const { data, error } = await client.auth.mfa.listFactors();
    if (error) return null;
    const totp = data.all.find((factor) => factor.factor_type === "totp" && factor.status === "verified");
    return totp?.id ?? null;
  }

  useEffect(() => {
    let active = true;
    void loadAuthenticator().then((id) => { if (active && id) setFactorId(id); }).catch(() => {});
    return () => { active = false; };
  }, [client]);

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const issue = validatePassword(password);
    if (issue) {
      setNotice(issue);
      return;
    }
    if (factorId && !validCode(code)) {
      setNotice("Enter the 6-digit code from your authenticator app.");
      return;
    }
    setSubmitting(true);
    setNotice("");
    try {
      if (factorId) {
        const { error } = await client.auth.mfa.challengeAndVerify({ factorId, code });
        setCode("");
        if (error) {
          setNotice("That authenticator code did not work. Enter the current code from the app and try again.");
          return;
        }
      }
      const { error } = await client.auth.updateUser({ password });
      if (error) throw error;
      setPassword("");
      onDone();
    } catch (error) {
      if (needsAuthenticator(error) && !factorId) setFactorId(await loadAuthenticator().catch(() => null));
      setNotice(passwordUpdateMessage(error));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard eyebrow="Reset password" title="Choose a new password">
      <form onSubmit={save} className="auth-form" noValidate>
        <label>New password
          <input autoComplete="new-password" type="password" minLength={MIN_PASSWORD_LENGTH} value={password} onChange={(event) => setPassword(event.target.value)} required />
          <small className="auth-hint">At least {MIN_PASSWORD_LENGTH} characters.</small>
        </label>
        {factorId && <label>Authenticator code
          <input autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={code} disabled={submitting} onChange={(event) => setCode(normalizeCode(event.target.value))} required />
          <small className="auth-hint">Your account uses an authenticator app. Enter the 6-digit code it shows now.</small>
        </label>}
        <button type="submit" disabled={submitting} className="auth-primary">{submitting ? "Saving…" : "Save new password"}</button>
      </form>
      <button type="button" className="auth-secondary auth-exit" onClick={onSignOut}>Sign out</button>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
    </AuthCard>
  );
}
