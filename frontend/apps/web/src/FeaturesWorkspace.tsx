import { useCallback, useEffect, useState } from "react";

import {
  activityLabel,
  askToAddSubject,
  cameraSummary,
  canSwitch,
  cardState,
  featureError,
  featureText,
  groupFeatures,
  headline,
  interestedSubject,
  offConfirmation,
  optimistic,
  STATE_LABELS,
  toggleCamera,
  type Feature,
  type FeatureChanges,
  type FeatureKey,
} from "./features-model";
import { Icon } from "./icon";
import { contactHref } from "./live-view-model";
import { Switch } from "./Switch";
import type { Camera } from "./syncam-api";
import type { WorkspaceData } from "./use-workspace";
import { canManageFeatures, readWorkspace } from "./workspace";

const DEFAULT_CONTACT = String(import.meta.env.VITE_SYNCAM_CONTACT_EMAIL ?? "");

// Features page (ADR-019): every AI feature as a card with its switch, its
// cameras and what it caught lately. The owner decides what each plan
// includes; the workspace owner switches included features on or off. The
// site box applies a change within about a minute, with no restart.
export function FeaturesWorkspace({ workspace, onNotify, contactEmail = DEFAULT_CONTACT, canManage }: {
  workspace: Pick<WorkspaceData, "api" | "cameras">;
  onNotify: (message: string) => void;
  contactEmail?: string;
  canManage?: boolean;
}) {
  const api = workspace.api;
  const manage = canManage ?? canManageFeatures(readWorkspace());
  const [features, setFeatures] = useState<Feature[] | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState<FeatureKey | null>(null);
  const [choosing, setChoosing] = useState<FeatureKey | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!api) return;
    try {
      setFeatures(await api.features(signal));
      setError("");
    } catch (caught) {
      if (!signal?.aborted) setError(caught instanceof Error ? caught.message : "Features could not be loaded.");
    }
  }, [api]);

  useEffect(() => {
    const abort = new AbortController();
    void load(abort.signal);
    return () => abort.abort();
  }, [load]);

  async function change(feature: Feature, changes: FeatureChanges) {
    if (!api || !features) return;
    if (changes.enabled === false) {
      const question = offConfirmation(feature);
      if (question && !window.confirm(question)) return;
    }
    const before = features;
    setFeatures(features.map((item) => (item.key === feature.key ? optimistic(item, changes) : item)));
    setSaving(feature.key);
    try {
      const saved = await api.updateFeature(feature.key, changes);
      setFeatures((current) => (current ?? before).map((item) => (item.key === saved.key ? saved : item)));
      const name = featureText(feature.key).name;
      onNotify("enabled" in changes
        ? `${name} switched ${saved.on ? "on" : "off"} · your site box picks it up within a minute`
        : `${name} cameras saved · your site box picks it up within a minute`);
    } catch (caught) {
      setFeatures(before);
      onNotify(featureError(caught));
    } finally {
      setSaving(null);
    }
  }

  const groups = features ? groupFeatures(features) : null;
  const cameras = workspace.cameras;
  const cardProps = { cameras, manage, saving, choosing, contactEmail, onChange: change, onChoose: setChoosing };

  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker">
            <span className="live-pill"><span />FEATURES</span>
            <span>{features ? headline(features) : "Your AI features"}</span>
          </div>
          <h1>Features</h1>
          <p>Choose which AI features watch your cameras, and where. Changes reach your site box within about a minute, with no restart, and every change is recorded in your audit log.</p>
        </div>
      </section>
      {!manage && features && (
        <p className="live-banner" role="note">
          <Icon name="shield" size={15} />
          You can see which features are on. Only the workspace owner can change them.
        </p>
      )}
      {error && <p className="auth-notice feature-notice" role="alert">{error}</p>}
      {!features && !error && <p className="live-muted feature-notice" role="status">Loading features…</p>}
      {groups && (
        <>
          <FeatureSection title="Your features" hint="Included in your plan." items={groups.yours} {...cardProps} />
          {groups.add.length > 0 && (
            <FeatureSection title="Add to your plan" hint="Ask us and we can switch these on for you." items={groups.add} {...cardProps} />
          )}
          {groups.soon.length > 0 && (
            <FeatureSection title="Coming soon" hint="Tell us what you would use first." items={groups.soon} {...cardProps} />
          )}
        </>
      )}
    </div>
  );
}

type CardProps = {
  cameras: Camera[];
  manage: boolean;
  saving: FeatureKey | null;
  choosing: FeatureKey | null;
  contactEmail: string;
  onChange: (feature: Feature, changes: FeatureChanges) => void;
  onChoose: (key: FeatureKey | null) => void;
};

