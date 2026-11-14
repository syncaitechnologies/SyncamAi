import { useCallback, useEffect, useRef, useState } from "react";

import type { AlertItem } from "./alert-contracts";

const preferenceKey = "syncam.desktop_alerts";

function readPreference() {
  try {
    return localStorage.getItem(preferenceKey) === "on";
  } catch {
    return false;
  }
}

function writePreference(on: boolean) {
  try {
    localStorage.setItem(preferenceKey, on ? "on" : "off");
  } catch {
    // Private windows may block storage; the toggle then lasts for this page.
  }
}

/** A short two-tone chime; louder and longer for critical alerts. */
function chime(critical: boolean) {
  try {
    const context = new AudioContext();
    const tones = critical ? [880, 660, 880, 660] : [880, 660];
    tones.forEach((frequency, index) => {
      const oscillator = context.createOscillator();
      const gain = context.createGain();
      oscillator.frequency.value = frequency;
      oscillator.connect(gain);
      gain.connect(context.destination);
      const start = context.currentTime + index * 0.22;
      gain.gain.setValueAtTime(0.0001, start);
      gain.gain.exponentialRampToValueAtTime(0.25, start + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, start + 0.2);
      oscillator.start(start);
      oscillator.stop(start + 0.21);
    });
    window.setTimeout(() => void context.close(), tones.length * 250 + 100);
  } catch {
    // Audio may be blocked until the user interacts with the page.
  }
}

/** Sound and desktop notifications for new live alerts while the dashboard is open. */
export function useLiveAlertNotifications(live: boolean, alerts: AlertItem[], newAlertCount: number) {
  const supported = typeof window !== "undefined" && "Notification" in window;
  const [permission, setPermission] = useState<NotificationPermission | "unsupported">(
    supported ? Notification.permission : "unsupported",
  );
  const [enabled, setEnabled] = useState(() => live && readPreference());
  const previous = useRef(newAlertCount);

  const enable = useCallback(async () => {
    if (enabled) {
      setEnabled(false);
      writePreference(false);
      return false;
    }
    let granted = permission === "granted";
    if (supported && permission === "default") {
      const result = await Notification.requestPermission();
      setPermission(result);
      granted = result === "granted";
    }
    // Sound works even when desktop notifications are denied.
    setEnabled(true);
    writePreference(true);
    chime(false);
    return granted;
  }, [enabled, permission, supported]);

  useEffect(() => {
    const arrived = newAlertCount - previous.current;
    previous.current = newAlertCount;
    if (!live || !enabled || arrived <= 0) return;
    const newest = [...alerts]
      .filter((alert) => alert.status === "unacknowledged")
      .sort((left, right) => Date.parse(right.occurredAt) - Date.parse(left.occurredAt))[0];
    if (!newest) return;
    chime(newest.severity === "Critical");
    if (supported && permission === "granted") {
      try {
        const notification = new Notification(`SyncCam: ${newest.title}`, {
          body: `${newest.zone} · ${newest.camera}`,
          tag: newest.id,
          requireInteraction: newest.severity === "Critical",
        });
        notification.onclick = () => {
          window.focus();
          notification.close();
        };
      } catch {
        // Some browsers only allow notifications from a service worker.
      }
    }
  }, [alerts, enabled, live, newAlertCount, permission, supported]);

  return { permission, enabled, enable };
}
