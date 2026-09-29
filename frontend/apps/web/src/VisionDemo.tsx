import { useState } from "react";
import { Icon } from "./icon";
import { defaultDemoSettings, demoFeatures, demoOutcome, demoScenarios, initialDemoState, transitionDemo,
  type DemoFeature, type DemoMode, type DemoSettings } from "./vision-demo-model";
import "./vision-demo.css";

function DemoScene({ settings, step }: { settings: DemoSettings; step: number }) {
  const visible = step > 0 && settings.scenario !== "empty";
  const isFace = settings.feature === "face";
  const x = settings.feature === "person" && settings.scenario === "entry" && step > 1 ? 485 : 270;
  return (
    <svg className="vision-scene" viewBox="0 0 800 440" role="img"
      aria-label={`Illustrated ${settings.feature} workflow, scripted step ${step} of 3. No camera footage.`}>
      <rect width="800" height="440" fill="#111e29" />
      <path d="M0 260H800M0 440 250 260M200 440 350 260M400 440V260M600 440 450 260M800 440 550 260M0 340H800"
        stroke="#2a3c49" fill="none" />
      <path d="M70 260V72H195V260M605 260V72H730V260M250 260V115H550V260" fill="#192a36" stroke="#354a58" />
      <path d="M420 235H690L750 400H380Z" fill="#f2a93b" fillOpacity=".09" stroke="#d4a657" strokeDasharray="8 6" />
      <text x="450" y="386" fill="#ddbd85" fontSize="15">{isFace ? "DEMO ENTRY ZONE" : "DEMO RESTRICTED ZONE"}</text>
      <text x="24" y="34" fill="#a6c1d1" fontSize="14" letterSpacing="2">ILLUSTRATED SCENE · NO CAMERA FEED</text>
      {visible && <g transform={`translate(${x}, 180)`}>
        <circle cx="0" cy="0" r="18" fill="#6b8a9b" />
        <path d="M-26 38Q0 18 26 38L20 100H-20Z" fill="#547487" />
        <path d="M-18 100-22 158M18 100 22 158M-26 44-43 88M26 44 43 88" stroke="#6b8a9b" strokeWidth="13" strokeLinecap="round" />
        <rect x="-60" y="-35" width="120" height="210" rx="5" stroke="#74dab4" strokeWidth="2" fill="none" />
        <rect x="-61" y="-61" width="146" height="24" rx="3" fill="#74dab4" />
        <text x="-53" y="-44" fill="#10251d" fontSize="13">{isFace ? "FICTIONAL SUBJECT" : "SIMULATED PERSON"}</text>
        {settings.feature === "weapon" && <g>
          <rect x="23" y="55" width="62" height="50" fill="none" stroke="#f4aa72" strokeWidth="2" />
          <path d={settings.scenario === "tool" ? "M40 79h27M53 68v23" : "M37 72h30v8H51v15H42V80h-5Z"}
            fill="none" stroke="#d2a57e" strokeWidth="5" />
        </g>}
        {isFace && <rect x="-24" y="-26" width="48" height="53" rx="5" stroke="#a69cf2" fill="none" strokeWidth="2" />}
      </g>}
      {visible && settings.feature === "person" && settings.scenario === "outside" && <g transform="translate(140,230)" fill="#587486">
        <circle cy="-30" r="13" /><rect x="-17" y="-10" width="34" height="65" rx="10" />
        <path d="M-10 45-14 100M10 45 14 100" stroke="#587486" strokeWidth="10" />
      </g>}
    </svg>
  );
}