function FeatureSection({ title, hint, items, ...props }: CardProps & { title: string; hint: string; items: Feature[] }) {
  const id = `features-${title.toLowerCase().replace(/\s+/g, "-")}`;
  return (
    <section className="feature-section" aria-labelledby={id}>
      <header className="feature-section-head">
        <h2 id={id}>{title}</h2>
        <p className="live-muted">{hint}</p>
      </header>
      <div className="feature-grid">
        {items.map((feature) => <FeatureCard key={feature.key} feature={feature} {...props} />)}
      </div>
    </section>
  );
}

function FeatureCard({ feature, cameras, manage, saving, choosing, contactEmail, onChange, onChoose }: CardProps & { feature: Feature }) {
  const text = featureText(feature.key);
  const state = cardState(feature);
  const activity = activityLabel(feature);
  const cameraIds = cameras.map((camera) => camera.id);
  const busy = saving === feature.key;
  const open = choosing === feature.key;
  const switchable = canSwitch(feature);
  const askHref = contactHref(contactEmail, askToAddSubject(feature.key));
  const interestedHref = contactHref(contactEmail, interestedSubject(feature.key));
  const contact = contactEmail.trim();

  return (
    <article className={`panel feature-card feature-${state}`} aria-labelledby={`feature-${feature.key}`}>
      <header className="feature-card-head">
        <span className="feature-icon" aria-hidden="true"><Icon name={text.icon} size={18} /></span>
        <div className="feature-title">
          <h3 id={`feature-${feature.key}`}>
            {text.name}
            {feature.status === "preview" && <span className="feature-badge">Preview</span>}
          </h3>
          <span className={`feature-state feature-state-${state}`}>{STATE_LABELS[state]}</span>
        </div>
        {switchable && (
          <Switch
            checked={feature.enabled}
            disabled={!manage || busy}
            label={`${text.name}: ${feature.enabled ? "on" : "off"}`}
            onChange={(checked) => onChange(feature, { enabled: checked })}
          />
        )}
      </header>
      <p className="feature-summary">{text.summary}</p>
      <p className="feature-good"><span>Good for</span> {text.goodFor}</p>
      {activity && <p className="feature-activity"><Icon name="trend" size={14} /> {activity}</p>}

      {switchable && feature.per_camera && feature.enabled && (
        <div className="feature-cameras">
          <button
            type="button"
            className="live-link"
            aria-expanded={open}
            disabled={!manage && !open}
            onClick={() => onChoose(open ? null : feature.key)}
          >
            {cameraSummary(feature, cameraIds)} {manage ? (open ? "· done" : "· change") : ""}
          </button>
          {open && (
            <fieldset className="feature-camera-list" disabled={!manage || busy}>
              <legend>Run {text.name.toLowerCase()} on</legend>
              <label className="admin-check">
                <input type="radio" name={`cameras-${feature.key}`} checked={feature.cameras === null}
                  onChange={() => onChange(feature, { cameras: null })} />
                All cameras, including new ones
              </label>
              <label className="admin-check">
                <input type="radio" name={`cameras-${feature.key}`} checked={feature.cameras !== null}
                  onChange={() => onChange(feature, { cameras: [...cameraIds] })} />
                Only the cameras I choose
              </label>
              {feature.cameras !== null && (
                <div className="feature-camera-choices">
                  {cameras.length === 0 && <p className="live-muted">No cameras at this site yet.</p>}
                  {cameras.map((camera) => (
                    <label key={camera.id} className="admin-check">
                      <input type="checkbox" checked={feature.cameras?.includes(camera.id) ?? true}
                        onChange={() => onChange(feature, { cameras: toggleCamera(feature.cameras, cameraIds, camera.id) })} />
                      {camera.name}
                    </label>
                  ))}
                </div>
              )}
            </fieldset>
          )}
        </div>
      )}

      {state === "needs_box" && (
        <p className="feature-note" role="note">
          {feature.key === "weapon"
            ? "Your site box does not have the weapon model yet. We install it with you when your plan includes it."
            : "Your site box has not loaded this feature yet. Update the site box, or contact us to set it up."}
        </p>
      )}
      {feature.key === "attendance" && feature.enabled && switchable && (
        <p className="live-muted">Switching it off stops new sightings. Past records stay on the Attendance page.</p>
      )}
      {feature.key === "weapon" && feature.included && (
        <p className="live-muted">Preview: alerts say "possible weapon" and always need a person to check them.</p>
      )}
      {state === "always_on" && <p className="live-muted">Included with every plan. It stays on so a camera never goes quiet without you knowing.</p>}
      {state === "not_in_plan" && (
        askHref
          ? <a className="live-primary feature-action" href={askHref}>Ask to add</a>
          : <p className="live-muted">Ask your SyncCam contact to add it to your plan{contact ? ` (${contact})` : ""}.</p>
      )}
      {state === "coming_soon" && (
        interestedHref
          ? <a className="live-link feature-action" href={interestedHref}>I'm interested</a>
          : <p className="live-muted">Tell your SyncCam contact if you would like this.</p>
      )}
    </article>
  );
}
