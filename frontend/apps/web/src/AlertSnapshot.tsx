import { useEffect, useState } from "react";

import type { SyncamApi } from "./syncam-api";

/** The annotated JPEG the site box captured for an alert. */
export function AlertSnapshot({ api, alertId, caption }: { api: SyncamApi; alertId: string; caption: string }) {
  const [state, setState] = useState<{ id: string; url: string | null; error: string } | null>(null);

  useEffect(() => {
    const abort = new AbortController();
    let created: string | null = null;
    setState(null);
    api.alertSnapshot(alertId, abort.signal)
      .then((url) => {
        created = url;
        if (!abort.signal.aborted) setState({ id: alertId, url, error: "" });
      })
      .catch((error: unknown) => {
        if (!abort.signal.aborted) {
          setState({ id: alertId, url: null, error: error instanceof Error ? error.message : "Snapshot unavailable" });
        }
      });
    return () => {
      abort.abort();
      if (created) URL.revokeObjectURL(created);
    };
  }, [api, alertId]);

  if (!state || state.id !== alertId) {
    return <div className="live-snapshot live-snapshot-empty" role="status">Loading snapshot…</div>;
  }
  if (!state.url) {
    return (
      <div className="live-snapshot live-snapshot-empty" role="status">
        {state.error || "No snapshot for this alert (for example, a camera that went offline)."}
      </div>
    );
  }
  return (
    <a className="live-snapshot" href={state.url} target="_blank" rel="noreferrer" title="Open full size">
      <img src={state.url} alt={`Snapshot: ${caption}`} />
    </a>
  );
}
