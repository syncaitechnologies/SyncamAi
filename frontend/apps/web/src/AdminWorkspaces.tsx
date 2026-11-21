import { useCallback, useEffect, useState } from "react";

import {
  changesFrom,
  dayFromNow,
  draftFrom,
  liveMinutesLabel,
  planLabel,
  usageLabel,
  type AdminDraft,
  type AdminWorkspace,
  type Plan,
} from "./admin-model";
import type { SyncamApi } from "./syncam-api";

// Owner admin screen (ADR-018): every client workspace with its plan, trial,
// limits and live-view minutes. Only platform operators signed in with their
// authenticator can open it; every change is written to that workspace's
// audit log.
export function AdminWorkspaces({ api, onNotify }: { api: SyncamApi | null; onNotify: (message: string) => void }) {
  const [workspaces, setWorkspaces] = useState<AdminWorkspace[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<string | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!api) return;
    try {
      setWorkspaces(await api.adminWorkspaces(signal));
      setError("");
    } catch (caught) {
      if (!signal?.aborted) setError(caught instanceof Error ? caught.message : "Workspaces could not be loaded.");
    }
  }, [api]);

  useEffect(() => {
    const abort = new AbortController();
    void load(abort.signal);
    return () => abort.abort();
  }, [load]);

  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker"><span className="live-pill"><span />OWNER</span><span>All client workspaces</span></div>
          <h1>Owner admin</h1>
          <p>Change a client's plan, trial, limits and live-view minutes. Every change is recorded in that client's audit log.</p>
        </div>
      </section>
      {error && <p className="auth-notice" role="alert">{error}</p>}
      {!workspaces && !error && <p className="live-muted" role="status">Loading workspaces…</p>}
      {workspaces && (
        <div className="admin-list">
          {workspaces.length === 0 && <p className="live-muted">No workspaces yet.</p>}
          {workspaces.map((workspace) => (
            <section className="panel live-card admin-card" key={workspace.tenant_id}>
              <header className="admin-card-head">
                <div>
                  <h2>{workspace.name}</h2>
                  <p className="live-muted">{workspace.slug} · created {workspace.created_at.slice(0, 10)}</p>
                </div>
                <span className={`admin-plan admin-plan-${workspace.plan}`}>{planLabel(workspace)}</span>
              </header>
              <dl className="live-facts admin-facts">
                <div><dt>Sites</dt><dd>{usageLabel(workspace.sites, workspace.site_limit, "sites")}</dd></div>
                <div><dt>Cameras</dt><dd>{usageLabel(workspace.cameras, workspace.camera_limit, "cameras")}</dd></div>
                <div><dt>Live view this month</dt><dd>{liveMinutesLabel(workspace)}</dd></div>
              </dl>
              {editing === workspace.tenant_id ? (
                <AdminEditor
                  api={api}
                  workspace={workspace}
                  onCancel={() => setEditing(null)}
                  onSaved={() => {
                    setEditing(null);
                    onNotify(`${workspace.name} updated · recorded in its audit log`);
                    void load();
                  }}
                />
              ) : (
                <button type="button" className="live-primary" onClick={() => setEditing(workspace.tenant_id)}>Change plan or limits</button>
              )}
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

function AdminEditor({ api, workspace, onCancel, onSaved }: {
  api: SyncamApi | null;
  workspace: AdminWorkspace;
  onCancel: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState<AdminDraft>(() => draftFrom(workspace));
  const [notice, setNotice] = useState("");
  const [saving, setSaving] = useState(false);
  const update = (field: keyof AdminDraft, value: string) => {
    setDraft((current) => ({ ...current, [field]: value }));
    setNotice("");
  };

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = changesFrom(workspace, draft);
    if ("error" in result) {
      setNotice(result.error);
      return;
    }
    if (!api) return;
    setSaving(true);
    try {
      await api.updateAdminWorkspace(workspace.tenant_id, result.changes);
      onSaved();
    } catch (caught) {
      setNotice(caught instanceof Error ? caught.message : "The change could not be saved.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <form className="admin-form" onSubmit={save} noValidate>
      <label>Plan
        <select value={draft.plan} onChange={(event) => update("plan", event.target.value as Plan)}>
          <option value="trial">Trial</option>
          <option value="active">Active (paying)</option>
          <option value="suspended">Suspended</option>
        </select>
      </label>
      <label>Trial ends
        <span className="admin-row">
          <input type="date" value={draft.trialEndsOn} onChange={(event) => update("trialEndsOn", event.target.value)} />
          <button type="button" className="secondary" onClick={() => update("trialEndsOn", dayFromNow(14))}>+14 days</button>
        </span>
      </label>
      <label>Site limit<input inputMode="numeric" placeholder="Unlimited" value={draft.siteLimit} onChange={(event) => update("siteLimit", event.target.value)} /></label>
      <label>Camera limit<input inputMode="numeric" placeholder="Unlimited" value={draft.cameraLimit} onChange={(event) => update("cameraLimit", event.target.value)} /></label>
      <label>Live-view minutes a month<input inputMode="numeric" placeholder="Unlimited" value={draft.liveViewMinutes} onChange={(event) => update("liveViewMinutes", event.target.value)} /></label>
      <p className="live-muted admin-hint">Leave a limit empty for unlimited. Live-view minutes 0 turns live view off.</p>
      {notice && <p className="auth-notice" role="alert">{notice}</p>}
      <div className="live-actions">
        <button type="submit" className="live-primary" disabled={saving}>{saving ? "Saving…" : "Save changes"}</button>
        <button type="button" className="secondary" disabled={saving} onClick={onCancel}>Cancel</button>
      </div>
    </form>
  );
}
