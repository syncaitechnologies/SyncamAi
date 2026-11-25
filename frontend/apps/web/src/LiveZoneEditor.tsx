import { useEffect, useMemo, useState, type MouseEvent } from "react";

import { Icon } from "./icon";
import { allDays, hasTemplate, protectionTemplates, templateZone, type ProtectionTemplate } from "./protection-templates";
import { vehicleClasses, type Schedule, type Zone, type ZoneKind } from "./syncam-api";
import type { WorkspaceData } from "./use-workspace";
import { canManage, readWorkspace } from "./workspace";
import { removeZoneQuestion } from "./zones-model";

type Point = [number, number];
type Preset = {
  id: string;
  label: string;
  help: string;
  kind: ZoneKind;
  classes: string[];
  loiter?: boolean;
  /** Counts visitors or table use (ADR-020): never alerts, has no schedule. */
  counts?: boolean;
};

const presets: Preset[] = [
  { id: "intrusion", label: "Unauthorized person", help: "Alert when a person enters, or is found inside, while the zone is armed.", kind: "intrusion", classes: ["person"] },
  { id: "restricted", label: "Restricted area", help: "Alert when a person enters a restricted area such as a store room.", kind: "restricted_zone", classes: ["person"] },
  { id: "near-vehicles", label: "Loitering near vehicles (car theft risk)", help: "Alert when a person stays near parked vehicles longer than the dwell time. Parked vehicles never alert.", kind: "loitering", classes: ["person", ...vehicleClasses], loiter: true },
  { id: "loitering", label: "Loitering", help: "Alert when a person stays in the area longer than the dwell time.", kind: "loitering", classes: ["person"], loiter: true },
  { id: "vehicle", label: "Vehicle enters", help: "Alert when a vehicle drives into the zone while armed (e.g. after hours).", kind: "intrusion", classes: vehicleClasses },
  { id: "line", label: "Line crossing", help: "Alert when a person crosses the line. Click two points.", kind: "tripwire", classes: ["person"] },
  { id: "door", label: "Door counter (visitors)", help: "Counts people walking in and out, without recognising anyone. Click two points across the door; the short tick shows the inside. Use Flip if it points out.", kind: "door_line", classes: ["person"], counts: true },
  { id: "table", label: "Table (dining time)", help: "Measures how long the table is used and how many groups sit there. Draw around the table and its chairs.", kind: "table", classes: ["person"], counts: true },
];
const isLine = (kind: ZoneKind) => kind === "tripwire" || kind === "door_line";

/** A short tick from the middle of a door line towards the inside (the right of the arrow). */
function insideTick(line: Point[]): [Point, Point] | null {
  if (line.length < 2) return null;
  const [[ax, ay], [bx, by]] = [line[0]!, line[line.length - 1]!];
  const length = Math.hypot(bx - ax, by - ay) || 1;
  const middle: Point = [(ax + bx) / 2, (ay + by) / 2];
  return [middle, [middle[0] - ((by - ay) / length) * 0.06, middle[1] + ((bx - ax) / length) * 0.06]];
}
const days = allDays;

function presetFor(zone: Zone) {
  const classes = zone.subject_classes ?? [];
  if (zone.kind === "loitering") return classes.some((item) => vehicleClasses.includes(item)) ? presets[2]! : presets[3]!;
  if (zone.kind === "tripwire") return presets[5]!;
  if (zone.kind === "door_line") return presets[6]!;
  if (zone.kind === "table") return presets[7]!;
  if (zone.kind === "restricted_zone") return presets[1]!;
  return classes.length && classes.every((item) => vehicleClasses.includes(item)) ? presets[4]! : presets[0]!;
}

