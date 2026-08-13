import type { AlertItem, Severity } from "./alert-contracts";

export interface ModuleCount {
  label: string;
  count: number;
  percent: number;
}

export interface DashboardSummary {
  activeIncidents: number;
  unacknowledged: number;
  criticalOpen: number;
  averageConfidence: number | null;
  severityCounts: Record<Severity, number>;
  moduleCounts: ModuleCount[];
  recentAlerts: AlertItem[];
}

export type DashboardRange = "24h" | "7d" | "30d";

const inactiveStatuses = new Set(["resolved", "dismissed", "snoozed"]);
const severityOrder: Severity[] = ["Critical", "High", "Medium"];
const rangeHours: Record<DashboardRange, number> = {
  "24h": 24,
  "7d": 24 * 7,
  "30d": 24 * 30,
};

export function filterAlertsForRange(
  alerts: AlertItem[],
  range: DashboardRange,
  now = Date.now(),
) {
  const cutoff = now - rangeHours[range] * 60 * 60 * 1_000;
  return alerts.filter((alert) => new Date(alert.occurredAt).getTime() >= cutoff);
}

export function buildDashboardSummary(alerts: AlertItem[]): DashboardSummary {
  const activeAlerts = alerts.filter(
    (alert) => !alert.placeholder && !inactiveStatuses.has(alert.status),
  );
  const severityCounts: Record<Severity, number> = {
    Critical: 0,
    High: 0,
    Medium: 0,
  };
  const modules = new Map<string, number>();
  let confidenceTotal = 0;

  for (const alert of activeAlerts) {
    severityCounts[alert.severity] += 1;
    modules.set(alert.type, (modules.get(alert.type) ?? 0) + 1);
    confidenceTotal += alert.confidence;
  }

  const moduleCounts = [...modules.entries()]
    .map(([label, count]) => ({
      label,
      count,
      percent: activeAlerts.length
        ? Math.round((count / activeAlerts.length) * 100)
        : 0,
    }))
    .sort((left, right) => right.count - left.count || left.label.localeCompare(right.label));

  return {
    activeIncidents: activeAlerts.length,
    unacknowledged: activeAlerts.filter(
      (alert) => alert.status === "unacknowledged",
    ).length,
    criticalOpen: activeAlerts.filter(
      (alert) => alert.severity === "Critical",
    ).length,
    averageConfidence: activeAlerts.length
      ? confidenceTotal / activeAlerts.length
      : null,
    severityCounts,
    moduleCounts,
    recentAlerts: [...activeAlerts]
      .sort((left, right) => {
        const severityDifference =
          severityOrder.indexOf(left.severity) -
          severityOrder.indexOf(right.severity);
        return (
          severityDifference ||
          new Date(right.occurredAt).getTime() -
            new Date(left.occurredAt).getTime()
        );
      })
      .slice(0, 6),
  };
}
