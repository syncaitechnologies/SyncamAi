import { useMemo, useRef, useState, type KeyboardEvent } from "react";

import type { AlertItem, Severity } from "./alert-contracts";
import {
  buildCameraWallTiles,
  filterCameraWallTiles,
  groupCameraWallAlerts,
  wallColumnCount,
  type CameraTile,
  type CameraWallFilter,
  type CameraWallLayout,
} from "./camera-wall-model";
import { Icon } from "./icon";

interface CameraWallProps {
  alerts: AlertItem[];
  dataMode: "demo" | "live";
  site: string;
  onOpenAlertCenter: (alertId?: string) => void;
  onNotify: (message: string) => void;
}

const layouts: CameraWallLayout[] = [1, 4, 9, 16, 25];
const statusLabels: Record<CameraTile["status"], string> = {
  online: "Online",
  recording: "Recording",
  offline: "Offline",
  degraded: "Degraded",
  masked: "Masked",
};

function severityClass(severity: Severity) {
  return `wall-event-${severity.toLowerCase()}`;
}

function CameraVisual({ tile }: { tile: CameraTile }) {
  return (
    <div
      className={`camera-visual camera-tone-${tile.tone} camera-${tile.status}`}
      aria-hidden="true"
    >
      <span className="camera-horizon" />
      <span className="camera-structure camera-structure-one" />
      <span className="camera-structure camera-structure-two" />
      <span className="camera-grid-lines" />
      <span className="privacy-preview-label">
        {tile.status === "masked" ? "Masked zone" : "No footage · demo visual"}
      </span>
    </div>
  );
}

function ActivityBars({ values }: { values: number[] }) {
  return (
    <span className="camera-activity" aria-hidden="true">
      {values.map((value, index) => (
        <span key={`${value}-${index}`} style={{ height: `${Math.max(8, value)}%` }} />
      ))}
    </span>
  );
}

