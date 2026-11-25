// Insights page (ADR-020): pure rules for anonymous visitor and table counts.
import { rangeDays } from "./attendance-model.ts";

export type InsightsZone = {
  zone_id: string;
  name: string;
  kind: string;
  entries: number;
  exits: number;
  occupied_seconds: number;
  sessions: number;
  session_seconds: number;
  peak_people: number;
};

/** GET /v1/analytics/insights. Hours are 0–23 in the site's time zone. */
export type InsightsSummary = {
  days: { date: string; entries: number; exits: number }[];
  hours: { hour: number; entries: number }[];
  zones: InsightsZone[];
};

export type TableRow = {
  zoneId: string;
  name: string;
  sessions: number;
  averageMinutes: number | null;
  occupiedHours: number;
  sessionsPerDay: number;
  peakPeople: number;
};

const isTable = (zone: InsightsZone) => zone.kind === "table" || (zone.kind === "" && (zone.sessions > 0 || zone.occupied_seconds > 0));
const isDoor = (zone: InsightsZone) => zone.kind === "door_line" || (zone.kind === "" && (zone.entries > 0 || zone.exits > 0));

export function totalVisitors(summary: InsightsSummary) {
  return summary.days.reduce((total, day) => total + day.entries, 0);
}

/** Visitors for every day of the range, oldest first, zero where nothing was counted. */
export function dailySeries(summary: InsightsSummary, from: string, to: string) {
  const counts = new Map(summary.days.map((day) => [day.date, day.entries]));
  return rangeDays(from, to).reverse().map((date) => ({ date, entries: counts.get(date) ?? 0 }));
}

/** Visitors for every hour of the day, 0–23. */
export function hourSeries(summary: InsightsSummary) {
  const counts = new Map(summary.hours.map((hour) => [hour.hour, hour.entries]));
  return Array.from({ length: 24 }, (_, hour) => ({ hour, entries: counts.get(hour) ?? 0 }));
}

export function busiestHour(summary: InsightsSummary): { hour: number; entries: number } | null {
  const best = hourSeries(summary).reduce((top, hour) => (hour.entries > top.entries ? hour : top), { hour: -1, entries: 0 });
  return best.entries > 0 ? best : null;
}

/** "7 pm – 8 pm". */
export function hourLabel(hour: number) {
  const name = (value: number) => `${value % 12 === 0 ? 12 : value % 12} ${value % 24 < 12 ? "am" : "pm"}`;
  return `${name(hour)} – ${name(hour + 1)}`;
}

/** "1 h 12 min", "45 min" or "–". */
export function durationLabel(minutes: number | null) {
  if (minutes === null || !Number.isFinite(minutes)) return "–";
  const rounded = Math.round(minutes);
  if (rounded < 60) return `${rounded} min`;
  return `${Math.floor(rounded / 60)} h ${rounded % 60} min`;
}

export function tableRows(summary: InsightsSummary, dayCount: number): TableRow[] {
  return summary.zones.filter(isTable).map((zone) => ({
    zoneId: zone.zone_id,
    name: zone.name,
    sessions: zone.sessions,
    averageMinutes: zone.sessions > 0 ? zone.session_seconds / zone.sessions / 60 : null,
    occupiedHours: Math.round((zone.occupied_seconds / 3600) * 10) / 10,
    sessionsPerDay: dayCount > 0 ? Math.round((zone.sessions / dayCount) * 10) / 10 : 0,
    peakPeople: zone.peak_people,
  })).sort((left, right) => right.sessions - left.sessions || left.name.localeCompare(right.name));
}

/** Average dining time over every table, in minutes; null without sessions. */
export function averageDiningMinutes(summary: InsightsSummary): number | null {
  const tables = summary.zones.filter(isTable);
  const sessions = tables.reduce((total, zone) => total + zone.sessions, 0);
  return sessions ? tables.reduce((total, zone) => total + zone.session_seconds, 0) / sessions / 60 : null;
}

/** Seated groups per table per day, across all tables. */
export function turnover(summary: InsightsSummary, dayCount: number): number | null {
  const tables = summary.zones.filter(isTable);
  if (!tables.length || dayCount <= 0) return null;
  const sessions = tables.reduce((total, zone) => total + zone.sessions, 0);
  return Math.round((sessions / tables.length / dayCount) * 10) / 10;
}

export function doorNames(summary: InsightsSummary) {
  return summary.zones.filter(isDoor).map((zone) => zone.name);
}

/** Bar height in percent of the largest value (0 stays 0). */
export function barPercent(value: number, max: number) {
  return max > 0 ? Math.max(value > 0 ? 2 : 0, Math.round((value / max) * 100)) : 0;
}
