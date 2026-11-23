// Dev-only, synthetic UI fixture for Find cameras. It never contacts the real
// API: the "api" below returns one fake recorder from a scan.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { CameraDiscovery } from "../src/CameraDiscovery";
import type { SyncamApi } from "../src/syncam-api";
import "../src/styles.css";
import "../src/live.css";

const api = {
  async setupBox() { return { paired: true, online: true }; },
  async runSetupTask() {
    return { devices: [{ device_id: "fixture-recorder", ip: "10.1.10.6", rtsp_port: 554, ports: [80, 554], family: null, brand: "Unknown brand", name: "", model: "", onvif: false }] };
  },
} as unknown as SyncamApi;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <div style={{ maxWidth: 560, padding: 24 }}>
      <CameraDiscovery api={api} siteId="fixture-site" timezone="America/New_York" onNotify={() => undefined} onAdded={() => undefined} onClose={() => undefined} />
    </div>
  </StrictMode>,
);
