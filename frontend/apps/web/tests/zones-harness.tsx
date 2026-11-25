// Dev-only, synthetic UI fixture for Zones & rules. It never contacts the real
// API: the "api" below keeps fake zones in memory. window.confirm is answered
// "yes" and the question is kept in document.body.dataset.lastConfirm.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { LiveZoneEditor } from "../src/LiveZoneEditor";
import type { Camera, Site, SyncamApi, Zone } from "../src/syncam-api";
import type { WorkspaceData } from "../src/use-workspace";
import { storeWorkspace } from "../src/workspace";
import "../src/styles.css";
import "../src/live.css";

storeWorkspace({ tenantId: "fixture-tenant", siteIds: ["fixture-site"], roles: ["site_admin"] });
window.confirm = (message?: string) => {
  document.body.dataset.lastConfirm = message ?? "";
  return true;
};

const square = { type: "Polygon", coordinates: [[[0.5, 0.1], [0.9, 0.1], [0.9, 0.9], [0.5, 0.9], [0.5, 0.1]]] };
function zone(id: string, name: string, kind: Zone["kind"], enabled: boolean): Zone {
  return { id, name, kind, enabled, camera_id: "cam-1", site_id: "fixture-site", geometry: square, config_version: 1, subject_classes: ["person"] } as unknown as Zone;
}

function Fixture() {
  const [log, setLog] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const [zones, setZones] = useState<Zone[]>([
    zone("z1", "Restricted area", "restricted_zone", false),
    zone("z2", "Desk area", "intrusion", false),
    zone("z3", "Night intrusion (whole view)", "intrusion", true),
  ]);
  const api = useMemo(() => ({
    async cameraReference() { return null; },
    async updateZone(target: Zone, changes: Partial<Zone>) {
      setLog((current) => [...current, `PATCH /v1/zones/${target.id} ${JSON.stringify(changes)}`]);
      setZones((current) => current.map((item) => (item.id === target.id ? { ...item, ...changes } : item)));
      return target;
    },
    async deleteZone(id: string) {
      setLog((current) => [...current, `DELETE /v1/zones/${id}`]);
      setZones((current) => current.filter((item) => item.id !== id));
    },
  }) as unknown as SyncamApi, []);
  const workspace = {
    api, zones, reload: () => undefined,
    cameras: [{ id: "cam-1", name: "Laptop webcam", site_id: "fixture-site", lifecycle_status: "active" }] as unknown as Camera[],
    site: { id: "fixture-site", name: "SyncAi office", timezone: "America/Toronto", status: "active" } as Site,
    sites: [], names: { cameras: {}, zones: {} }, loading: false, error: "", errorCode: undefined,
  } as unknown as WorkspaceData;
  return <>
    <aside style={{ padding: 16 }}>
      <strong>Synthetic Zones &amp; rules fixture — no live API requests</strong>
      <p role="status">Toast: {toast || "none"}</p>
      <ol>{log.map((entry, index) => <li key={index}><code>{entry}</code></li>)}</ol>
    </aside>
    <LiveZoneEditor workspace={workspace} onNotify={setToast} />
  </>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
