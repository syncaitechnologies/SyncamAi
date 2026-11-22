// Dev-only, synthetic UI fixture for Configuration > Site box (restart button).
// It never contacts the real API. ?box=offline shows an offline box and
// ?box=old a box too old to restart remotely.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { SiteSettings } from "../src/SiteSettings";
import { restartFailure } from "../src/site-box-model";
import type { SiteKey, SyncamApi } from "../src/syncam-api";
import type { WorkspaceData } from "../src/use-workspace";
import { storeWorkspace } from "../src/workspace";
import "../src/styles.css";
import "../src/live.css";

const mode = new URLSearchParams(window.location.search).get("box") ?? "online";
storeWorkspace({ tenantId: "tenant-fixture", siteIds: [], roles: ["super_admin"] });
const site = { id: "site-fixture", name: "Fixture warehouse", timezone: "America/Toronto", status: "active" };
const seen = new Date(Date.now() - (mode === "offline" ? 45 : 1) * 60_000).toISOString();
const key: SiteKey = { id: "key-1", site_id: site.id, label: "Front desk PC", created_at: seen, last_seen_at: seen, camera_status: {}, box_public_key: "PEM" };

function Fixture() {
  const [log, setLog] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const api = useMemo(() => ({
    siteId: site.id,
    async listSiteKeys() { return [key]; },
    async listTelegram() { return { targets: [], bot: "SyncCamFixtureBot" }; },
    async restartSiteBox(siteId: string) {
      setLog((current) => [...current, `restart ${siteId}`]);
      await new Promise((resolve) => setTimeout(resolve, 800));
      if (mode === "old") throw new Error(restartFailure("unknown setup task 'restart'"));
    },
  }) as unknown as SyncamApi, []);
  const workspace: WorkspaceData = {
    api, sites: [site], site, cameras: [], zones: [], names: { cameras: {}, zones: {} },
    loading: false, error: "", errorCode: "", reload: () => {},
  };
  return <>
    <aside style={{ padding: 16 }}>
      <strong>Synthetic site box fixture ({mode}) — no live API requests</strong>
      <p role="status">Toast: {toast || "none"}</p>
      <ol>{log.map((entry, index) => <li key={index}><code>{entry}</code></li>)}</ol>
    </aside>
    <SiteSettings workspace={workspace} onNotify={setToast} />
  </>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
