import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  AlertApiClient,
  AlertRealtimeClient,
  readAlertRuntimeConfig,
  validateLiveConfig,
  type ConnectionState,
} from "./alert-client";
import {
  initialAlertQueue,
  sortAlerts,
  toAlertItem,
  upsertAlert,
  type AlertItem,
} from "./alert-contracts";

export function useAlertFeed(seedAlerts: AlertItem[]) {
  const config = useMemo(readAlertRuntimeConfig, []);
  const [alerts, setAlerts] = useState(() =>
    initialAlertQueue(config.mode, seedAlerts),
  );
  const [queueLoaded, setQueueLoaded] = useState(config.mode === "demo");
  const [connection, setConnection] = useState<ConnectionState>(
    config.mode === "demo" ? "demo" : "connecting",
  );
  const [feedError, setFeedError] = useState("");
  const [newAlertCount, setNewAlertCount] = useState(0);
  const apiRef = useRef<AlertApiClient>();

  const refresh = useCallback(
    async (api: AlertApiClient, signal?: AbortSignal) => {
      try {
        const incoming = await api.listAlerts(signal);
        setAlerts(sortAlerts(incoming.map(toAlertItem)));
      } finally {
        setQueueLoaded(true);
      }
    },
    [],
  );

  useEffect(() => {
    if (config.mode === "demo") {
      const timer = window.setInterval(() => {
        const base = seedAlerts[1];
        if (!base) return;
        const incoming: AlertItem = {
          ...base,
          id: `alert-${Date.now()}`,
          title: "Door held open",
          summary:
            "The east personnel door has remained open beyond the configured dwell time.",
          severity: "Medium",
          status: "unacknowledged",
          type: "Door dwell",
          zone: "East personnel door",
          camera: "CAM-02 · East gate",
          occurredAt: new Date().toISOString(),
          confidence: 0.86,
          eventId: `EVT-LIVE-${Date.now().toString().slice(-4)}`,
          color: "#62c9a4",
          evidence: "Snapshot ready",
        };
        setAlerts((current) => sortAlerts([incoming, ...current].slice(0, 12)));
        setNewAlertCount((count) => count + 1);
      }, 18_000);
      return () => window.clearInterval(timer);
    }

    const abort = new AbortController();
    let realtime: AlertRealtimeClient | undefined;
    try {
      validateLiveConfig(config);
      const api = new AlertApiClient(config);
      apiRef.current = api;
      void refresh(api, abort.signal).catch((error: unknown) =>
        setFeedError(
          error instanceof Error
            ? error.message
            : "Alert queue could not be loaded.",
        ),
      );
      realtime = new AlertRealtimeClient(api, config, {
        onState: setConnection,
        onSnapshot: (incoming) => {
          setAlerts(sortAlerts(incoming.map(toAlertItem)));
          setQueueLoaded(true);
        },
        onAlert: (incoming, isNew) => {
          setAlerts((current) => upsertAlert(current, incoming));
          if (isNew) setNewAlertCount((count) => count + 1);
        },
        onGap: () =>
          void refresh(api).catch((error: unknown) =>
            setFeedError(
              error instanceof Error
                ? error.message
                : "Alert queue resync failed.",
            ),
          ),
        onError: setFeedError,
      });
      realtime.start();
    } catch (error) {
      setConnection("error");
      setFeedError(
        error instanceof Error
          ? error.message
          : "Live alert mode could not start.",
      );
    }
    return () => {
      abort.abort();
      realtime?.stop();
    };
  }, [config, refresh, seedAlerts]);

  const acknowledge = useCallback(
    async (alertId: string) => {
      if (config.mode === "demo") {
        setAlerts((current) =>
          current.map((alert) =>
            alert.id === alertId && alert.status === "unacknowledged"
              ? { ...alert, status: "acknowledged" }
              : alert,
          ),
        );
        return;
      }
      const api = apiRef.current;
      if (!api) throw new Error(feedError || "Alert API is not connected.");
      const acknowledged = await api.acknowledgeAlert(alertId);
      setAlerts((current) => upsertAlert(current, acknowledged));
    },
    [config.mode, feedError],
  );

  return {
    alerts,
    setAlerts,
    acknowledge,
    connection,
    feedError,
    newAlertCount,
    queueLoaded,
    dataMode: config.mode,
  };
}
