// Dev-only, synthetic UI fixture. Not a Vite production entry point and never
// contacts Supabase. It cannot issue a token accepted by the Go API.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import type { Session } from "@supabase/supabase-js";
import type { AuthClient } from "../src/supabase-auth";
import { MfaGate } from "../src/MfaGate";
import "../src/styles.css";

function Fixture() {
  const [scenario, setScenario] = useState("enroll");
  return <><aside style={{ padding: 16 }}>
    <strong>Synthetic MFA fixture — no provider or live API requests</strong>
    <label> Scenario <select value={scenario} onChange={(event) => setScenario(event.target.value)}>
      {["enroll", "challenge", "check-error", "unsupported", "missing-membership"].map((value) => <option key={value}>{value}</option>)}
    </select></label>
    <p>Fixture code: 123456. Any other six-digit code fails. Never use real account details here.</p>
  </aside><Scenario key={scenario} scenario={scenario} /></>;
}

function Scenario({ scenario }: { scenario: string }) {
  const [session, setSession] = useState<Session | null>({ access_token: "test1", user: { id: "synthetic-user", email: "fixture@example.invalid" } } as Session);
  const client = useMemo(() => {
    let hasFactor = scenario === "challenge" || scenario === "unsupported";
    let enrollments = 0;
    return { auth: {
      async getClaims(token: string) {
        if (scenario === "check-error") throw new Error("Synthetic provider outage");
        return { error: null, data: { claims: {
          sub: "synthetic-user", aal: token === "test2" ? "aal2" : "aal1",
          app_metadata: scenario === "missing-membership" ? {} : { syncam: {
            tenant_id: "synthetic-tenant", roles: ["super_admin"], site_ids: [], scopes: ["alerts:read"], data_class: ["metadata"],
          } },
        } } };
      },
      mfa: {
        async listFactors() { return { error: null, data: { all: hasFactor ? [{ id: "synthetic-factor", status: "verified", factor_type: scenario === "unsupported" ? "phone" : "totp" }] : [] } }; },
        async enroll() {
          enrollments += 1;
          if (enrollments > 1) throw new Error("Fixture detected duplicate enrollment");
          return { error: null, data: { id: "synthetic-factor", totp: {
            qr_code: "data:image/svg+xml," + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200"><rect width="200" height="200" fill="white"/><text x="20" y="100">SYNTHETIC QR</text></svg>'),
            secret: "SYNTHETIC-SETUP-KEY-NOT-A-REAL-SECRET",
          } } };
        },
        async challengeAndVerify({ code }: { code: string }) {
          if (code !== "123456") return { error: new Error("Synthetic invalid code"), data: null };
          hasFactor = true;
          setSession({ access_token: "test2", user: { id: "synthetic-user", email: "fixture@example.invalid" } } as Session);
          return { error: null, data: {} };
        },
      },
    } } as unknown as AuthClient;
  }, [scenario]);
  return session ? <MfaGate client={client} session={session} onSignOut={() => setSession(null)} notice="" signingOut={false} /> : <p role="status">Synthetic session signed out.</p>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
