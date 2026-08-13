import { useEffect, useMemo, useRef, useState } from "react";

import type { AlertItem, AlertStatus, Severity } from "./alert-contracts";
import { useAlertFeed } from "./use-alert-feed";

type Filter = "all" | "critical" | "unacknowledged" | "acknowledged";

const seedAlerts: AlertItem[] = [
  {
    id: "alert-1048",
    title: "Person down detected",
    summary: "A person has remained on the floor near the west loading bay.",
    severity: "Critical",
    status: "unacknowledged",
    type: "Fall detection",
    site: "Northstar Distribution",
    zone: "West loading bay",
    camera: "CAM-07 · Dock west",
    occurredAt: "2026-08-13T14:31:00-04:00",
    confidence: 0.97,
    model: "fall-v1.4.2",
    threshold: 0.82,
    eventId: "EVT-8F2A-1048",
    color: "#ff6b5e",
    evidence: "10s clip ready",
  },
  {
    id: "alert-1047",
    title: "Restricted zone entry",
    summary: "Movement detected inside the solvent storage perimeter.",
    severity: "High",
    status: "unacknowledged",
    type: "Zone intrusion",
    site: "Northstar Distribution",
    zone: "Solvent storage",
    camera: "CAM-12 · Aisle 4",
    occurredAt: "2026-08-13T14:28:00-04:00",
    confidence: 0.91,
    model: "intrusion-v2.1.0",
    threshold: 0.78,
    eventId: "EVT-8F2A-1047",
    color: "#f2a93b",
    evidence: "10s clip ready",
  },
  {
    id: "alert-1046",
    title: "PPE compliance exception",
    summary:
      "Two workers entered the welding bay without required eye protection.",
    severity: "High",
    status: "acknowledged",
    type: "PPE compliance",
    site: "Northstar Distribution",
    zone: "Welding bay",
    camera: "CAM-03 · Fabrication",
    occurredAt: "2026-08-13T14:22:00-04:00",
    confidence: 0.88,
    model: "ppe-v3.0.1",
    threshold: 0.8,
    eventId: "EVT-8F2A-1046",
    color: "#f2a93b",
    evidence: "Snapshot ready",
  },
  {
    id: "alert-1045",
    title: "Camera health degraded",
    summary: "Frame rate has dropped below the site reliability threshold.",
    severity: "Medium",
    status: "acknowledged",
    type: "Camera health",
    site: "Northstar Distribution",
    zone: "East entrance",
    camera: "CAM-02 · East gate",
    occurredAt: "2026-08-13T14:10:00-04:00",
    confidence: 0.99,
    model: "health-agent-v1.0",
    threshold: 0.95,
    eventId: "EVT-8F2A-1045",
    color: "#62c9a4",
    evidence: "Health timeline ready",
  },
  {
    id: "alert-1044",
    title: "Smoke pattern detected",
    summary: "Low-density smoke pattern detected near the charging station.",
    severity: "Critical",
    status: "dispatched",
    type: "Fire and smoke",
    site: "Northstar Distribution",
    zone: "Battery charging",
    camera: "CAM-18 · Charging room",
    occurredAt: "2026-08-13T13:56:00-04:00",
    confidence: 0.93,
    model: "smoke-v2.3.0",
    threshold: 0.84,
    eventId: "EVT-8F2A-1044",
    color: "#ff6b5e",
    evidence: "10s clip ready",
  },
];

const statusLabels: Record<AlertStatus, string> = {
  unacknowledged: "Unacknowledged",
  acknowledged: "Acknowledged",
  dispatched: "Dispatched",
  arrived: "Arrived",
  resolved: "Resolved",
  snoozed: "Snoozed",
  dismissed: "Dismissed",
};
const firstAlert = seedAlerts[0]!;
const emptyAlert: AlertItem = {
  id: "queue-state",
  title: "Alert queue clear",
  summary: "No alerts require review in this view.",
  severity: "Medium",
  status: "resolved",
  type: "Queue status",
  site: "Configured site",
  zone: "No active event",
  camera: "No camera selected",
  occurredAt: new Date().toISOString(),
  confidence: 0,
  model: "Not applicable",
  threshold: 0,
  eventId: "No event selected",
  color: "#62c9a4",
  evidence: "No evidence",
  placeholder: true,
};

