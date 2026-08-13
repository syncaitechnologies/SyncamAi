import { useMemo, useState } from "react";

import type { AlertItem, Severity } from "./alert-contracts";
import {
  buildDashboardSummary,
  filterAlertsForRange,
  type DashboardRange,
} from "./dashboard-model";
import { Icon } from "./icon";

interface OperationsDashboardProps {
  alerts: AlertItem[];
  connectionLabel: string;
  dataMode: "demo" | "live";
  feedHealthy: boolean;
  site: string;
  onOpenAlertCenter: (alertId?: string) => void;
  onSiteChange: (site: string) => void;
  onNotify: (message: string) => void;
}

const severityLabels: Severity[] = ["Critical", "High", "Medium"];
const demoCameraHealth = [
  { name: "CAM-02 · East gate", detail: "Frame rate below threshold", state: "Degraded" },
  { name: "CAM-11 · Yard north", detail: "Last signal 7 minutes ago", state: "Offline" },
  { name: "CAM-18 · Charging room", detail: "Healthy · 99.9% uptime", state: "Online" },
];

function formatConfidence(value: number | null) {
  return value === null ? "—" : `${Math.round(value * 100)}%`;
}

function formatAge(timestamp: string) {
  const minutes = Math.max(
    1,
    Math.round((Date.now() - new Date(timestamp).getTime()) / 60_000),
  );
  return minutes < 60 ? `${minutes}m` : `${Math.floor(minutes / 60)}h`;
}