export function CameraWall({
  alerts,
  dataMode,
  site,
  onOpenAlertCenter,
  onNotify,
}: CameraWallProps) {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<CameraWallFilter>("all");
  const [layout, setLayout] = useState<CameraWallLayout>(9);
  const [showAlerts, setShowAlerts] = useState(true);
  const [spotlightId, setSpotlightId] = useState<string | null>(null);
  const wallRef = useRef<HTMLDivElement>(null);
  const tiles = useMemo(
    () => buildCameraWallTiles(alerts, dataMode),
    [alerts, dataMode],
  );
  const filteredTiles = useMemo(
    () => filterCameraWallTiles(tiles, query, filter),
    [filter, query, tiles],
  );
  const visibleTiles = filteredTiles.slice(0, layout);
  const spotlight = tiles.find(({ id }) => id === spotlightId) ?? null;
  const activeAlerts = useMemo(() => groupCameraWallAlerts(alerts), [alerts]);
  const columns = wallColumnCount(layout);

  function handleGridKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) {
      return;
    }
    const buttons = Array.from(
      wallRef.current?.querySelectorAll<HTMLButtonElement>("[data-camera-tile]") ?? [],
    );
    const current = buttons.indexOf(event.currentTarget);
    const movement =
      event.key === "ArrowLeft"
        ? -1
        : event.key === "ArrowRight"
          ? 1
          : event.key === "ArrowUp"
            ? -columns
            : columns;
    const target = buttons[current + movement];
    if (!target) return;
    event.preventDefault();
    target.focus();
  }

  return (
    <section className="camera-wall-page" aria-labelledby="camera-wall-title">
      <header className="camera-wall-heading">
        <div>
          <div className="heading-kicker">
            <span className={dataMode === "live" ? "live-pill" : "live-pill demo-pill"}>
              <span />
              {dataMode === "live" ? "LIVE" : "DEMO"}
            </span>
            <span>Situational awareness · Operator+</span>
          </div>
          <h1 id="camera-wall-title">Camera Wall</h1>
          <p>Scan camera health and alert activity without losing queue context.</p>
        </div>
        <div className="camera-wall-summary" aria-label="Camera wall summary">
          <span><strong>{tiles.length || "—"}</strong><small>Cameras</small></span>
          <span><strong>{tiles.filter(({ status }) => status === "offline").length || "—"}</strong><small>Offline</small></span>
          <span><strong>{tiles.filter(({ alert }) => alert).length || "—"}</strong><small>Active events</small></span>
        </div>
      </header>

      <div className={`wall-boundary-banner ${dataMode === "live" ? "wall-boundary-live" : ""}`} role="status">
        <span className="wall-boundary-icon"><Icon name="shield" size={17} /></span>
        <span>
          <strong>{dataMode === "demo" ? "Privacy-safe demonstration" : "Streaming boundary not connected"}</strong>
          <small>
            {dataMode === "demo"
              ? "Camera metadata and scene graphics are synthetic. No footage, stream credentials, or customer data are present."
              : "Camera inventory and audited WebRTC session APIs are required before raw pixels can render."}
          </small>
        </span>
      </div>

      <div className="camera-wall-toolbar" aria-label="Camera Wall controls">
        <label className="wall-search">
          <Icon name="search" size={16} />
          <span className="sr-only">Search cameras</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search camera, group, zone, or event"
          />
          <kbd>⌘ K</kbd>
        </label>
        <label className="wall-filter">
          <span>Status</span>
          <select value={filter} onChange={(event) => setFilter(event.target.value as CameraWallFilter)}>
            <option value="all">All cameras</option>
            <option value="active">Has active event</option>
            <option value="online">Online</option>
            <option value="offline">Offline</option>
            <option value="degraded">Degraded</option>
            <option value="masked">Masked</option>
          </select>
        </label>
        <div className="wall-layout-control">
          <span>Layout</span>
          <div role="group" aria-label="Camera grid layout">
            {layouts.map((value) => (
              <button
                key={value}
                type="button"
                className={layout === value ? "selected" : ""}
                aria-pressed={layout === value}
                onClick={() => {
                  setLayout(value);
                  setSpotlightId(null);
                }}
              >
                {value}
              </button>
            ))}
          </div>
        </div>
        <button
          className={showAlerts ? "wall-alert-toggle selected" : "wall-alert-toggle"}
          type="button"
          aria-pressed={showAlerts}
          onClick={() => setShowAlerts((value) => !value)}
        >
          <Icon name="bell" size={15} />
          Alerts
          <span>{activeAlerts.length}</span>
        </button>
      </div>

      {dataMode === "live" ? (
        <div className="wall-empty-state">
          <span className="wall-empty-orbit"><Icon name="camera" size={30} /></span>
          <span className="dashboard-overline">Awaiting camera services</span>
          <h2>Live streams are intentionally unavailable</h2>
          <p>
            This screen will request tenant-scoped, short-lived viewing sessions only after the camera registry and playback service are implemented.
          </p>
          <button type="button" onClick={() => onOpenAlertCenter()}>
            Review metadata-only alerts <Icon name="arrow" size={14} />
          </button>
        </div>
      ) : (
        <div className={`camera-wall-content ${showAlerts ? "with-alert-rail" : ""}`}>
          <div className="camera-wall-main">
            {spotlight ? (
              <article className={`wall-spotlight ${spotlight.alert ? severityClass(spotlight.alert.severity) : ""}`}>
                <div className="spotlight-visual"><CameraVisual tile={spotlight} /></div>
                <div className="spotlight-details">
                  <button className="spotlight-close" type="button" onClick={() => setSpotlightId(null)} aria-label="Close spotlight">
                    <Icon name="close" size={16} />
                  </button>
                  <span className={`camera-status camera-status-${spotlight.status}`}><i />{statusLabels[spotlight.status]}</span>
                  <span className="dashboard-overline">{spotlight.id} · {spotlight.group}</span>
                  <h2>{spotlight.name}</h2>
                  <p>{spotlight.location}</p>
                  {spotlight.alert ? (
                    <div className="spotlight-event">
                      <span>{spotlight.alert.severity}</span>
                      <strong>{spotlight.alert.title}</strong>
                      <small>{spotlight.alert.type} · {Math.round(spotlight.alert.confidence * 100)}% confidence</small>
                      <button type="button" onClick={() => onOpenAlertCenter(spotlight.alert?.id)}>Open alert record <Icon name="arrow" size={13} /></button>
                    </div>
                  ) : (
                    <div className="spotlight-event spotlight-event-clear"><Icon name="check" /><strong>No active alert</strong><small>Metadata-only demo state</small></div>
                  )}
                  <button className="unavailable-stream-action" type="button" onClick={() => onNotify("Audited live-view sessions are not connected in this local slice.")}>
                    <Icon name="play" size={14} /> Request live session
                  </button>
                </div>
              </article>
            ) : visibleTiles.length ? (
              <div
                ref={wallRef}
                className={`camera-grid camera-grid-${columns}`}
                aria-label={`${visibleTiles.length} synthetic camera tiles for ${site}`}
              >
                {visibleTiles.map((tile) => (
                  <article key={tile.id} className={`camera-tile ${tile.alert ? severityClass(tile.alert.severity) : ""}`}>
                    <button
                      className="camera-tile-surface"
                      type="button"
                      data-camera-tile
                      onClick={() => setSpotlightId(tile.id)}
                      onKeyDown={handleGridKeyDown}
                      aria-label={`${tile.id}, ${tile.name}, ${statusLabels[tile.status]}${tile.alert ? `, active ${tile.alert.severity} alert ${tile.alert.title}` : ""}. Synthetic preview; no footage.`}
                    >
                      <CameraVisual tile={tile} />
                      <span className={`camera-status camera-status-${tile.status}`}><i />{statusLabels[tile.status]}</span>
                      {tile.alert && <span className="camera-event-chip"><b>{tile.alert.severity}</b>{tile.alert.type}</span>}
                      <span className="spotlight-hint"><Icon name="maximize" size={13} /> Spotlight</span>
                    </button>
                    <footer className="camera-tile-footer">
                      <span><strong>{tile.id} · {tile.name}</strong><small>{tile.location}</small></span>
                      <ActivityBars values={tile.activity} />
                    </footer>
                  </article>
                ))}
              </div>
            ) : (
              <div className="wall-filter-empty">
                <Icon name="search" size={24} />
                <h2>No cameras match this view</h2>
                <p>Clear the search or select another status filter.</p>
                <button type="button" onClick={() => { setQuery(""); setFilter("all"); }}>Clear filters</button>
              </div>
            )}

            <div className="camera-wall-legend" aria-label="Camera status legend">
              {(["online", "recording", "offline", "degraded", "masked"] as const).map((status) => (
                <span key={status} className={`camera-status camera-status-${status}`}><i />{statusLabels[status]}</span>
              ))}
              <small>Arrow keys move between tiles · Enter opens spotlight</small>
            </div>
          </div>

          {showAlerts && (
            <aside className="wall-alert-rail" aria-label="Active alert rail">
              <header>
                <div><span className="dashboard-overline">Realtime queue</span><h2>Active alerts</h2></div>
                <span>{activeAlerts.length}</span>
              </header>
              <div className="wall-alert-list">
                {activeAlerts.length ? activeAlerts.slice(0, 6).map((alert) => (
                  <button key={alert.id} type="button" onClick={() => onOpenAlertCenter(alert.id)}>
                    <span className={`wall-rail-severity ${severityClass(alert.severity)}`}>{alert.severity.slice(0, 1)}</span>
                    <span><strong>{alert.title}</strong><small>{alert.camera} · {alert.zone}</small></span>
                    <Icon name="chevron" size={13} />
                  </button>
                )) : <div className="wall-rail-empty"><Icon name="check" /><strong>Queue clear</strong><small>No active alerts.</small></div>}
              </div>
              <button className="wall-rail-footer" type="button" onClick={() => onOpenAlertCenter()}>
                Open Alert Center <Icon name="arrow" size={13} />
              </button>
            </aside>
          )}
        </div>
      )}
    </section>
  );
}
