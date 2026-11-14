import { useEffect, useMemo, useState, type MouseEvent } from "react";

import { Icon } from "./icon";
import { vehicleClasses, type Schedule, type Zone, type ZoneKind } from "./syncam-api";
import type { WorkspaceData } from "./use-workspace";
import { canManage, readWorkspace } from "./workspace";

type Point = [number, number];
type Preset = {
  id: string;
  label: string;
  help: string;
  kind: ZoneKind;
  classes: string[];
  loiter?: boolean;
};

const presets: Preset[] = [
  { id: "intrusion", label: "Unauthorized person", help: "Alert when a person enters, or is found inside, while the zone is armed.", kind: "intrusion", classes: ["person"] },
  { id: "restricted", label: "Restricted area", help: "Alert when a person enters a restricted area such as a store room.", kind: "restricted_zone", classes: ["person"] },
  { id: "near-vehicles", label: "Loitering near vehicles (car theft risk)", help: "Alert when a person stays near parked vehicles longer than the dwell time. Parked vehicles never alert.", kind: "loitering", classes: ["person", ...vehicleClasses], loiter: true },
  { id: "loitering", label: "Loitering", help: "Alert when a person stays in the area longer than the dwell time.", kind: "loitering", classes: ["person"], loiter: true },
  { id: "vehicle", label: "Vehicle enters", help: "Alert when a vehicle drives into the zone while armed (e.g. after hours).", kind: "intrusion", classes: vehicleClasses },
  { id: "line", label: "Line crossing", help: "Alert when a person crosses the line. Click two points.", kind: "tripwire", classes: ["person"] },
];
const days = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

function presetFor(zone: Zone) {
  const classes = zone.subject_classes ?? [];
  if (zone.kind === "loitering") return classes.some((item) => vehicleClasses.includes(item)) ? presets[2]! : presets[3]!;
  if (zone.kind === "tripwire") return presets[5]!;
  if (zone.kind === "restricted_zone") return presets[1]!;
  return classes.length && classes.every((item) => vehicleClasses.includes(item)) ? presets[4]! : presets[0]!;
}

function scheduleText(schedule?: Schedule) {
  if (!schedule?.windows?.length) return "Always armed";
  return schedule.windows.map((window) => `${window.days.length === 7 ? "Every day" : window.days.join(", ")} ${window.start}–${window.end}`).join("; ");
}

function points(zone: Zone): Point[] {
  const coordinates = zone.geometry.coordinates as number[][] | number[][][];
  const list = zone.geometry.type === "Polygon" ? (coordinates as number[][][])[0] ?? [] : (coordinates as number[][]);
  return list.map((pair) => [Number(pair[0]), Number(pair[1])] as Point);
}

