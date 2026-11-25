import type { StaffMember } from "./syncam-api";

/** One staff member on one site-local day, from GET /v1/attendance/daily. */
export type DayRecord = {
  date: string; // YYYY-MM-DD in the site's time zone
  staff_id: string;
  staff_name: string;
  employee_code?: string;
  first_seen: string;
  last_seen: string;
  sightings: number;
  camera_id: string;
};

/** A row of the attendance table: present (with times) or absent. */
export type DailyRow = {
  date: string;
  staffId: string;
  name: string;
  employeeCode: string;
  firstSeen: string | null;
  lastSeen: string | null;
  sightings: number;
  cameraId: string | null;
};

export type StaffTotal = { staffId: string; name: string; employeeCode: string; daysPresent: number };

/** The longest range the server summarises at once. */
export const MAX_RANGE_DAYS = 92;

function parseDay(day: string) {
  const [year, month, date] = day.split("-").map(Number);
  return new Date(Date.UTC(year!, month! - 1, date!));
}

function formatDay(date: Date) {
  return date.toISOString().slice(0, 10);
}

/** Every day from `from` to `to`, both included, newest first. */
export function rangeDays(from: string, to: string): string[] {
  const days: string[] = [];
  const start = parseDay(from);
  for (let day = parseDay(to); day >= start && days.length <= MAX_RANGE_DAYS; day = new Date(day.getTime() - 86_400_000)) {
    days.push(formatDay(day));
  }
  return days;
}

/** Why a range can't be shown, or null when it's fine. */
export function rangeProblem(from: string, to: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from) || !/^\d{4}-\d{2}-\d{2}$/.test(to)) return "Choose a start and an end date.";
  if (to < from) return "The end date must be on or after the start date.";
  if ((parseDay(to).getTime() - parseDay(from).getTime()) / 86_400_000 + 1 > MAX_RANGE_DAYS) {
    return `Choose at most ${MAX_RANGE_DAYS} days.`;
  }
  return null;
}

/** Quick ranges relative to `today` (YYYY-MM-DD). */
export function quickRanges(today: string): { label: string; from: string; to: string }[] {
  const end = parseDay(today);
  const back = (days: number) => formatDay(new Date(end.getTime() - days * 86_400_000));
  return [
    { label: "Today", from: today, to: today },
    { label: "Yesterday", from: back(1), to: back(1) },
    { label: "Last 7 days", from: back(6), to: today },
    { label: "This month", from: `${today.slice(0, 8)}01`, to: today },
  ];
}

/**
 * One row per staff member per day, newest day first; enrolled staff with no
 * sighting that day are listed as absent. Staff who were removed but have
 * sightings in the range still show on the days they were seen.
 */
export function dailyRows(staff: StaffMember[], records: DayRecord[], days: string[]): DailyRow[] {
  const seen = new Map(records.map((record) => [`${record.date}|${record.staff_id}`, record]));
  const rows: DailyRow[] = [];
  for (const day of days) {
    const listed = new Set<string>();
    const dayRows: DailyRow[] = [];
    for (const member of staff) {
      listed.add(member.id);
      const record = seen.get(`${day}|${member.id}`);
      dayRows.push({
        date: day, staffId: member.id, name: member.display_name, employeeCode: member.employee_code ?? "",
        firstSeen: record?.first_seen ?? null, lastSeen: record?.last_seen ?? null,
        sightings: record?.sightings ?? 0, cameraId: record?.camera_id ?? null,
      });
    }
    for (const record of records) {
      if (record.date !== day || listed.has(record.staff_id)) continue;
      dayRows.push({
        date: day, staffId: record.staff_id, name: record.staff_name || "Former staff member", employeeCode: record.employee_code ?? "",
        firstSeen: record.first_seen, lastSeen: record.last_seen, sightings: record.sightings, cameraId: record.camera_id,
      });
    }
    dayRows.sort((left, right) => Number(right.firstSeen !== null) - Number(left.firstSeen !== null) || left.name.localeCompare(right.name));
    rows.push(...dayRows);
  }
  return rows;
}

/** Days present per staff member over the range. */
export function staffTotals(rows: DailyRow[]): StaffTotal[] {
  const totals = new Map<string, StaffTotal>();
  for (const row of rows) {
    const total = totals.get(row.staffId) ?? { staffId: row.staffId, name: row.name, employeeCode: row.employeeCode, daysPresent: 0 };
    if (row.firstSeen) total.daysPresent += 1;
    totals.set(row.staffId, total);
  }
  return [...totals.values()].sort((left, right) => left.name.localeCompare(right.name));
}

export function csvCell(value: string) {
  return /[",\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

export function attendanceCsv(rows: DailyRow[], format: (iso: string) => string): string {
  const lines = [["Date", "Name", "Employee code", "First seen", "Last seen", "Sightings"].join(",")];
  for (const row of rows) {
    lines.push([
      row.date,
      csvCell(row.name),
      csvCell(row.employeeCode),
      row.firstSeen ? format(row.firstSeen) : "Absent",
      row.lastSeen ? format(row.lastSeen) : "",
      String(row.sightings),
    ].join(","));
  }
  return `${lines.join("\n")}\n`;
}

/** "Tuesday, November 24", or "November 1 – November 24" for a range. */
export function rangeLabel(from: string, to: string, locale?: string): string {
  const options: Intl.DateTimeFormatOptions = { day: "numeric", month: "long", timeZone: "UTC" };
  if (from === to) return parseDay(from).toLocaleDateString(locale, { ...options, weekday: "long" });
  return `${parseDay(from).toLocaleDateString(locale, options)} – ${parseDay(to).toLocaleDateString(locale, options)}`;
}
