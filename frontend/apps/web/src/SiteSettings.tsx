import { useEffect, useMemo, useState, type FormEvent } from "react";

import { selectSite } from "./alert-client";
import { CameraDiscovery } from "./CameraDiscovery";
import { Icon } from "./icon";
import { qrMatrix, qrSvgPath } from "./qr";
import type { PairingCode, SiteKey, TelegramTarget } from "./syncam-api";
import { canRestart } from "./site-box-model";
import type { WorkspaceData } from "./use-workspace";
import { canManage, readWorkspace } from "./workspace";

/** Latest signed release of the Windows site box (built by .github/workflows/site-box-installer.yml). */
const BOX_INSTALLER_URL = "https://github.com/syncaitechnologies/SyncamAi/releases/latest/download/SyncCamSetup.exe";

/** A QR code of a link, dark on white so phone cameras read it in either theme. */
function LinkQr({ url }: { url: string }) {
  const { path, size } = useMemo(() => qrSvgPath(qrMatrix(url)), [url]);
  return (
    <svg className="live-qr" viewBox={`0 0 ${size} ${size}`} shapeRendering="crispEdges" role="img" aria-label="QR code that opens the SyncCam bot in Telegram">
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  );
}

function ago(value?: string) {
  if (!value) return "never";
  const minutes = Math.round((Date.now() - Date.parse(value)) / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  if (minutes < 1440) return `${Math.round(minutes / 60)} h ago`;
  return `${Math.round(minutes / 1440)} d ago`;
}

function copy(text: string, notify: (message: string) => void) {
  void navigator.clipboard?.writeText(text).then(
    () => notify("Copied to clipboard"),
    () => notify("Copy failed; select the text and copy it manually"),
  );
}

export function SiteSettings({ workspace, onNotify }: { workspace: WorkspaceData; onNotify: (message: string) => void }) {
  const { api, sites, site, cameras, reload } = workspace;
  const manage = canManage(readWorkspace());
  const [keys, setKeys] = useState<SiteKey[]>([]);
  const [telegram, setTelegram] = useState<{ targets: TelegramTarget[]; bot: string; error: string }>({ targets: [], bot: "", error: "" });
  const [secret, setSecret] = useState("");
  const [link, setLink] = useState<{ code: string; bot: string; expires_at: string } | null>(null);
  const [pairing, setPairing] = useState<PairingCode | null>(null);
  const [finding, setFinding] = useState(false);
  const [restart, setRestart] = useState<"idle" | "confirm" | "sending">("idle");
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!api?.siteId) return;
    const abort = new AbortController();
    api.listSiteKeys(api.siteId, abort.signal).then(setKeys).catch(() => setKeys([]));
    api.listTelegram(api.siteId, abort.signal)
      .then((result) => setTelegram({ ...result, error: "" }))
      .catch((error: unknown) => setTelegram({ targets: [], bot: "", error: error instanceof Error ? error.message : "Telegram unavailable" }));
    return () => abort.abort();
  }, [api, version]);

  if (!api) return null;
  const refresh = () => setVersion((value) => value + 1);
  const run = async (action: () => Promise<void>, failure: string) => {
    setBusy(true);
    try {
      await action();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : failure);
    } finally {
      setBusy(false);
    }
  };
  const cameraStatus: Record<string, { online: boolean; error?: string; seen?: string }> = {};
  for (const key of keys) {
    if (key.revoked_at) continue;
    for (const [cameraId, status] of Object.entries(key.camera_status ?? {})) {
      cameraStatus[cameraId] = { online: status.online, error: status.error, seen: status.last_frame_at };
    }
  }
  const activeKeys = keys.filter((key) => !key.revoked_at);
  const boxOnline = activeKeys.some((key) => key.last_seen_at && Date.now() - Date.parse(key.last_seen_at) < 6 * 60_000);

  async function restartBox() {
    if (!site) return;
    setRestart("sending");
    try {
      await api!.restartSiteBox(site.id);
      onNotify("Site box is restarting. Alerts resume within about a minute.");
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "The site box could not be restarted.");
    } finally {
      setRestart("idle");
      refresh();
    }
  }

  function createSite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") ?? "").trim();
    const timezone = String(form.get("timezone") ?? "").trim();
    void run(async () => {
      const created = await api!.createSite(name, timezone);
      selectSite(created.id);
      onNotify(`Site "${created.name}" created`);
      window.location.reload();
    }, "Site could not be created");
  }

  function addCamera(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const name = String(new FormData(formElement).get("name") ?? "").trim();
    if (!name || !api!.siteId) return;
    void run(async () => {
      await api!.createCamera(api!.siteId, name);
      formElement.reset();
      onNotify(`Camera "${name}" added`);
      reload();
    }, "Camera could not be added");
  }

  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker"><span className="live-pill"><span />LIVE</span><span>Workspace setup</span></div>
          <h1>Configuration</h1>
          <p>Connect a site box to your existing cameras, then choose who gets alerts on Telegram.</p>
        </div>
        <div className="heading-actions">
          <label className="site-filter-label" htmlFor="config-site">Site</label>
          <select id="config-site" value={site?.id ?? ""} onChange={(event) => { selectSite(event.target.value); window.location.reload(); }}>
            {!site && <option value="">Select a site</option>}
            {sites.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.name}</option>)}
          </select>
        </div>
      </section>

      <div className="live-steps">
        <section className="panel live-card">
          <header><span className="live-step">1</span><div><h2>Site</h2><p>A location such as a warehouse, shop or office.</p></div></header>
          {site ? (
            <dl className="live-facts">
              <div><dt>Name</dt><dd>{site.name}</dd></div>
              <div><dt>Timezone</dt><dd>{site.timezone}</dd></div>
              <div><dt>Site ID</dt><dd><code>{site.id}</code></dd></div>
            </dl>
          ) : <p className="live-muted">No site selected yet.</p>}
          {manage && (
            <form className="live-inline-form" onSubmit={createSite}>
              <input name="name" placeholder="New site name" required maxLength={120} />
              <input name="timezone" defaultValue={Intl.DateTimeFormat().resolvedOptions().timeZone} required maxLength={64} aria-label="Timezone" />
              <button type="submit" disabled={busy}><Icon name="plus" size={14} />Add site</button>
            </form>
          )}
        </section>

        <section className="panel live-card">
          <header><span className="live-step">2</span><div><h2>Cameras</h2><p>Let the site box find the recorder and its cameras, or add a camera by hand.</p></div></header>
          {manage && site && (finding
            ? <CameraDiscovery api={api} siteId={site.id} timezone={site.timezone} onNotify={onNotify} onAdded={reload} onClose={() => setFinding(false)} />
            : <button type="button" className="live-primary" onClick={() => setFinding(true)}><Icon name="search" size={14} />Find cameras</button>)}
          <ul className="live-list">
            {cameras.length === 0 && <li className="live-muted">No cameras yet.</li>}
            {cameras.map((camera) => {
              const status = cameraStatus[camera.id];
              return (
                <li key={camera.id}>
                  <span className={`live-dot ${status ? (status.online ? "ok" : "bad") : ""}`} />
                  <span><strong>{camera.name}</strong><small>{status ? (status.online ? `Online · frame ${ago(status.seen)}` : `Offline · ${status.error || "no frames"}`) : "Waiting for the site box"}</small></span>
                  <button type="button" className="live-link" onClick={() => copy(camera.id, onNotify)} title="Copy camera ID"><code>{camera.id.slice(0, 8)}…</code> Copy ID</button>
                </li>
              );
            })}
          </ul>
          {manage && site && (
            <form className="live-inline-form" onSubmit={addCamera}>
              <input name="name" placeholder="Or add one by hand, e.g. Gate 1" required maxLength={120} />
              <button type="submit" disabled={busy}><Icon name="plus" size={14} />Add camera</button>
            </form>
          )}
        </section>

        <section className="panel live-card">
          <header><span className="live-step">3</span><div><h2>Site box</h2><p>The small PC on site that runs the AI. Pair it with a one-time code.</p></div></header>
          <p className={`live-banner ${boxOnline ? "ok" : ""}`}>
            <Icon name={boxOnline ? "check" : "clock"} size={14} />
            {boxOnline ? "Site box online" : activeKeys.length ? "Site box has not checked in during the last few minutes" : "No site box connected yet"}
          </p>
          <div className="live-download">
            <a className="live-link" href={BOX_INSTALLER_URL} rel="noopener noreferrer"><Icon name="download" size={14} />Download SyncCamSetup.exe</a>
            <small className="live-muted">Windows 10 or 11, 64-bit. Run it on a PC at the site that stays on and is on the same network as the cameras, then pair it with a code below. The box keeps the PC from going to sleep while it runs; a laptop's lid must stay open or be set to "Do nothing".</small>
          </div>
          {manage && site && activeKeys.length > 0 && (
            <div className="live-restart">
              {restart === "confirm" ? (
                <div className="live-secret" role="alertdialog" aria-labelledby="restart-title">
                  <strong id="restart-title">Restart the site box?</strong>
                  <small>Cameras and AI stop and start again. Alerts pause for about a minute. Use this if cameras look stuck or alerts stopped.</small>
                  <div>
                    <button type="button" onClick={() => void restartBox()}>Restart now</button>
                    <button type="button" className="secondary" onClick={() => setRestart("idle")}>Cancel</button>
                  </div>
                </div>
              ) : canRestart(manage, boxOnline) ? (
                <button type="button" className="live-link" disabled={restart === "sending"} onClick={() => setRestart("confirm")}>
                  <Icon name="refresh" size={14} />{restart === "sending" ? "Restarting…" : "Restart site box"}
                </button>
              ) : (
                <small className="live-muted">Restart from here is available while the site box is online. If it is offline, check that its computer is switched on, awake and connected to the internet, or restart that computer.</small>
              )}
            </div>
          )}
          <ul className="live-list">
            {keys.map((key) => (
              <li key={key.id}>
                <span className={`live-dot ${key.revoked_at ? "" : key.last_seen_at && Date.now() - Date.parse(key.last_seen_at) < 6 * 60_000 ? "ok" : "bad"}`} />
                <span><strong>{key.label}</strong><small>{key.revoked_at ? "Revoked" : `${key.box_public_key ? "Paired box · " : ""}Last seen ${ago(key.last_seen_at)}`}</small></span>
                {manage && !key.revoked_at && (
                  <button type="button" className="live-link danger" disabled={busy} onClick={() => void run(async () => { await api.revokeSiteKey(key.id); onNotify("Site key revoked"); refresh(); }, "Revoke failed")}>Revoke</button>
                )}
              </li>
            ))}
          </ul>
          {secret && (
            <div className="live-secret" role="alert">
              <strong>Copy this site key now — it is shown only once.</strong>
              <code>{secret}</code>
              <div>
                <button type="button" onClick={() => copy(secret, onNotify)}>Copy key</button>
                <button type="button" className="secondary" onClick={() => setSecret("")}>I saved it</button>
              </div>
              <small>For a box installed from source: set it as the <code>SYNCAM_SITE_KEY</code> environment variable on the box (see the site box guide).</small>
            </div>
          )}
          {pairing && (
            <div className="live-secret live-pairing" role="status">
              <strong>Pairing code for {site?.name ?? "this site"}</strong>
              <code className="live-pairing-code" aria-label={`Pairing code ${pairing.code.split("").join(" ")}`}>{pairing.code}</code>
              <small>
                On a Windows PC at the site that stays on, <a href={BOX_INSTALLER_URL} rel="noopener noreferrer">download SyncCamSetup.exe</a> and
                type this code when it asks. If Windows SmartScreen warns, choose <em>More info → Run anyway</em>.
                The code works once and expires at {new Date(pairing.expires_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}.
              </small>
              <div>
                <button type="button" onClick={() => copy(pairing.code, onNotify)}>Copy code</button>
                <button type="button" className="secondary" onClick={() => { setPairing(null); refresh(); }}>Done</button>
              </div>
            </div>
          )}
          {manage && site && (
            <div className="live-actions">
              <button type="button" className="live-primary" disabled={busy} onClick={() => void run(async () => { setSecret(""); setPairing(await api.createPairingCode(site.id)); }, "Pairing code could not be created")}>
                <Icon name="plus" size={14} />Pair a site box
              </button>
              <button type="button" className="live-link" disabled={busy} onClick={() => void run(async () => { setPairing(null); const created = await api.createSiteKey(site.id, `Site box ${activeKeys.length + 1}`); setSecret(created.secret); refresh(); }, "Site key could not be created")}>
                Advanced: create a site key instead
              </button>
            </div>
          )}
        </section>

        <section className="panel live-card">
          <header><span className="live-step">4</span><div><h2>Telegram alerts</h2><p>Each linked chat gets a photo message for every alert at this site.</p></div></header>
          {telegram.error ? <p className="live-muted">{telegram.error}</p> : (
            <ul className="live-list">
              {telegram.targets.length === 0 && <li className="live-muted">No chats linked yet.</li>}
              {telegram.targets.map((target) => (
                <li key={target.id}>
                  <span className={`live-dot ${target.muted ? "" : "ok"}`} />
                  <span><strong>{target.chat_title || `Chat ${target.chat_id}`}</strong><small>{target.min_severity} severity and above</small></span>
                  {manage && <button type="button" className="live-link danger" disabled={busy} onClick={() => void run(async () => { await api.deleteTelegram(target.id); onNotify("Telegram chat removed"); refresh(); }, "Remove failed")}>Remove</button>}
                </li>
              ))}
            </ul>
          )}
          {link && (
            <div className="live-secret">
              <strong>Open Telegram to finish linking (valid 15 minutes)</strong>
              {link.bot && (
                <div className="live-telegram-link">
                  <LinkQr url={`https://t.me/${link.bot}?start=${link.code}`} />
                  <div>
                    <small>Scan with the phone camera to open Telegram, then press <strong>Start</strong>.</small>
                    <a className="live-primary" href={`https://t.me/${link.bot}?start=${link.code}`} target="_blank" rel="noreferrer">Open @{link.bot} in Telegram</a>
                    <a className="live-link" href={`https://t.me/${link.bot}?startgroup=${link.code}`} target="_blank" rel="noreferrer">Alert a group instead: add the bot to a group</a>
                  </div>
                </div>
              )}
              <small>Or send <code>/start {link.code}</code> to the SyncCam bot. To alert a group, add the bot to the group and send the same message there.</small>
              <button type="button" className="secondary" onClick={() => { setLink(null); refresh(); }}>Done — refresh list</button>
            </div>
          )}
          {manage && site && !telegram.error && (
            <button type="button" className="live-primary" disabled={busy} onClick={() => void run(async () => setLink(await api.createTelegramLink(site.id)), "Telegram link could not be created")}>
              <Icon name="bell" size={14} />Link a Telegram chat
            </button>
          )}
        </section>
      </div>
    </div>
  );
}