function scheduleText(schedule?: Schedule, kind?: string) {
  if (kind === "door_line") return "Counts visitors";
  if (kind === "table") return "Measures dining time";
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
  const needed = isLine(preset.kind) ? 2 : 3;

  function addPoint(event: MouseEvent<SVGSVGElement>) {
    if (!manage) return;
    const box = event.currentTarget.getBoundingClientRect();
    const point: Point = [
      Math.round(((event.clientX - box.left) / box.width) * 1000) / 1000,
      Math.round(((event.clientY - box.top) / box.height) * 1000) / 1000,
    ];
    setDraft((current) => (isLine(preset.kind) && current.length >= 2 ? [point] : [...current, point]));
  }

  async function save() {
    if (!api || !site || !selectedCamera) return;
    if (draft.length < needed) {
      onNotify(isLine(preset.kind) ? "Click two points to draw the line." : "Click at least three points on the image.");
      return;
    }
    if (!preset.counts && armed === "schedule" && (!activeDays.length || start === end)) {
      onNotify("Choose at least one day and different start and end times.");
      return;
    }
    const geometry: Zone["geometry"] = isLine(preset.kind)
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
        ...(!preset.counts && armed === "schedule" ? { schedule: { timezone: site.timezone, windows: [{ days: activeDays, start, end }] } } : {}),
      });
      onNotify(preset.counts ? "Saved · counting starts within a minute on site box 0.1.7 or later" : "Zone saved · the site box applies it within a minute");
      setDraft([]);
      setName("");
      reload();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "Zone could not be saved");
    } finally {
      setBusy(false);
    }
  }

  async function protect(template: ProtectionTemplate) {
    if (!api || !site || !selectedCamera) return;
    setBusy(true);
    try {
      await api.createZone(templateZone(template, { siteId: site.id, cameraId: selectedCamera, timezone: site.timezone }));
      onNotify(`${template.label} added · the site box applies it within a minute`);
      reload();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "Protection could not be added");
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

  async function remove(zone: Zone) {
    if (!api) return;
    if (!window.confirm(removeZoneQuestion(zone.name))) return;
    try {
      await api.deleteZone(zone.id);
      onNotify(`${zone.name} removed · the site box stops using it within a minute`);
      reload();
    } catch (error) {
      onNotify(error instanceof Error ? error.message : "Zone could not be removed");
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
                  const tick = zone.kind === "door_line" ? insideTick(list) : null;
                  return zone.geometry.type === "LineString"
                    ? (
                      <g key={zone.id}>
                        <polyline points={path} className={`live-zone-shape line ${zone.kind} ${zone.enabled ? "" : "off"}`} />
                        {tick && <line x1={tick[0][0]} y1={tick[0][1]} x2={tick[1][0]} y2={tick[1][1]} className="live-zone-inside" />}
                      </g>
                    )
                    : <polygon key={zone.id} points={path} className={`live-zone-shape ${zone.kind} ${zone.enabled ? "" : "off"}`} />;
                })}
                {preset.kind === "door_line" && draft.length === 2 && (() => {
                  const tick = insideTick(draft)!;
                  return <line x1={tick[0][0]} y1={tick[0][1]} x2={tick[1][0]} y2={tick[1][1]} className="live-zone-inside" />;
                })()}
                {draft.length > 0 && (isLine(preset.kind) || draft.length < 3
                  ? <polyline points={draft.map((point) => point.join(",")).join(" ")} className="live-zone-shape draft" />
                  : <polygon points={draft.map((point) => point.join(",")).join(" ")} className="live-zone-shape draft" />)}
                {draft.map((point, index) => <circle key={index} cx={point[0]} cy={point[1]} r={0.008} className="live-zone-point" />)}
              </svg>
            </div>
            <footer>
              <span>{manage ? `Click on the picture to place points (${draft.length} placed).` : "View only: ask an administrator to change zones."}</span>
              {draft.length > 0 && <button type="button" onClick={() => setDraft((current) => current.slice(0, -1))}>Undo point</button>}
              {draft.length > 0 && <button type="button" onClick={() => setDraft([])}>Clear</button>}
              {preset.kind === "door_line" && draft.length === 2 && <button type="button" onClick={() => setDraft((current) => [current[1]!, current[0]!])}>Flip direction</button>}
            </footer>
          </section>
          <aside className="panel live-zone-side">
            {manage && (
              <div className="live-templates">
                <h2>Protect this camera</h2>
                <small className="live-muted">One click covers the whole picture. Times are in the site timezone ({site?.timezone}); change or pause it later like any zone.</small>
                {protectionTemplates.map((template) => {
                  const added = hasTemplate(zones, selectedCamera, template);
                  return (
                    <div key={template.id} className="live-template">
                      <strong>{template.label}</strong>
                      <small>{template.help}</small>
                      <button type="button" className={added ? "live-link" : "live-primary"} disabled={busy || added} onClick={() => void protect(template)}>
                        {added ? <><Icon name="check" size={14} />Added</> : `Add ${template.label.toLowerCase()}`}
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
            {manage && (
              <div className="live-zone-form">
                <h2>New zone</h2>
                <label>Rule<select value={presetId} onChange={(event) => { setPresetId(event.target.value); setDraft([]); }}>
                  {presets.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
                </select></label>
                <small>{preset.help}</small>
                <label>Name<input value={name} onChange={(event) => setName(event.target.value)} placeholder={preset.label} maxLength={120} /></label>
                {preset.loiter && <label>Dwell time (seconds)<input type="number" min={30} max={600} value={loiter} onChange={(event) => setLoiter(Number(event.target.value))} /></label>}
                {!preset.counts && <fieldset>
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
                </fieldset>}
                <button type="button" className="live-primary" disabled={busy || draft.length < needed} onClick={() => void save()}><Icon name="check" size={14} />Save zone</button>
              </div>
            )}
            <div className="live-zone-list">
              <h2>Zones on this camera</h2>
              {cameraZones.length === 0 && <p className="live-muted">None yet.</p>}
              {cameraZones.map((zone) => (
                <div key={zone.id} className="live-zone-item">
                  <span className={`zone-kind-dot zone-kind-${zone.kind}`} />
                  <span><strong>{zone.name}</strong><small>{presetFor(zone).label} · {scheduleText(zone.schedule, zone.kind)}</small></span>
                  {manage && (
                    <span className="live-zone-actions">
                      <button type="button" className={zone.enabled ? "live-link" : "live-link danger"} onClick={() => void toggle(zone)}>{zone.enabled ? "Armed" : "Paused"}</button>
                      <button type="button" className="live-link live-zone-remove" aria-label={`Remove ${zone.name}`} title="Remove zone" onClick={() => void remove(zone)}>Remove</button>
                    </span>
                  )}
                </div>
              ))}
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}
