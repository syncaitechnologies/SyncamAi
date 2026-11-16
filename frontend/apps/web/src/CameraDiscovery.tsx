import { useEffect, useRef, useState, type FormEvent } from "react";

import { adoptPayload, channelChoices, deviceTitle, isPrivateIPv4, manualDevice, validChoices, type ChannelChoice } from "./camera-discovery-model";
import { Icon } from "./icon";
import { encryptCredentials } from "./setup-crypto";
import type { FoundDevice, ProbeResult, SetupBox, SyncamApi } from "./syncam-api";

type Step =
  | { kind: "start" }
  | { kind: "scanning" }
  | { kind: "devices"; devices: FoundDevice[] }
  | { kind: "login"; device: FoundDevice }
  | { kind: "checking"; device: FoundDevice }
  | { kind: "channels"; device: FoundDevice; deviceId: string; brand: string; choices: ChannelChoice[]; note: string }
  | { kind: "adding"; count: number }
  | { kind: "done"; count: number };

/**
 * "Find cameras": the site box scans its network, checks a recorder with its
 * password (encrypted for that box in this browser), shows a picture of each
 * channel, and starts the ones the user picks. No IP addresses, RTSP paths or
 * config files to type.
 */
export function CameraDiscovery({ api, siteId, onNotify, onAdded, onClose }: {
  api: SyncamApi;
  siteId: string;
  onNotify: (message: string) => void;
  onAdded: () => void;
  onClose: () => void;
}) {
  const [box, setBox] = useState<SetupBox | null>(null);
  const [step, setStep] = useState<Step>({ kind: "start" });
  const [error, setError] = useState("");
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    api.setupBox(siteId, controller.signal).then(setBox).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Site box status unavailable"));
    return () => {
      controller.abort();
      abort.current?.abort();
    };
  }, [api, siteId]);

  const run = async (work: (signal: AbortSignal) => Promise<void>, fallback: Step) => {
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setError("");
    try {
      await work(controller.signal);
    } catch (reason) {
      if (reason instanceof DOMException && reason.name === "AbortError") return;
      setError(reason instanceof Error ? reason.message : "Something went wrong.");
      setStep(fallback);
    }
  };

  const scan = () => {
    setStep({ kind: "scanning" });
    void run(async (signal) => {
      const result = await api.runSetupTask<{ devices: FoundDevice[] }>(siteId, "scan", undefined, { signal, timeoutMs: 120_000 });
      setStep({ kind: "devices", devices: result.devices });
    }, { kind: "start" });
  };

  const check = (event: FormEvent<HTMLFormElement>, device: FoundDevice) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const username = String(form.get("username") ?? "").trim();
    const password = String(form.get("password") ?? "");
    const port = Number(form.get("port") ?? device.rtsp_port);
    const target = { ...device, rtsp_port: port, device_id: `${device.ip}:${port}` };
    if (!box?.public_key) {
      setError("Pair a site box first (step 3).");
      return;
    }
    setStep({ kind: "checking", device: target });
    void run(async (signal) => {
      const credentials = await encryptCredentials(box.public_key!, { username, password });
      const result = await api.runSetupTask<ProbeResult>(siteId, "probe", {
        device: { ip: target.ip, rtsp_port: target.rtsp_port, family: target.family },
        credentials,
      }, { signal, timeoutMs: 240_000, intervalMs: 2000 });
      setStep({
        kind: "channels", device: target, deviceId: result.device_id, brand: result.brand,
        choices: channelChoices(result), note: result.error,
      });
    }, { kind: "login", device: target });
  };

  const add = (deviceId: string, choices: ChannelChoice[]) => {
    const picked = validChoices(choices);
    if (!picked.length) {
      setError("Tick at least one channel and give it a name.");
      return;
    }
    setStep({ kind: "adding", count: picked.length });
    void run(async (signal) => {
      const created = [];
      for (const choice of picked) {
        const camera = await api.createCamera(siteId, choice.name.trim());
        created.push({ camera_id: camera.id, channel: choice.channel });
      }
      onAdded();
      await api.runSetupTask(siteId, "adopt", adoptPayload(deviceId, created), { signal, timeoutMs: 60_000 });
      onNotify(`${created.length} camera${created.length === 1 ? "" : "s"} added. Pictures appear within a minute.`);
      setStep({ kind: "done", count: created.length });
      onAdded();
    }, { kind: "start" });
  };

  const updateChoice = (index: number, change: Partial<ChannelChoice>) =>
    setStep((current) => current.kind === "channels"
      ? { ...current, choices: current.choices.map((choice, position) => (position === index ? { ...choice, ...change } : choice)) }
      : current);

  return (
    <div className="discovery" role="region" aria-label="Find cameras">
      <header className="discovery-head">
        <strong>Find cameras</strong>
        <button type="button" className="live-link" onClick={onClose}>Close</button>
      </header>
      {box && !box.paired && <p className="live-banner">Pair a site box first (step 3). Finding cameras runs on the site box, inside the client's network.</p>}
      {box?.paired && !box.online && <p className="live-banner">The site box "{box.label}" has not checked in recently. Steps run as soon as it is back online.</p>}
      {error && <p className="live-banner bad" role="alert">{error}</p>}

      {step.kind === "start" && (
        <div className="discovery-body">
          <p className="live-muted">The site box looks for recorders (DVR/NVR) and IP cameras on its network. It takes about half a minute.</p>
          <button type="button" className="live-primary" disabled={!box?.paired} onClick={scan}><Icon name="search" size={14} />Scan the network</button>
        </div>
      )}

      {step.kind === "scanning" && <p className="discovery-wait" role="status">Scanning the client's network…</p>}

      {step.kind === "devices" && (
        <div className="discovery-body">
          {step.devices.length === 0 && <p className="live-muted">No recorders or cameras answered. Enter the address below (shown on the recorder under Network).</p>}
          <ul className="discovery-devices">
            {step.devices.map((device) => (
              <li key={device.device_id}>
                <span><strong>{deviceTitle(device)}</strong><small>{device.brand}{device.onvif ? " · ONVIF" : ""}</small></span>
                <button type="button" onClick={() => setStep({ kind: "login", device })}>Connect</button>
              </li>
            ))}
          </ul>
          <form className="live-inline-form" onSubmit={(event) => {
            event.preventDefault();
            const ip = String(new FormData(event.currentTarget).get("ip") ?? "");
            if (!isPrivateIPv4(ip)) {
              setError("Enter the recorder's local address, e.g. 192.168.1.108.");
              return;
            }
            setStep({ kind: "login", device: manualDevice(ip, 554) });
          }}>
            <input name="ip" placeholder="Or type an address, e.g. 192.168.1.108" aria-label="Recorder address" />
            <button type="submit">Use address</button>
          </form>
          <button type="button" className="live-link" onClick={scan}>Scan again</button>
        </div>
      )}

      {step.kind === "login" && (
        <form className="live-zone-form discovery-body" onSubmit={(event) => check(event, step.device)}>
          <strong>{deviceTitle(step.device)}</strong>
          <small className="live-muted">Use a view-only account on the recorder. The password is encrypted in this browser for the site box only; SyncCam's servers cannot read it.</small>
          <label>Username<input name="username" defaultValue="admin" autoComplete="off" required /></label>
          <label>Password<input name="password" type="password" autoComplete="new-password" required /></label>
          <label>RTSP port<input name="port" type="number" min={1} max={65535} defaultValue={step.device.rtsp_port} required /></label>
          <div className="live-actions">
            <button type="submit" className="live-primary">Check and show channels</button>
            <button type="button" className="live-link" onClick={scan}>Back</button>
          </div>
        </form>
      )}

      {step.kind === "checking" && <p className="discovery-wait" role="status">Checking {step.device.ip} and taking a picture of each channel… this can take a minute.</p>}

      {step.kind === "channels" && (
        <div className="discovery-body">
          <strong>{step.brand} · {step.device.ip}</strong>
          {step.note && <p className="live-muted">{step.note}</p>}
          <ul className="discovery-channels">
            {step.choices.map((choice, index) => (
              <li key={choice.channel} className={choice.selected ? "on" : ""}>
                {choice.thumbnail
                  ? <img src={`data:image/jpeg;base64,${choice.thumbnail}`} alt={`Channel ${choice.channel}`} />
                  : <span className="discovery-nopic">No picture</span>}
                <label className="discovery-pick"><input type="checkbox" checked={choice.selected} onChange={(event) => updateChoice(index, { selected: event.target.checked })} />Channel {choice.channel}</label>
                <input value={choice.name} maxLength={120} aria-label={`Name for channel ${choice.channel}`} onChange={(event) => updateChoice(index, { name: event.target.value })} />
              </li>
            ))}
          </ul>
          <div className="live-actions">
            <button type="button" className="live-primary" onClick={() => add(step.deviceId, step.choices)}>
              <Icon name="plus" size={14} />Add {validChoices(step.choices).length} camera{validChoices(step.choices).length === 1 ? "" : "s"}
            </button>
            <button type="button" className="live-link" onClick={() => setStep({ kind: "login", device: step.device })}>Back</button>
          </div>
        </div>
      )}

      {step.kind === "adding" && <p className="discovery-wait" role="status">Adding {step.count} camera{step.count === 1 ? "" : "s"}…</p>}
      {step.kind === "done" && (
        <div className="discovery-body">
          <p className="live-banner ok"><Icon name="check" size={14} />{step.count} camera{step.count === 1 ? "" : "s"} added. Draw zones for them in Zones &amp; rules.</p>
          <button type="button" className="live-link" onClick={scan}>Find more cameras</button>
        </div>
      )}
    </div>
  );
}
