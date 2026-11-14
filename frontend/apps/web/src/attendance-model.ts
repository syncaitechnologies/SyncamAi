import type { AttendanceRecord, StaffMember } from "./syncam-api";

export type AttendanceRow = {
  staffId: string;
  name: string;
  employeeCode: string;
  firstSeen: string | null;
  lastSeen: string | null;
  sightings: number;
};

/** One row per staff member for the chosen day: first and last sighting. */
export function summarizeAttendance(staff: StaffMember[], records: AttendanceRecord[]): AttendanceRow[] {
  const rows = new Map<string, AttendanceRow>();
  for (const member of staff) {
    rows.set(member.id, {
      staffId: member.id,
      name: member.display_name,
      employeeCode: member.employee_code ?? "",
      firstSeen: null,
      lastSeen: null,
      sightings: 0,
    });
  }
  for (const record of [...records].sort((left, right) => Date.parse(left.occurred_at) - Date.parse(right.occurred_at))) {
    const row = rows.get(record.staff_id) ?? {
      staffId: record.staff_id,
      name: record.staff_name ?? "Former staff member",
      employeeCode: "",
      firstSeen: null,
      lastSeen: null,
      sightings: 0,
    };
    row.firstSeen ??= record.occurred_at;
    row.lastSeen = record.occurred_at;
    row.sightings += 1;
    rows.set(record.staff_id, row);
  }
  return [...rows.values()].sort((left, right) =>
    Number(right.firstSeen !== null) - Number(left.firstSeen !== null) || left.name.localeCompare(right.name),
  );
}

function csvCell(value: string) {
  return /[",\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

export function attendanceCsv(rows: AttendanceRow[], day: string, format: (iso: string) => string): string {
  const lines = [["Date", "Name", "Employee code", "First seen", "Last seen", "Sightings"].join(",")];
  for (const row of rows) {
    lines.push([
      day,
      csvCell(row.name),
      csvCell(row.employeeCode),
      row.firstSeen ? format(row.firstSeen) : "Absent",
      row.lastSeen ? format(row.lastSeen) : "",
      String(row.sightings),
    ].join(","));
  }
  return `${lines.join("\n")}\n`;
}

/** Local-day bounds for a YYYY-MM-DD date string. */
export function dayBounds(day: string): { from: Date; to: Date } {
  const [year, month, date] = day.split("-").map(Number);
  const from = new Date(year!, month! - 1, date!, 0, 0, 0, 0);
  const to = new Date(year!, month! - 1, date! + 1, 0, 0, 0, 0);
  return { from, to };
}
