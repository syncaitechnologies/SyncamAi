import { useEffect, useMemo, useState } from "react";
import type { Session } from "@supabase/supabase-js";

import { App } from "./App";
import {
  createSupabaseAuthClient,
  currentAuthRedirect,
  readAuthRuntimeConfig,
  ssoDomainFromEmail,
  syncAccessToken,
} from "./supabase-auth";

type AuthState = "loading" | "signed_out" | "error";

function AuthScreen({ message }: { message?: string }) {
  const config = useMemo(() => readAuthRuntimeConfig(), []);
  const client = useMemo(
    () => (config.mode === "configured" ? createSupabaseAuthClient(config) : null),
    [config],
  );
  const [state, setState] = useState<AuthState>("loading");
  const [session, setSession] = useState<Session | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [notice, setNotice] = useState(message ?? "");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!client) {
      syncAccessToken(null);
      setState("error");
      return;
    }

    let active = true;
    const updateSession = (next: Session | null) => {
      syncAccessToken(next);
      if (!active) return;
      setSession(next);
      setState(next ? "loading" : "signed_out");
    };
    const { data: listener } = client.auth.onAuthStateChange((_event, next) => {
      updateSession(next);
    });

    void client.auth.getSession().then(({ data, error }) => {
      if (!active) return;
      if (error) {
        syncAccessToken(null);
        setNotice("Your session could not be restored. Please sign in again.");
        setState("signed_out");
        return;
      }
      updateSession(data.session);
    });

    return () => {
      active = false;
      listener.subscription.unsubscribe();
    };
  }, [client]);

  async function signIn(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!client) return;
    setSubmitting(true);
    setNotice("");
    const { error } = await client.auth.signInWithPassword({ email, password });
    setSubmitting(false);
    if (error) {
      setPassword("");
      setNotice("Sign-in was not successful. Check your details and try again.");
    }
  }

  async function signInWithSso() {
    if (!client) return;
    const domain = ssoDomainFromEmail(email);
    if (!domain) {
      setNotice("Enter your work email to continue with single sign-on.");
      return;
    }
    setSubmitting(true);
    setNotice("");
    const { error } = await client.auth.signInWithSSO({
      domain,
      options: { redirectTo: currentAuthRedirect() },
    });
    setSubmitting(false);
    if (error) setNotice("Single sign-on is not available for this email domain.");
  }

  async function signOut() {
    if (!client) return;
    setNotice("");
    const { error } = await client.auth.signOut();
    if (error) setNotice("Sign-out could not be completed. Please try again.");
  }

  if (config.mode === "demo") return <App />;
  if (config.mode === "misconfigured") {
    return <AuthConfigurationRequired message={config.message} />;
  }
  if (session) {
    return <App userEmail={session.user.email} onSignOut={() => void signOut()} />;
  }
  if (state === "loading") {
    return (
      <main className="auth-shell" aria-busy="true">
        <p>Restoring your secure session…</p>
      </main>
    );
  }

  return (
    <main className="auth-shell">
      <section className="auth-card" aria-labelledby="sign-in-title">
        <span className="auth-mark" aria-hidden="true">SC</span>
        <p className="eyebrow">SyncCam AI operations</p>
        <h1 id="sign-in-title">Sign in to your workspace</h1>
        <p className="auth-copy">
          Use the account provisioned by your organization. Workspace access is
          verified by the SyncCam API after sign-in.
        </p>
        <form onSubmit={signIn} className="auth-form">
          <label>
            Work email
            <input
              autoComplete="email"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </label>
          <label>
            Password
            <input
              autoComplete="current-password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          <button type="submit" disabled={submitting} className="auth-primary">
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>
        <div className="auth-divider" aria-hidden="true"><span>or</span></div>
        <button
          type="button"
          disabled={submitting}
          onClick={() => void signInWithSso()}
          className="auth-secondary"
        >
          Continue with single sign-on
        </button>
        {notice && <p className="auth-notice" role="alert">{notice}</p>}
      </section>
    </main>
  );
}

function AuthConfigurationRequired({ message }: { message: string }) {
  return (
    <main className="auth-shell">
      <section className="auth-card" aria-labelledby="auth-config-title">
        <span className="auth-mark" aria-hidden="true">SC</span>
        <p className="eyebrow">SyncCam AI operations</p>
        <h1 id="auth-config-title">Authentication needs configuration</h1>
        <p className="auth-copy">{message}</p>
        <p className="auth-notice" role="alert">
          Add only the project URL and publishable key to the web app’s local
          environment. Never add a service-role key, database password, or
          access token to frontend configuration.
        </p>
      </section>
    </main>
  );
}

export function AuthGate() {
  return <AuthScreen />;
}
