import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import type { AlertItem, Severity } from "./alert-contracts";
import {
  filterCameraWallTiles,
  groupCameraWallAlerts,
  wallColumnCount,
  type CameraTile,
  type CameraWallFilter,
  type CameraWallLayout,
} from "./camera-wall-model";
import type { ApiCamera } from "./camera-contracts";
import { Icon } from "./icon";
import { LiveCameraFeed } from "./LiveCameraFeed";
import {
  canStartLive,
  contactHref,
  isUnlimited,
  limitMessage,
  resetLabel,
  stillAgeLabel,
  usageLabel,
  type LiveViewUsage,
} from "./live-view-model";
import type { SyncamApi } from "./syncam-api";
import { useCameraInventory } from "./use-camera-inventory";
import { useCameraStills, type CameraStill } from "./use-camera-stills";

interface CameraWallProps {
  alerts: AlertItem[];
  dataMode: "demo" | "live";
  site: string;
  /** Present in live mode; enables audited near-live viewing. */
  api?: SyncamApi | null;
  onOpenAlertCenter: (alertId?: string) => void;
  onNotify: (message: string) => void;
  /** Shown when live view minutes run out. Defaults to VITE_SYNCAM_CONTACT_EMAIL. */
  contactEmail?: string;
  /** Synthetic cameras for browser fixtures; skips the camera list request. */
  fixtureCameras?: ApiCamera[];
}

const USAGE_REFRESH_MS = 30_000;

const layouts: CameraWallLayout[] = [1, 4, 9, 16, 25];
const statusLabels: Record<CameraTile["status"], string> = {
  online: "Online",
  recording: "Recording",
  offline: "Offline",
  pending: "Pending",
  degraded: "Degraded",
  masked: "Masked",
};

function severityClass(severity: Severity) {
  return `wall-event-${severity.toLowerCase()}`;
}

/**
 * A tile's picture: the camera's reference still in live mode, or the
 * synthetic scene graphic in demo mode and while no still exists.
 */
