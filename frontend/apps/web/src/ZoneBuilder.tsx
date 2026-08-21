import { useMemo, useState } from "react";

import { Icon } from "./icon";

type ZoneKind = "intrusion" | "restricted_zone" | "loitering" | "abandoned" | "tripwire";
type RuleZone = { id: string; name: string; kind: ZoneKind; enabled: boolean; threshold: string; severity: "High" | "Medium" | "Low"; points: string };

const seedZones: RuleZone[] = [
  { id: "zone-01", name: "Loading bay perimeter", kind: "intrusion", enabled: true, threshold: "0.75 confidence", severity: "High", points: "22,74 36,24 71,30 82,78" },
  { id: "zone-02", name: "Solvent storage", kind: "restricted_zone", enabled: true, threshold: "Always active", severity: "High", points: "58,50 70,23 87,39 78,62" },
  { id: "zone-03", name: "Dispatch dwell area", kind: "loitering", enabled: false, threshold: "Dwell 90 seconds", severity: "Medium", points: "15,25 34,12 49,32 33,52" },
];

const kindLabels: Record<ZoneKind, string> = { intrusion: "Intrusion", restricted_zone: "Restricted", loitering: "Loitering", abandoned: "Abandoned object", tripwire: "Tripwire" };

export function ZoneBuilder({ dataMode, onNotify }: { dataMode: "demo" | "live"; onNotify: (message: string) => void }) {
  const [zones, setZones] = useState(seedZones);
  const [selectedID, setSelectedID] = useState(seedZones[0]!.id);
  const [drawing, setDrawing] = useState(false);
  const selected = zones.find((zone) => zone.id === selectedID) ?? zones[0]!;
  const enabledCount = useMemo(() => zones.filter((zone) => zone.enabled).length, [zones]);

  function updateSelected(update: Partial<RuleZone>) {
    setZones((current) => current.map((zone) => zone.id === selected.id ? { ...zone, ...update } : zone));
  }
  function newZone() {
    const id = `zone-${Date.now()}`;
    setZones((current) => [...current, { id, name: "Untitled zone", kind: "intrusion", enabled: true, threshold: "0.75 confidence", severity: "Medium", points: "28,32 51,22 62,57 34,67" }]);
    setSelectedID(id); setDrawing(true); onNotify("Draft zone created locally. Draw controls are in demonstration mode.");
  }

  return <section className="zone-builder-page" aria-labelledby="zone-builder-title">
    <header className="zone-builder-heading">
      <div>
        <div className="heading-kicker"><span className={dataMode === "live" ? "live-pill" : "live-pill demo-pill"}><span />{dataMode === "live" ? "LIVE" : "DEMO"}</span><span>Configuration · Site Admin</span></div>
        <h1 id="zone-builder-title">Zones & rules</h1>
        <p>Draw and tune camera-local safety rules before publishing a versioned configuration.</p>
      </div>
      <div className="zone-heading-actions"><span><strong>{enabledCount}</strong> enabled rules</span><button type="button" onClick={newZone}><Icon name="plus" size={15} /> New zone</button></div>
    </header>
    <div className="zone-boundary" role="status"><Icon name="shield" size={17} /><span><strong>{dataMode === "demo" ? "Synthetic design workspace" : "Rule editor ready"}</strong><small>{dataMode === "demo" ? "Canvas geometry is illustrative only. Publishing to an edge device is disabled in this local view." : "Zone metadata is versioned; edge configuration push is not connected in this slice."}</small></span></div>
    <div className="zone-builder-layout">
      <aside className="zone-rule-list" aria-label="Zone rules">
        <header><div><span className="dashboard-overline">Rule list</span><h2>{zones.length} zones</h2></div><button type="button" aria-label="Add a zone" onClick={newZone}>+</button></header>
        <div className="zone-rule-scroll">{zones.map((zone) => <button key={zone.id} type="button" className={zone.id === selected.id ? "zone-rule-card selected" : "zone-rule-card"} onClick={() => { setSelectedID(zone.id); setDrawing(false); }}><span className={`zone-kind-dot zone-kind-${zone.kind}`} /><span><strong>{zone.name}</strong><small>{kindLabels[zone.kind]} · {zone.threshold}</small></span><span className={zone.enabled ? "zone-enabled" : "zone-disabled"}>{zone.enabled ? "On" : "Off"}</span></button>)}</div>
        <footer><Icon name="clock" size={14} /> Draft #3 · saved locally</footer>
      </aside>
      <main className="zone-canvas-panel">
        <header><span><Icon name="camera" size={15} /> CAM-07 · Dock west</span><span className="zone-canvas-label">Synthetic scene · no footage</span></header>
        <div className={`zone-canvas ${drawing ? "is-drawing" : ""}`} aria-label="Synthetic zone drawing canvas">
          <span className="zone-canvas-horizon" /><span className="zone-canvas-aisle aisle-one" /><span className="zone-canvas-aisle aisle-two" /><span className="zone-canvas-grid" />
          <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">{zones.map((zone) => <polygon key={zone.id} points={zone.points} className={`zone-polygon ${zone.kind} ${zone.id === selected.id ? "selected" : ""}`} />)}</svg>
          <div className="zone-canvas-overlay"><span>{drawing ? "Click canvas to place vertices" : "Select a zone to edit its rule"}</span>{drawing && <button type="button" onClick={() => setDrawing(false)}><Icon name="check" size={14} /> Finish drawing</button>}</div>
        </div>
        <footer><button type="button" className={drawing ? "tool-active" : ""} onClick={() => setDrawing(true)}><Icon name="polygon" size={15} /> Draw polygon</button><button type="button" onClick={() => onNotify("Tripwire creation will use the versioned zone API when the edge sync contract is connected.")}><Icon name="line" size={15} /> Tripwire</button><span>Keyboard coordinate entry is planned with the production canvas.</span></footer>
      </main>
      <aside className="zone-config-panel" aria-label="Selected zone configuration">
        <header><div><span className="dashboard-overline">Rule configuration</span><h2>{selected.name}</h2></div><span className={`zone-version ${selected.enabled ? "" : "muted"}`}>v1</span></header>
        <label>Zone name<input value={selected.name} onChange={(event) => updateSelected({ name: event.target.value })} /></label>
        <label>Rule type<select value={selected.kind} onChange={(event) => updateSelected({ kind: event.target.value as ZoneKind })}>{Object.entries(kindLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label>Threshold<input value={selected.threshold} onChange={(event) => updateSelected({ threshold: event.target.value })} /></label>
        <label>Severity<select value={selected.severity} onChange={(event) => updateSelected({ severity: event.target.value as RuleZone["severity"] })}><option>High</option><option>Medium</option><option>Low</option></select></label>
        <label className="zone-toggle"><span><strong>Rule enabled</strong><small>Applies after versioned edge push</small></span><input type="checkbox" checked={selected.enabled} onChange={(event) => updateSelected({ enabled: event.target.checked })} /></label>
        <div className="zone-config-actions"><button type="button" className="secondary" onClick={() => onNotify("Simulation is intentionally unavailable until synthetic-track injection is connected.")}>Test rule</button><button type="button" onClick={() => onNotify("Saved as local draft. Edge sync is intentionally not yet available.")}>Save draft</button></div>
      </aside>
    </div>
  </section>;
}
