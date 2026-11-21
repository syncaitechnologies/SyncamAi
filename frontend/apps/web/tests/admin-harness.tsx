// Dev-only, synthetic UI fixture for the owner admin screen. It never
// contacts the real API: the "api" below keeps two fake workspaces in memory.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { AdminWorkspaces } from "../src/AdminWorkspaces";
import type { AdminChanges, AdminWorkspace } from "../src/admin-model";
import type { SyncamApi } from "../src/syncam-api";
import "../src/styles.css";
import "../src/live.css";

const now = Date.now();
const initial: AdminWorkspace[] = [
  { tenant_id: "fixture-1", name: "Acme Security", slug: "acme-security-1a2b3c4d", plan: "trial",
    trial_ends_at: new Date(now + 5 * 86_400_000).toISOString(), site_limit: 1, camera_limit: 10, live_view_minutes: 10,
    live_view_seconds: 372, sites: 1, cameras: 4, created_at: new Date(now - 9 * 86_400_000).toISOString() },
  { tenant_id: "fixture-2", name: "SyncAi Technologies", slug: "syncai-technologies", plan: "active",
    trial_ends_at: null, site_limit: null, camera_limit: null, live_view_minutes: null,
    live_view_seconds: 318, sites: 1, cameras: 1, created_at: new Date(now - 80 * 86_400_000).toISOString() },
];

function Fixture() {
  const [log, setLog] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const api = useMemo(() => {
    let workspaces = initial.map((workspace) => ({ ...workspace }));
    return {
      async adminWorkspaces() { return workspaces.map((workspace) => ({ ...workspace })); },
      async updateAdminWorkspace(id: string, changes: AdminChanges) {
        setLog((current) => [...current, `PATCH ${id} ${JSON.stringify(changes)}`]);
        workspaces = workspaces.map((workspace) => (workspace.tenant_id === id ? { ...workspace, ...changes } as AdminWorkspace : workspace));
      },
    } as unknown as SyncamApi;
  }, []);
  return <>
    <aside style={{ padding: 16 }}>
      <strong>Synthetic owner admin fixture — no live API requests</strong>
      <p role="status">Toast: {toast || "none"}</p>
      <ol>{log.map((entry, index) => <li key={index}><code>{entry}</code></li>)}</ol>
    </aside>
    <AdminWorkspaces api={api} onNotify={setToast} />
  </>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