export function LiveZoneEditor({ workspace, onNotify }: { workspace: WorkspaceData; onNotify: (message: string) => void }) {
  const { api, cameras, zones, site, reload } = workspace;
  const manage = canManage(readWorkspace());
  const [cameraId, setCameraId] = useState("");
  const [image, setImage] = useState<{ camera: string; url: string | null } | null>(null);
  const [draft, setDraft] = useState<Point[]>([]);
  const [presetId, setPresetId] = useState("intrusion");
  const [name, setName] = useState("");
  const [loiter, setLoiter] = useState(90);
  const [armed, setArmed] = useState<"always" | "schedule">("schedule");
  const [start, setStart] = useState("22:00");
  const [end, setEnd] = useState("06:00");
  const [activeDays, setActiveDays] = useState<string[]>(days);
  const [busy, setBusy] = useState(false);
  const preset = presets.find((item) => item.id === presetId) ?? presets[0]!;
  const selectedCamera = cameraId || cameras[0]?.id || "";
  const cameraZones = useMemo(() => zones.filter((zone) => zone.camera_id === selectedCamera), [zones, selectedCamera]);

  useEffect(() => {
    if (!api || !selectedCamera) return;
    const abort = new AbortController();
    let created: string | null = null;
    api.cameraReference(selectedCamera, abort.signal)
      .then((url) => { created = url; if (!abort.signal.aborted) setImage({ camera: selectedCamera, url }); })
      .catch(() => { if (!abort.signal.aborted) setImage({ camera: selectedCamera, url: null }); });
    return () => { abort.abort(); if (created) URL.revokeObjectURL(created); };
  }, [api, selectedCamera]);

  if (!api) return null;
  const needed = preset.kind === "tripwire" ? 2 : 3;

  function addPoint(event: MouseEvent<SVGSVGElement>) {
    if (!manage) return;
    const box = event.currentTarget.getBoundingClientRect();
    const point: Point = [
      Math.round(((event.clientX - box.left) / box.width) * 1000) / 1000,
      Math.round(((event.clientY - box.top) / box.height) * 1000) / 1000,
    ];
    setDraft((current) => (preset.kind === "tripwire" && current.length >= 2 ? [point] : [...current, point]));
  }

  async function save() {
    if (!api || !site || !selectedCamera) return;
    if (draft.length < needed) {
      onNotify(preset.kind === "tripwire" ? "Click two points to draw the line." : "Click at least three points on the image.");
      return;
    }
    if (armed === "schedule" && (!activeDays.length || start === end)) {
      onNotify("Choose at least one day and different start and end times.");
      return;
    }
    const geometry: Zone["geometry"] = preset.kind === "tripwire"
      ? { type: "LineString", coordinates: draft.slice(0, 2) }
      : { type: "Polygon", coordinates: [[...draft, draft[0]!]] };
    setBusy(true);
    try {
      await api.createZone({
        site_id: site.id,
        camera_id: selectedCamera,
        name: name.trim() || preset.label,
        kind: preset.kind,
        geometry,
        subject_classes: preset.classes,
        ...(preset.loiter ? { loiter_seconds: Math.min(600, Math.max(30, loiter)) } : {}),
        ...(armed === "schedule" ? { schedule: { timezone: site.timezone, windows: [{ days: activeDays, start, end }] } } : {}),
      });
      onNotify("Zone saved · the site box applies it within a minute");
      setDraft([]);
      setName("");
      reload();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "Zone could not be saved");
    } finally {
      setBusy(false);
    }
  }

  async function toggle(zone: Zone) {
    if (!api) return;
    try {
      await api.updateZone(zone, { enabled: !zone.enabled });
      onNotify(zone.enabled ? "Zone paused" : "Zone armed");
      reload();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "Zone could not be updated");
    }
  }

  const imageUrl = image?.camera === selectedCamera ? image.url : null;
  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker"><span className="live-pill"><span />LIVE</span><span>Zone protection</span></div>
          <h1>Zones &amp; rules</h1>
          <p>Draw areas on a real camera picture and choose when they are armed.</p>
        </div>
        <div className="heading-actions">
          <label className="site-filter-label" htmlFor="zone-camera">Camera</label>
          <select id="zone-camera" value={selectedCamera} onChange={(event) => { setCameraId(event.target.value); setDraft([]); }}>
            {cameras.length === 0 && <option value="">No cameras yet</option>}
            {cameras.map((camera) => <option key={camera.id} value={camera.id}>{camera.name}</option>)}
          </select>
        </div>
      </section>
      {cameras.length === 0 ? (
        <p className="live-banner">Add cameras in Configuration first.</p>
      ) : (
        <div className="live-zone-layout">
          <section className="panel live-zone-canvas">
            <div className="live-zone-stage">
              {imageUrl ? <img src={imageUrl} alt="Latest camera picture" /> : <div className="live-zone-placeholder">No camera picture yet — start the site box and it uploads one within a minute. You can still draw.</div>}
              <svg viewBox="0 0 1 1" preserveAspectRatio="none" onClick={addPoint} role="img" aria-label="Zone drawing area">
                {cameraZones.map((zone) => {
                  const list = points(zone);
                  const path = list.map((point) => point.join(",")).join(" ");
                  return zone.geometry.type === "LineString"
                    ? <polyline key={zone.id} points={path} className={`live-zone-shape line ${zone.enabled ? "" : "off"}`} />
                    : <polygon key={zone.id} points={path} className={`live-zone-shape ${zone.kind} ${zone.enabled ? "" : "off"}`} />;
                })}
                {draft.length > 0 && (preset.kind === "tripwire" || draft.length < 3
                  ? <polyline points={draft.map((point) => point.join(",")).join(" ")} className="live-zone-shape draft" />
                  : <polygon points={draft.map((point) => point.join(",")).join(" ")} className="live-zone-shape draft" />)}
                {draft.map((point, index) => <circle key={index} cx={point[0]} cy={point[1]} r={0.008} className="live-zone-point" />)}
              </svg>
            </div>
            <footer>
              <span>{manage ? `Click on the picture to place points (${draft.length} placed).` : "View only: ask an administrator to change zones."}</span>
              {draft.length > 0 && <button type="button" onClick={() => setDraft((current) => current.slice(0, -1))}>Undo point</button>}
              {draft.length > 0 && <button type="button" onClick={() => setDraft([])}>Clear</button>}
            </footer>
          </section>
          <aside className="panel live-zone-side">
            {manage && (
              <div className="live-zone-form">
                <h2>New zone</h2>
                <label>Rule<select value={presetId} onChange={(event) => { setPresetId(event.target.value); setDraft([]); }}>
                  {presets.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
                </select></label>
                <small>{preset.help}</small>
                <label>Name<input value={name} onChange={(event) => setName(event.target.value)} placeholder={preset.label} maxLength={120} /></label>
                {preset.loiter && <label>Dwell time (seconds)<input type="number" min={30} max={600} value={loiter} onChange={(event) => setLoiter(Number(event.target.value))} /></label>}
                <fieldset>
                  <legend>Armed</legend>
                  <label className="live-radio"><input type="radio" checked={armed === "always"} onChange={() => setArmed("always")} />Always</label>
                  <label className="live-radio"><input type="radio" checked={armed === "schedule"} onChange={() => setArmed("schedule")} />On a schedule</label>
                  {armed === "schedule" && (
                    <>
                      <div className="live-days">{days.map((day) => (
                        <button key={day} type="button" className={activeDays.includes(day) ? "on" : ""} onClick={() => setActiveDays((current) => current.includes(day) ? current.filter((item) => item !== day) : days.filter((item) => item === day || current.includes(item)))}>{day}</button>
                      ))}</div>
                      <div className="live-times">
                        <label>From<input type="time" value={start} onChange={(event) => setStart(event.target.value)} /></label>
                        <label>To<input type="time" value={end} onChange={(event) => setEnd(event.target.value)} /></label>
                      </div>
                      <small>Times are in the site timezone ({site?.timezone}). An end before the start runs overnight.</small>
                    </>
                  )}
                </fieldset>
                <button type="button" className="live-primary" disabled={busy || draft.length < needed} onClick={() => void save()}><Icon name="check" size={14} />Save zone</button>
              </div>
            )}
            <div className="live-zone-list">
              <h2>Zones on this camera</h2>
              {cameraZones.length === 0 && <p className="live-muted">None yet.</p>}
              {cameraZones.map((zone) => (
                <div key={zone.id} className="live-zone-item">
                  <span className={`zone-kind-dot zone-kind-${zone.kind}`} />
                  <span><strong>{zone.name}</strong><small>{presetFor(zone).label} · {scheduleText(zone.schedule)}</small></span>
                  {manage && <button type="button" className={zone.enabled ? "live-link" : "live-link danger"} onClick={() => void toggle(zone)}>{zone.enabled ? "Armed" : "Paused"}</button>}
                </div>
              ))}
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}
