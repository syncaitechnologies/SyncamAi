// Dev-only, synthetic UI fixture for the Features page. It never contacts the
// real API: the "api" below keeps fake features and cameras in memory.
// ?viewer=1 shows the page as a non-owner; ?fail=1 makes every change fail.
import { StrictMode, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { FeaturesWorkspace } from "../src/FeaturesWorkspace";
import type { Feature, FeatureChanges, FeatureKey } from "../src/features-model";
import type { Camera, SyncamApi } from "../src/syncam-api";
import "../src/styles.css";
import "../src/live.css";

const params = new URLSearchParams(window.location.search);
const cameras: Camera[] = [
  { id: "cam-gate", name: "Main gate" },
  { id: "cam-store", name: "Stock room" },
  { id: "cam-yard", name: "Back yard" },
] as Camera[];

function row(key: FeatureKey, overrides: Partial<Feature> = {}): Feature {
  return {
    key, status: "available", per_camera: true, included: true, enabled: true, on: true, cameras: null,
    box_ready: true, activity_30d: 0, updated_at: null, ...overrides,
  };
}

const initial: Feature[] = [
  row("zone_alerts", { activity_30d: 23 }),
  row("loitering", { enabled: false, on: false, activity_30d: 0 }),
  row("attendance", { cameras: ["cam-gate"], activity_30d: 1520 }),
  row("weapon", { status: "preview", included: false, enabled: true, on: false, box_ready: false, activity_30d: 0 }),
  row("insights", { activity_30d: 1240, insights: { table_sessions: 96, table_seconds: 259200 } }),
  row("live_view", { per_camera: false, activity_30d: null, live_view: { limit_minutes: 10, used_seconds: 372 } }),
  row("camera_health", { status: "always_on", per_camera: false, activity_30d: 2 }),
  ...(["fire_smoke", "fall", "fight", "ppe", "number_plates"] as FeatureKey[]).map((key) =>
    row(key, { status: "coming_soon", per_camera: false, included: false, enabled: false, on: false, box_ready: null, activity_30d: null })),
];

function Fixture() {
  const [log, setLog] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const api = useMemo(() => {
    let features = initial.map((feature) => ({ ...feature }));
    return {
      async features() { return features.map((feature) => ({ ...feature })); },
      async updateFeature(key: FeatureKey, changes: FeatureChanges) {
        setLog((current) => [...current, `PATCH /v1/features/${key} ${JSON.stringify(changes)}`]);
        await new Promise((resolve) => setTimeout(resolve, 250));
        if (params.has("fail")) throw Object.assign(new Error("Access denied."), { status: 403, code: "FORBIDDEN" });
        features = features.map((feature) => {
          if (feature.key !== key) return feature;
          const enabled = changes.enabled ?? feature.enabled;
          const next = { ...feature, enabled, on: feature.included && enabled, updated_at: new Date().toISOString() };
          if ("cameras" in changes) next.cameras = changes.cameras ?? null;
          return next;
        });
        return { ...features.find((feature) => feature.key === key)! };
      },
    } as unknown as SyncamApi;
  }, []);
  return <>
    <aside style={{ padding: 16 }}>
      <strong>Synthetic Features fixture — no live API requests</strong>
      <p role="status">Toast: {toast || "none"}</p>
      <ol>{log.map((entry, index) => <li key={index}><code>{entry}</code></li>)}</ol>
    </aside>
    <FeaturesWorkspace
      workspace={{ api, cameras }}
      onNotify={setToast}
      contactEmail="sales@example.test"
      canManage={!params.has("viewer")}
    />
  </>;
}

window.confirm = (message?: string) => {
  document.body.dataset.lastConfirm = message ?? "";
  return true;
};

createRoot(document.getElementById("root")!).render(<StrictMode><Fixture /></StrictMode>);
