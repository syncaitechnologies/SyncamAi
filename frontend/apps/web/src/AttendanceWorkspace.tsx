import { useEffect, useMemo, useState, type FormEvent } from "react";

import {
  attendanceCsv,
  dailyRows,
  quickRanges,
  rangeDays,
  rangeLabel,
  rangeProblem,
  staffTotals,
  type DayRecord,
} from "./attendance-model";
import { Icon } from "./icon";
import type { StaffMember } from "./syncam-api";
import type { WorkspaceData } from "./use-workspace";
import { canManage, readWorkspace } from "./workspace";

/** Today (YYYY-MM-DD) in the site's time zone, which attendance days use. */
function todayIn(timeZone: string | undefined) {
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  } catch {
    return new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  }
}

function timeFormatFor(timeZone: string | undefined) {
  try {
    return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", timeZone });
  } catch {
    return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
  }
}

export function AttendanceWorkspace({ workspace, onNotify }: { workspace: WorkspaceData; onNotify: (message: string) => void }) {
  const { api, site, names } = workspace;
  const manage = canManage(readWorkspace());
  const today = todayIn(site?.timezone);
  const [from, setFrom] = useState(today);
  const [to, setTo] = useState(today);
  const [staff, setStaff] = useState<StaffMember[]>([]);
  const [records, setRecords] = useState<DayRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [consent, setConsent] = useState(false);
  const [enrolled, setEnrolled] = useState<StaffMember | null>(null);
  const [version, setVersion] = useState(0);
  const problem = rangeProblem(from, to);

  useEffect(() => {
    if (!api?.siteId || problem) return;
    const abort = new AbortController();
    setError("");
    setLoading(true);
    Promise.all([api.listStaff(api.siteId, abort.signal), api.dailyAttendance(api.siteId, from, to, abort.signal)])
      .then(([listedStaff, listedRecords]) => {
        setStaff(listedStaff);
        setRecords(listedRecords);
      })
      .catch((caught: unknown) => {
        if (!abort.signal.aborted) setError(caught instanceof Error ? caught.message : "Attendance could not be loaded.");
      })
      .finally(() => {
        if (!abort.signal.aborted) setLoading(false);
      });
    return () => abort.abort();
  }, [api, from, to, problem, version]);

  const activeStaff = useMemo(() => staff.filter((member) => member.active), [staff]);
  const days = useMemo(() => (problem ? [] : rangeDays(from, to)), [from, to, problem]);
  const rows = useMemo(() => dailyRows(activeStaff, records, days), [activeStaff, records, days]);
  const totals = useMemo(() => staffTotals(rows), [rows]);
  if (!api) return null;
  const timeFormat = timeFormatFor(site?.timezone);
  const singleDay = days.length === 1;
  const presentToday = rows.filter((row) => row.firstSeen).length;

  function chooseRange(nextFrom: string, nextTo: string) {
    setFrom(nextFrom);
    setTo(nextTo);
  }

  function addStaff(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const values = new FormData(form);
    if (!site || !consent) return;
    setBusy(true);
    api!.createStaff(site.id, String(values.get("name") ?? "").trim(), String(values.get("code") ?? "").trim())
      .then((created) => {
        form.reset();
        setConsent(false);
        setEnrolled(created);
        setVersion((value) => value + 1);
      })
      .catch((caught: unknown) => onNotify(caught instanceof Error ? caught.message : "Staff member could not be added"))
      .finally(() => setBusy(false));
  }

  function setActive(member: StaffMember, active: boolean) {
    api!.setStaffActive(member.id, active)
      .then(() => {
        onNotify(active ? `${member.display_name} re-activated` : `${member.display_name} removed from attendance. Also run "forget" on the site box to delete their face data.`);
        setVersion((value) => value + 1);
      })
      .catch((caught: unknown) => onNotify(caught instanceof Error ? caught.message : "Update failed"));
  }

  function exportCsv() {
    const blob = new Blob([attendanceCsv(rows, (iso) => timeFormat.format(new Date(iso)))], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    const period = from === to ? from : `${from}_to_${to}`;
    link.download = `attendance-${site?.name ?? "site"}-${period}.csv`.replaceAll(/[^\w.-]+/g, "_");
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker"><span className="live-pill"><span />LIVE</span><span>Staff attendance</span></div>
          <h1>Attendance</h1>
          <p>Enrolled staff are marked present when an entrance camera recognises them. Face data stays on the site box.</p>
        </div>
        <div className="heading-actions attendance-range">
          <label className="site-filter-label" htmlFor="attendance-from">From</label>
          <input id="attendance-from" className="live-date" type="date" value={from} max={to || today} onChange={(event) => setFrom(event.target.value)} />
          <label className="site-filter-label" htmlFor="attendance-to">To</label>
          <input id="attendance-to" className="live-date" type="date" value={to} min={from} max={today} onChange={(event) => setTo(event.target.value)} />
          <button type="button" className="live-primary" onClick={exportCsv} disabled={!rows.length || Boolean(problem)}><Icon name="file" size={14} />Export CSV</button>
        </div>
      </section>
      <div className="attendance-quick" role="group" aria-label="Quick date ranges">
        {quickRanges(today).map((range) => (
          <button
            key={range.label}
            type="button"
            className={range.from === from && range.to === to ? "live-link attendance-quick-on" : "live-link"}
            aria-pressed={range.from === from && range.to === to}
            onClick={() => chooseRange(range.from, range.to)}
          >
            {range.label}
          </button>
        ))}
      </div>
      {problem && <p className="live-banner">{problem}</p>}
      {error && <p className="live-banner">{error}</p>}
      <div className="live-zone-layout">
        <section className="panel live-card">
          <header>
            <div>
              <h2>
                {singleDay
                  ? `${presentToday} of ${activeStaff.length} present`
                  : `${activeStaff.length} staff · ${days.length} days`}
              </h2>
              <p>{problem ? "" : rangeLabel(from, to)}{loading ? " · loading…" : ""}{site?.timezone ? ` · times in ${site.timezone}` : ""}</p>
            </div>
          </header>
          {!singleDay && totals.length > 0 && (
            <ul className="attendance-totals">
              {totals.map((total) => (
                <li key={total.staffId}>
                  <strong>{total.name}</strong>
                  <span>present {total.daysPresent} of {days.length} days</span>
                </li>
              ))}
            </ul>
          )}
          <div className="attendance-table-wrap">
            <table className="live-table">
              <thead><tr>{!singleDay && <th>Date</th>}<th>Name</th><th>Code</th><th>First seen</th><th>Last seen</th><th>Camera</th></tr></thead>
              <tbody>
                {rows.length === 0 && <tr><td colSpan={6} className="live-muted">{activeStaff.length === 0 ? "No staff enrolled yet." : "No attendance in this period."}</td></tr>}
                {rows.map((row) => (
                  <tr key={`${row.date}|${row.staffId}`} className={row.firstSeen ? "" : "absent"}>
                    {!singleDay && <td>{row.date}</td>}
                    <td>{row.name}</td>
                    <td>{row.employeeCode || "—"}</td>
                    <td>{row.firstSeen ? timeFormat.format(new Date(row.firstSeen)) : "Absent"}</td>
                    <td>{row.lastSeen ? timeFormat.format(new Date(row.lastSeen)) : "—"}</td>
                    <td>{row.cameraId ? names.cameras[row.cameraId] ?? "Camera" : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>        <aside className="panel live-zone-side">
          {manage && (
            <form className="live-zone-form" onSubmit={addStaff}>
              <h2>Add a staff member</h2>
              <label>Full name<input name="name" required maxLength={120} /></label>
              <label>Employee code (optional)<input name="code" maxLength={64} /></label>
              <label className="live-radio live-consent">
                <input type="checkbox" checked={consent} onChange={(event) => setConsent(event.target.checked)} />
                I have this person's written consent to use their face for attendance, and they know they can withdraw it.
              </label>
              <button type="submit" className="live-primary" disabled={busy || !consent}><Icon name="plus" size={14} />Add staff member</button>
            </form>
          )}
          {enrolled && (
            <div className="live-secret">
              <strong>Next: enroll {enrolled.display_name}'s face on the site box</strong>
              <small>Put 3–5 clear photos of their face in a folder on the site box, then run:</small>
              <code>python -m syncam_ai.site_worker enroll --config D:\private\syncam-site.json --staff-id {enrolled.id} --photos D:\private\photos\{enrolled.display_name.split(" ")[0]}</code>
              <div><button type="button" onClick={() => void navigator.clipboard?.writeText(enrolled.id).then(() => onNotify("Staff ID copied"))}>Copy staff ID</button><button type="button" className="secondary" onClick={() => setEnrolled(null)}>Done</button></div>
            </div>
          )}
          <div className="live-zone-list">
            <h2>Staff ({activeStaff.length})</h2>
            {staff.length === 0 && <p className="live-muted">Nobody yet.</p>}
            {staff.map((member) => (
              <div key={member.id} className="live-zone-item">
                <span className={`live-dot ${member.active ? "ok" : ""}`} />
                <span><strong>{member.display_name}</strong><small>Consent recorded {new Date(member.consent_recorded_at).toLocaleDateString()}</small></span>
                {manage && <button type="button" className={member.active ? "live-link danger" : "live-link"} onClick={() => setActive(member, !member.active)}>{member.active ? "Remove" : "Restore"}</button>}
              </div>
            ))}
          </div>
        </aside>
      </div>
    </div>
  );
}
