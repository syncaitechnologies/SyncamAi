import { useEffect, useMemo, useState } from "react";
import type { Session } from "@supabase/supabase-js";

import { App } from "./App";
import {
  ForgotPassword,
  NewPasswordForm,
  ResetCode,
  SignInForm,
  SignUpForm,
  VerifyEmailCode,
  type AuthView,
} from "./AuthForms";
import { MfaGate } from "./MfaGate";
import { observeSession } from "./auth-session";
import {
  createSupabaseAuthClient,
  readAuthRuntimeConfig,
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
  const [notice, setNotice] = useState(message ?? "");
  const [signingOut, setSigningOut] = useState(false);
  const [view, setView] = useState<AuthView>(() =>
    typeof window !== "undefined" && new URLSearchParams(window.location.search).get("signup") !== null
      ? { name: "sign-up" } : { name: "sign-in" });
  // A password-reset session must set a new password before the dashboard opens.
  const [recovering, setRecovering] = useState(false);

  useEffect(() => {
    if (!client) return;
    const { data } = client.auth.onAuthStateChange((event) => {
      if (event === "PASSWORD_RECOVERY") setRecovering(true);
    });
    return () => data.subscription.unsubscribe();
  }, [client]);

  useEffect(() => {
    if (!client) {
      syncAccessToken(null);
      setState("error");
      return;
    }

    syncAccessToken(null);
    let lastAccessToken: string | undefined;
    const stop = observeSession(client, (next) => {
      if (!next || next.access_token !== lastAccessToken) syncAccessToken(null);
      lastAccessToken = next?.access_token;
      setSession(next);
      if (!next) setRecovering(false);
      setState(next ? "loading" : "signed_out");
    }, () => {
      syncAccessToken(null);
      setSession(null);
      setNotice("Your session could not be restored. Please sign in again.");
      setState("signed_out");
    });

    return () => {
      stop();
      syncAccessToken(null);
    };
  }, [client]);

  async function signOut() {
    if (!client || signingOut) return;
    setSigningOut(true);
    setNotice("");
    syncAccessToken(null);
    try {
      const { error } = await client.auth.signOut();
      if (error) throw error;
      setSession(null);
      setRecovering(false);
      setView({ name: "sign-in" });
      setState("signed_out");
    } catch {
      setNotice("Sign-out could not be completed. Check your connection and retry. Your provider session may still be active.");
    } finally {
      setSigningOut(false);
    }
  }

  if (config.mode === "demo") return <App />;
  if (config.mode === "misconfigured") {
    return <AuthConfigurationRequired message={config.message} />;
  }
  if (session && client && recovering) {
    return <NewPasswordForm client={client} onDone={() => setRecovering(false)} onSignOut={() => void signOut()} />;
  }
  if (session && client) {
    return <MfaGate key={session.user.id} client={client} session={session} notice={notice} signingOut={signingOut} onSignOut={() => void signOut()} />;
  }
  if (state === "loading") {
    return (
      <main className="auth-shell" aria-busy="true">
        <p>Restoring your secure session…</p>
      </main>
    );
  }
  if (!client) return <AuthConfigurationRequired message="Authentication is not available." />;

  const go = (next: AuthView) => { setNotice(""); setView(next); };
  switch (view.name) {
    case "sign-up":
      return <SignUpForm client={client} go={go} />;
    case "verify":
      return <VerifyEmailCode client={client} go={go} email={view.email} />;
    case "forgot":
      return <ForgotPassword client={client} go={go} />;
    case "reset-code":
      return <ResetCode client={client} go={go} email={view.email} onRecovering={setRecovering} />;
    default:
      return <SignInForm key={notice} client={client} go={go} notice={notice || view.notice} />;
  }
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