export function VisionDemo({ dataMode, onOpenOverview }: { dataMode: DemoMode; onOpenOverview: () => void }) {
  const [settings, setSettings] = useState(() => defaultDemoSettings("person"));
  const [state, setState] = useState(initialDemoState);
  const feature = demoFeatures.find(({ id }) => id === settings.feature)!;
  const result = demoOutcome(dataMode, settings, state);
  function configure(next: DemoSettings) {
    setSettings(next);
    setState(initialDemoState());
  }
  function selectFeature(id: DemoFeature) { configure(defaultDemoSettings(id)); }

  if (dataMode !== "demo") return <section className="vision-page">
    <h1>AI walkthrough unavailable in live mode</h1>
    <p>The three AI services have not been activated. The registry and biometric prerequisites remain outstanding.</p>
  </section>;

  return <section className="vision-page" aria-labelledby="vision-title">
    <header className="vision-heading">
      <div><span className="vision-eyebrow">SYNCCAM AI · CLIENT WALKTHROUGH</span>
        <h1 id="vision-title">Three workflows. One console.</h1>
        <p>Choose a feature and walk through what an operator sees.</p></div>
      <div className="vision-heading-actions"><span className="vision-badge">Interactive demo</span>
        <button type="button" className="vision-overview" onClick={onOpenOverview}>Open overview <Icon name="arrow" size={14} /></button></div>
    </header>
    <div className="vision-notice"><Icon name="activity" />
      <p><strong>Simulated observations.</strong> These illustrations and results explain the workflows.
        Live AI is not running; no photos, faces or camera feeds are processed.</p></div>
    <div className="vision-features" role="group" aria-label="Choose an AI workflow">
      {demoFeatures.map((item, index) => <button type="button" key={item.id}
        className={`vision-feature ${item.id === settings.feature ? "selected" : ""}`}
        aria-pressed={item.id === settings.feature} onClick={() => selectFeature(item.id)}>
        <span className="vision-feature-number">0{index + 1}</span><Icon name={item.icon} size={22} />
        <strong>{item.title}</strong><span>{item.description}</span>
      </button>)}
    </div>
    <div className="vision-workspace">
      <div className="vision-monitor">
        <header><span><Icon name="camera" size={16} />{feature.camera}</span><span>Scripted scene</span></header>
        <DemoScene settings={settings} step={state.step} />
        <div className="vision-scene-caption">
          <strong>{state.step ? `${state.step} / 3 scripted steps` : "Start the walkthrough"}</strong>
          <span>{settings.feature === "person" ? "Camera-local tracks · no identity" :
            settings.feature === "weapon" ? "Three observations → human review" : "Opt-in → liveness → human review"}</span>
        </div>
        <ol className="vision-steps" aria-label="Workflow progress">
          {feature.steps.map((label, index) => <li key={label} className={state.step > index ? "complete" : ""}>
            <span>{state.step > index ? "✓" : index + 1}</span>{label}</li>)}
        </ol>
      </div>
      <div className="vision-controls">
        <span className="vision-eyebrow">PRESENTATION CONTROLS</span><h2>{feature.title}</h2>
        <label htmlFor="vision-scenario">Scenario</label>
        <select id="vision-scenario" value={settings.scenario}
          onChange={(event) => configure({ ...settings, scenario: event.target.value })}>
          {demoScenarios[settings.feature].map(({ id, label }) => <option key={id} value={id}>{label}</option>)}
        </select>
        {settings.feature === "weapon" && <div className="vision-threshold">
          <label htmlFor="vision-threshold">Demo threshold <strong>{Math.round(settings.threshold * 100)}%</strong></label>
          <input id="vision-threshold" type="range" min="50" max="90" step="5" value={settings.threshold * 100}
            onChange={(event) => configure({ ...settings, threshold: Number(event.target.value) / 100 })} />
          <p>Fixture scores: candidate 84%, uncertain 62%. These are scripted values, not accuracy measurements.</p>
        </div>}
        {settings.feature === "face" && <fieldset className="vision-face-checks">
          <legend>Simulated prerequisites</legend>
          <label><input type="checkbox" checked={settings.consent}
            onChange={(event) => configure({ ...settings, consent: event.target.checked })} />
            Demo opt-in present</label>
          <label><input type="checkbox" checked={settings.liveness}
            onChange={(event) => configure({ ...settings, liveness: event.target.checked })} />
            Demo liveness passes</label>
          <p>Fictional checks only. These switches do not collect consent or verify liveness.</p>
        </fieldset>}
        <div className="vision-buttons">
          <button type="button" className="vision-primary" disabled={state.step >= 3}
            onClick={() => setState((current) => transitionDemo(dataMode, settings, current, { type: "advance" }))}>
            <Icon name="play" size={16} />{state.step === 0 ? "Start scenario" : state.step >= 3 ? "Scenario complete" : "Next observation"}</button>
          <button type="button" className="vision-secondary"
            onClick={() => setState(initialDemoState())}>Reset</button>
        </div>
        <div className={`vision-result ${result.eligible ? "ready" : ""}`} role="status" aria-live="polite" aria-atomic="true">
          <span className="vision-eyebrow">DEMO OUTCOME</span><h3>{result.title}</h3><p>{result.detail}</p>
          {result.eligible && <>
            <span className="vision-review-status">{state.review === "pending" ? "Pending human review" :
              state.review === "acknowledged" ? "Acknowledged in this demo" : "Dismissed in this demo"}</span>
            <div className="vision-buttons">
              <button type="button" className="vision-primary" disabled={state.review !== "pending"}
                onClick={() => setState((current) => transitionDemo(dataMode, settings, current,
                  { type: "review", decision: "acknowledged" }))}>Acknowledge demo</button>
              <button type="button" className="vision-secondary" disabled={state.review !== "pending"}
                onClick={() => setState((current) => transitionDemo(dataMode, settings, current,
                  { type: "review", decision: "dismissed" }))}>Dismiss demo</button>
            </div>
          </>}
        </div>
      </div>
    </div>
    <footer className="vision-footer"><span><Icon name="shield" size={16} />Presentation state stays in this page and resets on reload.</span>
      <details><summary>What is needed for live AI?</summary>
        <p>{feature.blockers}</p><p>Shared delivery also needs a running inference host, authenticated backend,
          verified edge device identity, privacy-mask runtime and tested alert delivery. AWS is optional for a local prototype.</p>
      </details>
    </footer>
  </section>;
}