function Icon({ name, size = 18 }: { name: string; size?: number }) {
  const paths: Record<string, string> = {
    grid: "M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z",
    bell: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4",
    camera:
      "M4 7h3l1.5-2h7L17 7h3v11H4zM12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z",
    chart: "M4 19V5M4 19h17M8 16v-4M12 16V8M16 16v-7M20 16v-3",
    file: "M6 3h9l4 4v14H6zM14 3v5h5M9 13h6M9 17h6",
    settings:
      "M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-1.7 1.7-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.1h-2.4v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L8 17l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H6.7v-2.4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L8 8.6l1.7-1.7.1.1a1.7 1.7 0 0 0 1.9.3 1.7 1.7 0 0 0 1-1.6v-.1h2.4v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1 1.7 1.7-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1V14h-.1a1.7 1.7 0 0 0-1.6 1Z",
    search: "m20 20-4.5-4.5M11 18a7 7 0 1 1 0-14 7 7 0 0 1 0 14Z",
    chevron: "m9 18 6-6-6-6",
    arrow: "M5 12h14m-6-6 6 6-6 6",
    check: "m5 12 4 4L19 6",
    more: "M5 12h.01M12 12h.01M19 12h.01",
    shield: "M12 3 20 6v5c0 5-3.4 8.5-8 10-4.6-1.5-8-5-8-10V6z",
    close: "m6 6 12 12M18 6 6 18",
    clock: "M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
    external: "M14 4h6v6M20 4l-9 9M18 13v6H5V6h6",
  };
  return (
    <svg
      aria-hidden="true"
      className="icon"
      height={size}
      viewBox="0 0 24 24"
      width={size}
    >
      <path d={paths[name] ?? paths.more} />
    </svg>
  );
}

function SeverityMark({ severity }: { severity: Severity }) {
  return (
    <span
      className={`severity-mark severity-${severity.toLowerCase()}`}
      aria-label={`${severity} severity`}
    >
      {severity === "Critical" ? "!" : severity === "High" ? "▲" : "•"}
    </span>
  );
}

function formatAge(timestamp: string) {
  const minutes = Math.max(
    1,
    Math.round((Date.now() - new Date(timestamp).getTime()) / 60000),
  );
  return minutes < 60
    ? `${minutes}m ago`
    : `${Math.floor(minutes / 60)}h ${minutes % 60}m ago`;
}