function CameraVisual({ tile, still, now }: { tile: CameraTile; still?: CameraStill | null; now: number }) {
  if (still && tile.status !== "masked") {
    return (
      <div className="camera-visual camera-still" aria-hidden="true">
        <img src={still.url} alt="" />
        <span className="camera-still-age">{stillAgeLabel(still.capturedAt, now)}</span>
      </div>
    );
  }
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
        {tile.status === "masked"
          ? "Masked zone"
          : tile.synthetic
            ? "No footage · demo visual"
            : still === null
              ? "No still picture yet"
              : "Loading still picture"}
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
  api = null,
  onOpenAlertCenter,
  onNotify,
  contactEmail = String(import.meta.env.VITE_SYNCAM_CONTACT_EMAIL ?? ""),
  fixtureCameras,
}: CameraWallProps) {
  const canWatch = dataMode === "live" && api !== null;
  // Only the spotlight camera can play live, so at most one stream runs.
  const [live, setLive] = useState(false);
  const [usage, setUsage] = useState<LiveViewUsage | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const refreshUsage = useCallback(() => {
    if (!canWatch || !api) return;
    api.liveViewUsage().then(setUsage, () => undefined);
  }, [api, canWatch]);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<CameraWallFilter>("all");
  const [layout, setLayout] = useState<CameraWallLayout>(9);
  const [showAlerts, setShowAlerts] = useState(true);
  const [spotlightId, setSpotlightId] = useState<string | null>(null);
  const wallRef = useRef<HTMLDivElement>(null);
  const inventory = useCameraInventory(alerts, dataMode, fixtureCameras);
  const tiles = inventory.tiles;
  const filteredTiles = useMemo(
    () => filterCameraWallTiles(tiles, query, filter),
    [filter, query, tiles],
  );
  const visibleTiles = filteredTiles.slice(0, layout);
  const spotlight = tiles.find(({ id }) => id === spotlightId) ?? null;
  const liveId = spotlight && live ? spotlight.id : null;
  const stillIds = useMemo(() => {
    if (!canWatch) return [];
    const ids = visibleTiles.map(({ id }) => id);
    if (spotlight && !ids.includes(spotlight.id)) ids.push(spotlight.id);
    return ids;
  }, [canWatch, spotlight, visibleTiles]);
  const stills = useCameraStills(canWatch ? api : null, stillIds);
  const allowed = canStartLive(usage);
  const contact = contactHref(contactEmail, "More SyncCam live view minutes");

  useEffect(() => {
    refreshUsage();
  }, [refreshUsage]);
  // Keep the minutes-left figure current while a camera plays.
  useEffect(() => {
    if (!liveId) return;
    const timer = window.setInterval(refreshUsage, USAGE_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [liveId, refreshUsage]);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  const openSpotlight = (id: string, watch: boolean) => {
    setSpotlightId(id);
    setLive(watch);
  };
  const closeSpotlight = () => {
    setSpotlightId(null);
    setLive(false);
  };
  const blockedReason = allowed ? "" : limitMessage(usage);
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
          <strong>{dataMode === "demo" ? "Privacy-safe demonstration" : "Still pictures, live on request"}</strong>
          <small>
            {dataMode === "demo"
              ? "Camera metadata and scene graphics are synthetic. No footage, stream credentials, or customer data are present."
              : "Tiles show a still picture from each camera, updated about every 5 minutes. Watch live plays one camera at a time at about 2 frames a second and pauses after 5 minutes; frames are never stored, every session is logged."}
          </small>
        </span>
        {canWatch && usage && (
          <span className={`wall-usage ${allowed ? "" : "wall-usage-out"}`} aria-label="Live view minutes this month">
            <strong>{usageLabel(usage)}</strong>
            <small>
              {!allowed && usage.limit_minutes !== 0
                ? <>Used up · {resetLabel(usage)}{contact && <> · <a href={contact}>Contact {contactEmail.trim()}</a></>}</>
                : !allowed
                  ? contact ? <a href={contact}>Contact {contactEmail.trim()} to turn it on</a> : "Ask your SyncCam contact to turn it on"
                  : isUnlimited(usage) ? "This workspace has no monthly cap" : resetLabel(usage)}
            </small>
          </span>
        )}
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
            <option value="pending">Pending activation</option>
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
                  closeSpotlight();
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

      {dataMode === "live" && (!inventory.loaded || inventory.error || tiles.length === 0) ? (
        <div className="wall-empty-state">
          <span className="wall-empty-orbit"><Icon name="camera" size={30} /></span>
          <span className="dashboard-overline">
            {!inventory.loaded ? "Loading tenant inventory" : inventory.error ? "Inventory unavailable" : "Camera registry empty"}
          </span>
          <h2>
            {!inventory.loaded ? "Loading authorized cameras" : inventory.error ? "Camera metadata could not be loaded" : "No cameras are registered for this site"}
          </h2>
          <p>
            {inventory.error || "Register a camera for this site and connect a site box to watch it live here."}
          </p>
          <button type="button" onClick={() => onOpenAlertCenter()}>
            Review metadata-only alerts <Icon name="arrow" size={14} />
          </button>
        </div>
      ) : (
        <div className={`camera-wall-content ${showAlerts ? "with-alert-rail" : ""}`}>
          <div className="camera-wall-main">
            {spotlight && (
              <article className={`wall-spotlight ${spotlight.alert ? severityClass(spotlight.alert.severity) : ""}`}>
                <div className="spotlight-visual">
                  {canWatch && api && liveId === spotlight.id ? (
                    <LiveCameraFeed
                      key={spotlight.id}
                      api={api}
                      cameraId={spotlight.id}
                      cameraName={spotlight.name}
                      usage={usage}
                      contactEmail={contactEmail}
                      onSessionChange={refreshUsage}
                    />
                  ) : (
                    <CameraVisual tile={spotlight} still={stills[spotlight.id]} now={now} />
                  )}
                </div>
                <div className="spotlight-details">
                  <button className="spotlight-close" type="button" onClick={closeSpotlight} aria-label="Close spotlight">
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
                    <div className="spotlight-event spotlight-event-clear"><Icon name="check" /><strong>No active alert</strong><small>{dataMode === "demo" ? "Metadata-only demo state" : "Nothing open for this camera"}</small></div>
                  )}
                  {canWatch ? (
                    <>
                      <button
                        className="wall-live-action"
                        type="button"
                        aria-pressed={liveId === spotlight.id}
                        disabled={liveId !== spotlight.id && !allowed}
                        title={liveId !== spotlight.id ? blockedReason || undefined : undefined}
                        onClick={() => setLive((value) => !value)}
                      >
                        <Icon name="play" size={14} /> {liveId === spotlight.id ? "Stop live view" : "Watch live"}
                      </button>
                      {liveId !== spotlight.id && blockedReason && <small className="wall-live-blocked">{blockedReason}</small>}
                    </>
                  ) : (
                    <button className="unavailable-stream-action" type="button" onClick={() => onNotify("Live view needs live mode with a connected site box.")}>
                      <Icon name="play" size={14} /> Request live session
                    </button>
                  )}
                </div>
              </article>
            )}
            {visibleTiles.length ? (
              <div
                ref={wallRef}
                className={`camera-grid camera-grid-${columns}`}
                aria-label={`${visibleTiles.length} ${dataMode === "demo" ? "synthetic" : "still picture"} camera tiles for ${site}`}
              >
                {visibleTiles.map((tile) => (
                  <article
                    key={tile.id}
                    className={`camera-tile ${tile.alert ? severityClass(tile.alert.severity) : ""} ${tile.id === spotlightId ? "camera-tile-selected" : ""}`}
                  >
                    <button
                      className="camera-tile-surface"
                      type="button"
                      data-camera-tile
                      aria-current={tile.id === spotlightId ? "true" : undefined}
                      onClick={() => openSpotlight(tile.id, false)}
                      onKeyDown={handleGridKeyDown}
                      aria-label={`${tile.id}, ${tile.name}, ${statusLabels[tile.status]}${tile.alert ? `, active ${tile.alert.severity} alert ${tile.alert.title}` : ""}. ${tile.synthetic ? "Synthetic preview; no footage" : stills[tile.id] ? "Still picture" : "No picture yet"}${tile.id === liveId ? "; playing live in the spotlight" : ""}.`}
                    >
                      <CameraVisual tile={tile} still={stills[tile.id]} now={now} />
                      <span className={`camera-status camera-status-${tile.status}`}><i />{statusLabels[tile.status]}</span>
                      {tile.id === liveId && <span className="camera-live-marker"><i /> Live in spotlight</span>}
                      {tile.alert && <span className="camera-event-chip"><b>{tile.alert.severity}</b>{tile.alert.type}</span>}
                      <span className="spotlight-hint"><Icon name="maximize" size={13} /> Spotlight</span>
                    </button>
                    <footer className="camera-tile-footer">
                      <span><strong>{tile.synthetic ? `${tile.id} · ` : ""}{tile.name}</strong><small>{tile.location}</small></span>
                      {canWatch ? (
                        <button
                          className="wall-live-action"
                          type="button"
                          aria-pressed={tile.id === liveId}
                          disabled={tile.id !== liveId && !allowed}
                          title={tile.id !== liveId ? blockedReason || undefined : undefined}
                          onClick={() => (tile.id === liveId ? setLive(false) : openSpotlight(tile.id, true))}
                        >
                          <Icon name="play" size={13} /> {tile.id === liveId ? "Stop" : "Watch live"}
                        </button>
                      ) : (
                        <ActivityBars values={tile.activity} />
                      )}
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
              {(["online", "recording", "offline", "pending", "degraded", "masked"] as const).map((status) => (
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
