import { readFileSync } from "node:fs";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

function localPhoneDemoTls() {
  const certificatePath = process.env.SYNCAM_DEV_TLS_CERTIFICATE;
  const privateKeyPath = process.env.SYNCAM_DEV_TLS_PRIVATE_KEY;
  if (!certificatePath && !privateKeyPath) {
    return undefined;
  }
  if (!certificatePath || !privateKeyPath) {
    throw new Error(
      "Set both SYNCAM_DEV_TLS_CERTIFICATE and SYNCAM_DEV_TLS_PRIVATE_KEY for the local phone demo.",
    );
  }
  return {
    cert: readFileSync(certificatePath),
    key: readFileSync(privateKeyPath),
  };
}

export default defineConfig({
  plugins: [react()],
  server: {
    https: localPhoneDemoTls(),
    proxy: {
      "/v1": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/ws": { target: "ws://127.0.0.1:8080", ws: true },
    },
  },
});