export function App() {
  const {
    alerts,
    setAlerts,
    acknowledge: acknowledgeThroughFeed,
    connection,
    feedError,
    newAlertCount,
    queueLoaded,
    dataMode,
  } = useAlertFeed(seedAlerts);
  const [selectedId, setSelectedId] = useState(firstAlert.id);
  const [filter, setFilter] = useState<Filter>("all");
  const [site, setSite] = useState("Northstar Distribution");
  const [toast, setToast] = useState(
    dataMode === "demo"
      ? "Local demo feed active"
      : "Connecting to realtime alerts",
  );
  const [dismissId, setDismissId] = useState<string | null>(null);
  const dismissTriggerRef = useRef<HTMLButtonElement>(null);
  const dismissModalRef = useRef<HTMLDivElement>(null);
  const filteredAlerts = useMemo(
    () =>
      alerts.filter(
        (alert) =>
          filter === "all" ||
          (filter === "critical"
            ? alert.severity === "Critical"
            : filter === "unacknowledged"
              ? alert.status === "unacknowledged"
              : ["acknowledged", "dispatched"].includes(alert.status)),
      ),
    [alerts, filter],
  );
  const selectedAlert = alerts.find((alert) => alert.id === selectedId) ??
    alerts[0] ?? {
      ...emptyAlert,
      title: queueLoaded
        ? feedError
          ? "Alert queue unavailable"
          : "Alert queue clear"
        : "Loading alert queue",
      summary: queueLoaded
        ? feedError || "No alerts require review in this view."
        : "Establishing the tenant- and site-scoped alert feed.",
    };
  const unacknowledgedCount = alerts.filter(
    (alert) => alert.status === "unacknowledged",
  ).length;
  const criticalCount = alerts.filter(
    (alert) => alert.severity === "Critical" && alert.status !== "dismissed",
  ).length;
  const connectionLabel =
    connection === "demo"
      ? "Local demo feed"
      : connection === "connected"
        ? "Realtime connected"
        : connection === "connecting"
          ? "Connecting realtime"
          : connection === "reconnecting"
            ? "Reconnecting realtime"
            : connection === "offline"
              ? "Network offline"
              : "Realtime unavailable";
  const feedHealthy = connection === "demo" || connection === "connected";

  useEffect(() => {
    const timer = window.setTimeout(() => setToast(""), 3500);
    return () => window.clearTimeout(timer);
  }, [toast]);
  useEffect(() => {
    if (feedError) setToast(feedError);
  }, [feedError]);
  useEffect(() => {
    if (!alerts.some((alert) => alert.id === selectedId) && alerts[0])
      setSelectedId(alerts[0].id);
  }, [alerts, selectedId]);
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (dismissId || selectedAlert.placeholder) return;
      const target = event.target as HTMLElement;
      if (["INPUT", "SELECT", "TEXTAREA"].includes(target.tagName)) return;
      const index = filteredAlerts.findIndex(
        (alert) => alert.id === selectedId,
      );
      if (event.key.toLowerCase() === "j" && filteredAlerts.length) {
        event.preventDefault();
        setSelectedId(
          filteredAlerts[Math.min(index + 1, filteredAlerts.length - 1)]!.id,
        );
      }
      if (event.key.toLowerCase() === "k" && filteredAlerts.length) {
        event.preventDefault();
        setSelectedId(filteredAlerts[Math.max(index - 1, 0)]!.id);
      }
      if (event.key.toLowerCase() === "a" && selectedAlert)
        acknowledge(selectedAlert.id);
      if (event.key.toLowerCase() === "e" && selectedAlert)
        escalate(selectedAlert.id);
      if (event.key.toLowerCase() === "d" && selectedAlert)
        setDismissId(selectedAlert.id);
      if (event.code === "Space" && selectedAlert) {
        event.preventDefault();
        setToast("Evidence preview paused");
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [dismissId, filteredAlerts, selectedAlert, selectedId]);
  useEffect(() => {
    if (!dismissId) return;
    const modal = dismissModalRef.current;
    const focusable = modal?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    focusable?.[0]?.focus();
    function handleModalKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        setDismissId(null);
        return;
      }
      if (event.key !== "Tab" || !focusable?.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    }
    document.addEventListener("keydown", handleModalKeyDown);
    return () => {
      document.removeEventListener("keydown", handleModalKeyDown);
      dismissTriggerRef.current?.focus();
    };
  }, [dismissId]);

  function acknowledge(id: string) {
    void acknowledgeThroughFeed(id)
      .then(() => setToast("Alert acknowledged · audit entry recorded"))
      .catch((error: unknown) =>
        setToast(
          error instanceof Error
            ? error.message
            : "Alert acknowledgement failed.",
        ),
      );
  }
  function unavailableInLiveMode(action: string) {
    setToast(`${action} is waiting for its audited backend contract.`);
  }
  function dispatch(id: string) {
    if (dataMode === "live") {
      unavailableInLiveMode("Guard dispatch");
      return;
    }
    setAlerts((current) =>
      current.map((alert) =>
        alert.id === id ? { ...alert, status: "dispatched" } : alert,
      ),
    );
    setToast("Guard dispatch started · nearest on-shift guard notified");
  }
  function escalate(id: string) {
    if (dataMode === "live") {
      unavailableInLiveMode("Escalation");
      return;
    }
    setAlerts((current) =>
      current.map((alert) =>
        alert.id === id ? { ...alert, severity: "Critical" } : alert,
      ),
    );
    setToast("Alert escalated · phone tree notification queued");
  }
  function snooze(id: string) {
    if (dataMode === "live") {
      unavailableInLiveMode("Snooze");
      return;
    }
    setAlerts((current) =>
      current.map((alert) =>
        alert.id === id ? { ...alert, status: "snoozed" } : alert,
      ),
    );
    setToast("Alert snoozed for 15 minutes");
  }
  function dismiss(id: string, reason: string) {
    if (dataMode === "live") {
      setDismissId(null);
      unavailableInLiveMode("Dismissal");
      return;
    }
    setAlerts((current) =>
      current.map((alert) =>
        alert.id === id ? { ...alert, status: "dismissed" } : alert,
      ),
    );
    setDismissId(null);
    setToast(`Alert dismissed · ${reason}`);
  }
  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand-lockup">
          <div className="brand-mark">
            <span>SC</span>
          </div>
          <div>
            <strong>SyncCam</strong>
            <span>AI operations</span>
          </div>
        </div>
        <div className="workspace-select">
          <span className="eyebrow">Workspace</span>
          <button className="site-picker" type="button">
            <span className="site-avatar">N</span>
            <span>
              <strong>Northstar</strong>
              <small>3 sites · Active</small>
            </span>
            <Icon name="chevron" size={15} />
          </button>
        </div>
        <nav className="nav-list">
          <span className="nav-label">Monitor</span>
          <button className="nav-item" type="button">
            <Icon name="grid" />
            Overview
          </button>
          <button className="nav-item active" type="button">
            <Icon name="bell" />
            <span>Alert Center</span>
            <span className="nav-count">{unacknowledgedCount}</span>
          </button>
          <button className="nav-item" type="button">
            <Icon name="camera" />
            Live view
          </button>
          <button className="nav-item" type="button">
            <Icon name="chart" />
            Incidents
          </button>
          <span className="nav-label nav-label-spaced">Manage</span>
          <button className="nav-item" type="button">
            <Icon name="file" />
            Reports
          </button>
          <button className="nav-item" type="button">
            <Icon name="settings" />
            Configuration
          </button>
        </nav>
        <div className="sidebar-bottom">
          <div className="security-card">
            <Icon name="shield" size={16} />
            <span>
              <strong>Audit protection on</strong>
              <small>All actions are logged</small>
            </span>
          </div>
          <button className="profile-button" type="button">
            <span className="user-avatar">RS</span>
            <span>
              <strong>Rajan Shah</strong>
              <small>SOC operator</small>
            </span>
            <Icon name="more" />
          </button>
        </div>
      </aside>
      <main className="main-content">
        <header className="topbar">
          <div className="breadcrumbs">
            <span>Monitor</span>
            <Icon name="chevron" size={14} />
            <strong>Alert Center</strong>
          </div>
          <div className="topbar-actions">
            <div className="connection-status" data-state={connection}>
              <span className="pulse-dot" />
              {connectionLabel}
              {dataMode === "live" && (
                <span className="latency">resume enabled</span>
              )}
            </div>
            <button className="topbar-icon" type="button" aria-label="Search">
              <Icon name="search" />
            </button>
            <button
              className="topbar-icon notification-button"
              type="button"
              aria-label="Notifications"
            >
              <Icon name="bell" />
              {newAlertCount > 0 && <span className="notification-dot" />}
            </button>
            <div className="topbar-date">
              Thu, Aug 13 <span>·</span> 14:32 EDT
            </div>
          </div>
        </header>
        <section className="page-heading">
          <div>
            <div className="heading-kicker">
              <span
                className={
                  dataMode === "live" ? "live-pill" : "live-pill demo-pill"
                }
              >
                <span />
                {dataMode === "live" ? "LIVE" : "DEMO"}
              </span>
              <span>Operations console</span>
            </div>
            <h1>Alert Center</h1>
            <p>Validate and respond to events across your camera network.</p>
          </div>
          <div className="heading-actions">
            <label className="site-filter-label" htmlFor="site-filter">
              Viewing site
            </label>
            <select
              disabled={dataMode === "live"}
              id="site-filter"
              value={dataMode === "live" ? "Configured site" : site}
              onChange={(event) => setSite(event.target.value)}
            >
              {dataMode === "live" ? (
                <option>Configured site</option>
              ) : (
                <>
                  <option>Northstar Distribution</option>
                  <option>Northstar Retail · Queen St</option>
                  <option>Northstar HQ</option>
                </>
              )}{" "}
            </select>
            <button
              className="icon-action"
              type="button"
              aria-label="More page actions"
            >
              <Icon name="more" />
            </button>
          </div>
        </section>
        <div
          className={`status-strip ${feedHealthy ? "" : "status-strip-warning"}`}
          role="status"
          aria-live="polite"
        >
          <div className="status-strip-main">
            <span className="status-icon">
              <Icon name={feedHealthy ? "shield" : "clock"} size={16} />
            </span>
            <span>
              <strong>
                {dataMode === "demo" ? "Local demo data" : connectionLabel}
              </strong>{" "}
              ·{" "}
              {dataMode === "demo"
                ? "Synthetic events only"
                : feedHealthy
                  ? "Tenant- and site-scoped stream active"
                  : "REST queue remains visible while recovery continues"}
            </span>
          </div>
          <span className="status-strip-meta">
            {dataMode === "demo"
              ? "No backend required"
              : "Last sequence stored for resume"}
            <span className="strip-divider" />
            {dataMode === "demo"
              ? "18s simulated arrivals"
              : "Single-use ticket authentication"}
          </span>
        </div>
        <div className="sr-only" role="status" aria-live="polite">
          {newAlertCount > 0
            ? `${newAlertCount} new ${newAlertCount === 1 ? "alert" : "alerts"} received.`
            : ""}
        </div>
        <section
          className="alert-workspace"
          aria-label="Alert triage workspace"
        >
          <div className="queue-panel panel">
            <div className="panel-heading queue-heading">
              <div>
                <div className="panel-kicker">Incoming events</div>
                <h2>
                  Alert queue{" "}
                  <span className="count-badge">{unacknowledgedCount}</span>
                </h2>
              </div>
              <button
                className="small-icon-button"
                type="button"
                aria-label="Queue options"
              >
                <Icon name="more" />
              </button>
            </div>
            <div className="queue-toolbar">
              <div
                className="filter-tabs"
                role="tablist"
                aria-label="Alert filters"
              >
                {(
                  [
                    ["all", "All"],
                    ["critical", "Critical"],
                    ["unacknowledged", "Unacked"],
                    ["acknowledged", "Acked"],
                  ] as const
                ).map(([value, label]) => (
                  <button
                    className={
                      filter === value ? "filter-tab selected" : "filter-tab"
                    }
                    key={value}
                    onClick={() => setFilter(value)}
                    role="tab"
                    aria-selected={filter === value}
                    type="button"
                  >
                    {label}
                    {value === "critical" && <span className="tab-dot" />}
                  </button>
                ))}
              </div>
              <button className="filter-control" type="button">
                <Icon name="search" size={15} />
                Filter
              </button>
            </div>
            <div className="queue-summary">
              <span>
                <strong>{unacknowledgedCount} unacknowledged</strong> ·{" "}
                {criticalCount} critical
              </span>
              <button
                type="button"
                onClick={() => setToast("Queue sorted by severity and age")}
              >
                Severity <Icon name="chevron" size={13} />
              </button>
            </div>
            <div className="alert-list" role="list" aria-label="Alert queue">
              {filteredAlerts.length === 0 ? (
                <div className="empty-queue">
                  <span className="empty-icon">
                    <Icon name="check" />
                  </span>
                  <strong>
                    {queueLoaded
                      ? "No alerts in this view"
                      : "Loading alert queue"}
                  </strong>
                  <span>
                    {queueLoaded
                      ? "Try another filter or wait for a new event."
                      : "Connecting to the tenant-scoped feed."}
                  </span>
                </div>
              ) : (
                filteredAlerts.map((alert) => (
                  <button
                    className={`alert-card ${selectedId === alert.id ? "selected" : ""} ${alert.status === "unacknowledged" ? "is-new" : ""}`}
                    key={alert.id}
                    onClick={() => setSelectedId(alert.id)}
                    role="listitem"
                    type="button"
                  >
                    <div className="alert-card-top">
                      <SeverityMark severity={alert.severity} />
                      <span className="alert-type">{alert.type}</span>
                      <span className="alert-age">
                        {formatAge(alert.occurredAt)}
                      </span>
                    </div>
                    <strong className="alert-card-title">{alert.title}</strong>
                    <span className="alert-card-location">
                      <Icon name="camera" size={13} />
                      {alert.zone}
                      <span>·</span>
                      {alert.camera.split(" · ")[0]}
                    </span>
                    <div className="alert-card-bottom">
                      <span className={`status-chip status-${alert.status}`}>
                        {statusLabels[alert.status]}
                      </span>
                      <span className="confidence">
                        {Math.round(alert.confidence * 100)}% match
                      </span>
                    </div>
                  </button>
                ))
              )}
            </div>
            <div className="keyboard-hints">
              <span>
                <kbd>J</kbd>
                <kbd>K</kbd> navigate
              </span>
              <span>
                <kbd>A</kbd> acknowledge
              </span>
              <span>
                <kbd>D</kbd> dismiss
              </span>
            </div>
          </div>
          <div className="detail-panel panel">
            {selectedAlert.placeholder ? (
              <div className="empty-detail" role="status">
                <div>
                  <span className="empty-icon">
                    <Icon name={queueLoaded ? "check" : "clock"} />
                  </span>
                  <h2>{selectedAlert.title}</h2>
                  <p>{selectedAlert.summary}</p>
                </div>
              </div>
            ) : (
              <>
                <div className="detail-header">
                  <div>
                    <div className="panel-kicker">Selected alert</div>
                    <div className="detail-title-row">
                      <SeverityMark severity={selectedAlert.severity} />
                      <h2>{selectedAlert.title}</h2>
                    </div>
                  </div>
                  <button
                    className="small-icon-button"
                    type="button"
                    aria-label="Alert options"
                  >
                    <Icon name="more" />
                  </button>
                </div>
                <div className="detail-meta">
                  <span
                    className={`status-chip status-${selectedAlert.status}`}
                  >
                    {statusLabels[selectedAlert.status]}
                  </span>
                  <span>{formatAge(selectedAlert.occurredAt)}</span>
                  <span>·</span>
                  <span>{selectedAlert.eventId}</span>
                </div>
                <div className="evidence-card">
                  <div className="evidence-art">
                    <div className="evidence-grid" />
                    <div className="camera-label">
                      <span className="record-dot" />
                      {selectedAlert.camera}
                    </div>
                    <div className="evidence-bounds">
                      <span>PERSON</span>
                    </div>
                    <button
                      className="play-button"
                      type="button"
                      onClick={() => setToast("Evidence preview paused")}
                      aria-label="Play evidence preview"
                    >
                      ▶
                    </button>
                    <span className="evidence-duration">00:10</span>
                  </div>
                  <div className="evidence-footer">
                    <span>
                      <Icon name="file" size={14} />
                      {selectedAlert.evidence}
                    </span>
                    <button
                      type="button"
                      onClick={() => setToast("Evidence export prepared")}
                    >
                      Export evidence <Icon name="external" size={13} />
                    </button>
                  </div>
                </div>
                <div className="detail-copy">
                  <p>{selectedAlert.summary}</p>
                  <div className="detail-facts">
                    <div>
                      <span>Detected in</span>
                      <strong>{selectedAlert.zone}</strong>
                    </div>
                    <div>
                      <span>Detection confidence</span>
                      <strong>
                        {Math.round(selectedAlert.confidence * 100)}%{" "}
                        <em>
                          +
                          {Math.round(
                            (selectedAlert.confidence -
                              selectedAlert.threshold) *
                              100,
                          )}
                          %
                        </em>
                      </strong>
                    </div>
                  </div>
                </div>
                <div className="action-group">
                  {selectedAlert.status === "unacknowledged" ? (
                    <button
                      className="primary-action"
                      type="button"
                      onClick={() => acknowledge(selectedAlert.id)}
                    >
                      <Icon name="check" size={16} />
                      Acknowledge <span>A</span>
                    </button>
                  ) : selectedAlert.status === "acknowledged" ? (
                    <button
                      className="primary-action dispatch-action"
                      type="button"
                      onClick={() => dispatch(selectedAlert.id)}
                    >
                      <Icon name="arrow" size={16} />
                      Dispatch guard
                    </button>
                  ) : (
                    <button
                      className="primary-action"
                      type="button"
                      onClick={() => setToast("Alert already in response flow")}
                    >
                      <Icon name="check" size={16} />
                      Response in progress
                    </button>
                  )}
                  <button
                    className="secondary-action"
                    type="button"
                    onClick={() => escalate(selectedAlert.id)}
                  >
                    Escalate <span>E</span>
                  </button>
                  <button
                    ref={dismissTriggerRef}
                    className="secondary-action"
                    type="button"
                    onClick={() => setDismissId(selectedAlert.id)}
                  >
                    Dismiss <span>D</span>
                  </button>
                  <button
                    className="more-action"
                    type="button"
                    onClick={() => snooze(selectedAlert.id)}
                    aria-label="Snooze alert"
                  >
                    <Icon name="clock" size={16} />
                  </button>
                </div>
                <div className="detail-section">
                  <div className="section-heading">
                    <h3>Event details</h3>
                    <span className="verified">
                      <Icon name="check" size={13} />
                      Verified
                    </span>
                  </div>
                  <div className="detail-grid">
                    <div>
                      <span>Camera</span>
                      <strong>{selectedAlert.camera}</strong>
                    </div>
                    <div>
                      <span>Rule</span>
                      <strong>{selectedAlert.type}</strong>
                    </div>
                    <div>
                      <span>Model</span>
                      <strong>{selectedAlert.model}</strong>
                    </div>
                    <div>
                      <span>Site threshold</span>
                      <strong>
                        {Math.round(selectedAlert.threshold * 100)}% confidence
                      </strong>
                    </div>
                  </div>
                </div>
                <div className="detail-section timeline-section">
                  <div className="section-heading">
                    <h3>Event timeline</h3>
                    <button
                      type="button"
                      onClick={() => setToast("Full event timeline opened")}
                    >
                      View full <Icon name="arrow" size={13} />
                    </button>
                  </div>
                  <div className="timeline">
                    <span className="timeline-label">14:20</span>
                    <div className="timeline-track">
                      <span className="timeline-band pre" />
                      <span className="timeline-marker" />
                      <span className="timeline-band post" />
                    </div>
                    <span className="timeline-label">14:40</span>
                  </div>
                  <div className="timeline-caption">
                    <span>Pre-event</span>
                    <strong>14:31:00 · Alert confirmed</strong>
                    <span>Post-event</span>
                  </div>
                </div>
              </>
            )}
          </div>
          <aside className="context-panel panel">
            {selectedAlert.placeholder ? (
              <div className="empty-detail" aria-hidden="true" />
            ) : (
              <>
                <div className="panel-heading">
                  <div>
                    <div className="panel-kicker">Live context</div>
                    <h2>Response context</h2>
                  </div>
                  <button
                    className="small-icon-button"
                    type="button"
                    aria-label="Collapse context"
                  >
                    <Icon name="chevron" />
                  </button>
                </div>
                <div className="context-block">
                  <div className="context-heading">
                    <h3>Camera health</h3>
                    <span className="healthy-dot">Healthy</span>
                  </div>
                  <div className="camera-health">
                    <div className="health-ring">
                      <strong>98</strong>
                      <span>score</span>
                    </div>
                    <div className="health-stats">
                      <span>
                        <b>99.8%</b> uptime
                      </span>
                      <span>
                        <b>12 fps</b> current
                      </span>
                      <span>
                        <b>24 ms</b> latency
                      </span>
                    </div>
                  </div>
                  <button
                    className="text-button"
                    type="button"
                    onClick={() => setToast("Camera health details opened")}
                  >
                    Open camera details <Icon name="arrow" size={13} />
                  </button>
                </div>
                <div className="context-block dispatch-block">
                  <div className="context-heading">
                    <h3>Guard dispatch</h3>
                    <span className="status-chip status-dispatched">
                      {selectedAlert.status === "dispatched"
                        ? "Active"
                        : "Standby"}
                    </span>
                  </div>
                  {selectedAlert.status === "dispatched" ? (
                    <div className="dispatch-person">
                      <span className="guard-avatar">AM</span>
                      <div>
                        <strong>Arjun Mehta</strong>
                        <span>En route · 2 min ETA</span>
                      </div>
                      <Icon name="arrow" size={15} />
                    </div>
                  ) : (
                    <div className="dispatch-empty">
                      <span className="dispatch-icon">
                        <Icon name="shield" size={17} />
                      </span>
                      <span>
                        <strong>No guard assigned</strong>
                        <small>
                          Acknowledge to dispatch the nearest on-shift guard.
                        </small>
                      </span>
                    </div>
                  )}
                </div>
                <div className="context-block">
                  <div className="context-heading">
                    <h3>Related events</h3>
                    <span>Last 24 hours</span>
                  </div>
                  <div className="related-event">
                    <SeverityMark severity="Medium" />
                    <span>
                      <strong>Door held open</strong>
                      <small>East personnel door · 13:58</small>
                    </span>
                    <Icon name="chevron" size={14} />
                  </div>
                  <div className="related-event">
                    <SeverityMark severity="High" />
                    <span>
                      <strong>Zone entry</strong>
                      <small>West loading bay · 11:42</small>
                    </span>
                    <Icon name="chevron" size={14} />
                  </div>
                </div>
                <div className="context-block notes-block">
                  <div className="context-heading">
                    <h3>Operator notes</h3>
                    <button
                      type="button"
                      onClick={() => setToast("Note composer opened")}
                    >
                      Add note <span>+</span>
                    </button>
                  </div>
                  <p>No notes on this event yet.</p>
                </div>
                <div className="context-footer">
                  <Icon name="shield" size={14} />
                  <span>
                    Access is tenant-scoped. Evidence viewing is logged.
                  </span>
                </div>
              </>
            )}
          </aside>
        </section>
      </main>
      {toast && (
        <div className="toast">
          <span className="toast-check">
            <Icon name="check" size={14} />
          </span>
          {toast}
          <button
            type="button"
            onClick={() => setToast("")}
            aria-label="Dismiss notification"
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      )}
      {dismissId && (
        <div className="modal-backdrop" role="presentation">
          <div
            ref={dismissModalRef}
            className="dismiss-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="dismiss-title"
            aria-describedby="dismiss-description"
          >
            <button
              className="modal-close"
              type="button"
              onClick={() => setDismissId(null)}
              aria-label="Close dialog"
            >
              <Icon name="close" />
            </button>
            <div className="modal-icon">
              <Icon name="file" size={20} />
            </div>
            <h2 id="dismiss-title">Dismiss this alert?</h2>
            <p id="dismiss-description">
              A reason is required and will be added to the append-only audit
              record.
            </p>
            <div className="reason-list">
              {[
                "False positive",
                "Duplicate event",
                "Handled / no action needed",
              ].map((reason) => (
                <button
                  key={reason}
                  type="button"
                  onClick={() => dismiss(dismissId, reason)}
                >
                  {reason}
                  <Icon name="chevron" size={15} />
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