export function OperationsDashboard({
  alerts,
  connectionLabel,
  dataMode,
  feedHealthy,
  site,
  onOpenAlertCenter,
  onSiteChange,
  onNotify,
}: OperationsDashboardProps) {
  const [range, setRange] = useState<DashboardRange>("24h");
  const summary = useMemo(
    () => buildDashboardSummary(filterAlertsForRange(alerts, range)),
    [alerts, range],
  );
  const maximumModuleCount = Math.max(
    1,
    ...summary.moduleCounts.map((module) => module.count),
  );
  const readinessScore = dataMode === "demo" ? 86 : null;

  return (
    <>
      <section className="page-heading dashboard-heading">
        <div>
          <div className="heading-kicker">
            <span
              className={dataMode === "live" ? "live-pill" : "live-pill demo-pill"}
            >
              <span />
              {dataMode === "live" ? "LIVE" : "DEMO"}
            </span>
            <span>Role view · SOC operator</span>
          </div>
          <h1>Operations overview</h1>
          <p>See what needs attention across the site in under five seconds.</p>
        </div>
        <div className="heading-actions">
          <label className="site-filter-label" htmlFor="dashboard-site-filter">
            Viewing site
          </label>
          <select
            disabled={dataMode === "live"}
            id="dashboard-site-filter"
            value={dataMode === "live" ? "Configured site" : site}
            onChange={(event) => onSiteChange(event.target.value)}
          >
            {dataMode === "live" ? (
              <option>Configured site</option>
            ) : (
              <>
                <option>Northstar Distribution</option>
                <option>Northstar Retail · Queen St</option>
                <option>Northstar HQ</option>
              </>
            )}
          </select>
          <button
            className="primary-page-action"
            type="button"
            onClick={() => onNotify("Live Wall is scheduled for the next frontend slice.")}
          >
            <Icon name="camera" size={16} />
            Open Live Wall
          </button>
        </div>
      </section>

      <section className="dashboard-shell" aria-label="Operations dashboard">
        <div
          className={`dashboard-hero ${feedHealthy ? "" : "dashboard-hero-warning"}`}
          role="status"
        >
          <div>
            <span className="dashboard-overline">Shift command summary</span>
            <h2>{dataMode === "live" ? "Configured site" : site}</h2>
            <p>
              {feedHealthy
                ? `${connectionLabel}. Alert-derived metrics are current.`
                : "Realtime recovery is in progress. The last REST queue remains visible."}
            </p>
          </div>
          <div className="hero-signal">
            <span className={feedHealthy ? "signal-orb" : "signal-orb signal-warning"}>
              <Icon name={feedHealthy ? "activity" : "clock"} size={25} />
            </span>
            <span>
              <strong>{feedHealthy ? "Monitoring active" : "Feed delayed"}</strong>
              <small>{summary.activeIncidents} open queue signals</small>
            </span>
          </div>
        </div>

        <div className="dashboard-toolbar" aria-label="Dashboard filters">
          <div>
            <span className="dashboard-overline">Time window</span>
            <div className="range-tabs" role="group" aria-label="Time window">
              {(["24h", "7d", "30d"] as const).map((value) => (
                <button
                  className={range === value ? "selected" : ""}
                  key={value}
                  onClick={() => setRange(value)}
                  type="button"
                  aria-pressed={range === value}
                >
                  {value}
                </button>
              ))}
            </div>
          </div>
          <p>
            {dataMode === "demo"
              ? `Queue-derived metrics include synthetic alerts from the selected ${range} window.`
              : "Historical analytics are not connected; the selected window applies to the current queue."}
          </p>
        </div>

        <div className="kpi-grid">
          <button className="kpi-card kpi-urgent" type="button" onClick={() => onOpenAlertCenter()}>
            <span className="kpi-icon"><Icon name="activity" /></span>
            <span className="kpi-copy">
              <small>Active incidents</small>
              <strong>{summary.activeIncidents}</strong>
              <em>{summary.criticalOpen} critical now</em>
            </span>
            <Icon name="arrow" size={15} />
          </button>
          <button className="kpi-card" type="button" onClick={() => onOpenAlertCenter()}>
            <span className="kpi-icon"><Icon name="bell" /></span>
            <span className="kpi-copy">
              <small>Unacknowledged</small>
              <strong>{summary.unacknowledged}</strong>
              <em>Requires operator review</em>
            </span>
            <Icon name="arrow" size={15} />
          </button>
          <div className="kpi-card">
            <span className="kpi-icon"><Icon name="shield" /></span>
            <span className="kpi-copy">
              <small>Queue confidence</small>
              <strong>{formatConfidence(summary.averageConfidence)}</strong>
              <em>Mean active signal confidence</em>
            </span>
          </div>
          <div className="kpi-card kpi-placeholder">
            <span className="kpi-icon"><Icon name="camera" /></span>
            <span className="kpi-copy">
              <small>Detection uptime</small>
              <strong>{dataMode === "demo" ? "98.7%" : "—"}</strong>
              <em>{dataMode === "demo" ? "Synthetic telemetry" : "Telemetry not connected"}</em>
            </span>
            <span className="source-badge">{dataMode === "demo" ? "DEMO" : "PENDING"}</span>
          </div>
        </div>

        <div className="dashboard-grid">
          <section className="dashboard-card readiness-card" aria-labelledby="readiness-title">
            <div className="dashboard-card-heading">
              <div>
                <span className="dashboard-overline">Response posture</span>
                <h2 id="readiness-title">Shift readiness</h2>
              </div>
              <span className="source-badge">{dataMode === "demo" ? "DEMO MODEL" : "NOT CONNECTED"}</span>
            </div>
            <div className="readiness-layout">
              <div
                className="readiness-ring"
                style={{ "--score": `${readinessScore ?? 0}%` } as React.CSSProperties}
                aria-label={
                  readinessScore === null
                    ? "Readiness score unavailable"
                    : `Synthetic readiness score ${readinessScore} out of 100`
                }
              >
                <span><strong>{readinessScore ?? "—"}</strong><small>/ 100</small></span>
              </div>
              <div className="readiness-copy">
                <strong>{readinessScore === null ? "Awaiting analytics contract" : "Response posture is stable"}</strong>
                <p>
                  {readinessScore === null
                    ? "A verified dashboard aggregate endpoint is required before this score can be shown in live mode."
                    : "Synthetic staffing, acknowledgement, and camera signals indicate normal shift readiness."}
                </p>
                <button type="button" onClick={() => onOpenAlertCenter()}>
                  Review active signals <Icon name="arrow" size={13} />
                </button>
              </div>
            </div>
          </section>

          <section className="dashboard-card module-card" aria-labelledby="module-title">
            <div className="dashboard-card-heading">
              <div>
                <span className="dashboard-overline">Current queue</span>
                <h2 id="module-title">Signals by module</h2>
              </div>
              <span className="verified-source"><Icon name="check" size={13} /> Alert feed</span>
            </div>
            {summary.moduleCounts.length ? (
              <div className="module-bars">
                {summary.moduleCounts.slice(0, 5).map((module) => (
                  <div className="module-row" key={module.label}>
                    <div><span>{module.label}</span><strong>{module.count}</strong></div>
                    <span className="module-track">
                      <span style={{ width: `${(module.count / maximumModuleCount) * 100}%` }} />
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="dashboard-empty"><Icon name="check" /><strong>No active signals</strong><span>The monitored queue is clear.</span></div>
            )}
            <details className="data-table-disclosure">
              <summary>View data table</summary>
              <table>
                <thead><tr><th>Module</th><th>Alerts</th><th>Share</th></tr></thead>
                <tbody>
                  {summary.moduleCounts.map((module) => (
                    <tr key={module.label}><td>{module.label}</td><td>{module.count}</td><td>{module.percent}%</td></tr>
                  ))}
                </tbody>
              </table>
            </details>
          </section>

          <section className="dashboard-card incident-feed-card" aria-labelledby="feed-title">
            <div className="dashboard-card-heading">
              <div>
                <span className="dashboard-overline">Newest first</span>
                <h2 id="feed-title">Live incident feed</h2>
              </div>
              <button type="button" onClick={() => onOpenAlertCenter()}>
                View all <Icon name="arrow" size={13} />
              </button>
            </div>
            <div className="dashboard-incident-list">
              {summary.recentAlerts.length ? summary.recentAlerts.map((alert) => (
                <button key={alert.id} type="button" onClick={() => onOpenAlertCenter(alert.id)}>
                  <span className={`feed-severity severity-${alert.severity.toLowerCase()}`} aria-label={`${alert.severity} severity`}>
                    {alert.severity === "Critical" ? "!" : alert.severity === "High" ? "▲" : "•"}
                  </span>
                  <span className="feed-copy"><strong>{alert.title}</strong><small>{alert.camera} · {alert.zone}</small></span>
                  <span className="feed-age">{formatAge(alert.occurredAt)}</span>
                  <Icon name="chevron" size={13} />
                </button>
              )) : (
                <div className="dashboard-empty"><Icon name="check" /><strong>No alerts — all systems nominal</strong><span>New tenant-scoped signals will appear here.</span></div>
              )}
            </div>
          </section>

          <section className="dashboard-card severity-card" aria-labelledby="severity-title">
            <div className="dashboard-card-heading">
              <div>
                <span className="dashboard-overline">Open queue</span>
                <h2 id="severity-title">Severity distribution</h2>
              </div>
              <span className="verified-source"><Icon name="check" size={13} /> Alert feed</span>
            </div>
            <div className="severity-stack" aria-label="Open alerts by severity">
              {severityLabels.map((severity) => {
                const count = summary.severityCounts[severity];
                const width = summary.activeIncidents
                  ? Math.max(8, Math.round((count / summary.activeIncidents) * 100))
                  : 0;
                return count ? <span key={severity} className={`severity-block severity-block-${severity.toLowerCase()}`} style={{ width: `${width}%` }} title={`${severity}: ${count}`} /> : null;
              })}
            </div>
            <ul className="severity-legend">
              {severityLabels.map((severity) => (
                <li key={severity}><span className={`legend-dot legend-${severity.toLowerCase()}`} /><span>{severity}</span><strong>{summary.severityCounts[severity]}</strong></li>
              ))}
            </ul>
          </section>

          <section className="dashboard-card camera-health-card" aria-labelledby="camera-health-title">
            <div className="dashboard-card-heading">
              <div>
                <span className="dashboard-overline">Fleet attention</span>
                <h2 id="camera-health-title">Camera health</h2>
              </div>
              <span className="source-badge">{dataMode === "demo" ? "SYNTHETIC" : "PENDING API"}</span>
            </div>
            {dataMode === "demo" ? (
              <div className="camera-mini-list">
                {demoCameraHealth.map((camera) => (
                  <div key={camera.name}>
                    <span className={`camera-state camera-${camera.state.toLowerCase()}`} />
                    <span><strong>{camera.name}</strong><small>{camera.detail}</small></span>
                    <em>{camera.state}</em>
                  </div>
                ))}
              </div>
            ) : (
              <div className="dashboard-empty dashboard-empty-inline">
                <Icon name="camera" />
                <strong>Camera telemetry is not connected</strong>
                <span>No synthetic values are shown in live mode.</span>
              </div>
            )}
          </section>
        </div>
      </section>
    </>
  );
}
