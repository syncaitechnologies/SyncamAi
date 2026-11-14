import { useEffect, useMemo, useState, type FormEvent } from "react";

import { attendanceCsv, dayBounds, summarizeAttendance } from "./attendance-model";
import { Icon } from "./icon";
import type { AttendanceRecord, StaffMember } from "./syncam-api";
import type { WorkspaceData } from "./use-workspace";
import { canManage, readWorkspace } from "./workspace";

function today() {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

export function AttendanceWorkspace({ workspace, onNotify }: { workspace: WorkspaceData; onNotify: (message: string) => void }) {
  const { api, site, names } = workspace;
  const manage = canManage(readWorkspace());
  const [day, setDay] = useState(today);
  const [staff, setStaff] = useState<StaffMember[]>([]);
  const [records, setRecords] = useState<AttendanceRecord[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [consent, setConsent] = useState(false);
  const [enrolled, setEnrolled] = useState<StaffMember | null>(null);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!api?.siteId) return;
    const abort = new AbortController();
    const { from, to } = dayBounds(day);
    setError("");
    Promise.all([api.listStaff(api.siteId, abort.signal), api.listAttendance(api.siteId, from, to, abort.signal)])
      .then(([listedStaff, listedRecords]) => {
        setStaff(listedStaff);
        setRecords(listedRecords);
      })
      .catch((caught: unknown) => {
        if (!abort.signal.aborted) setError(caught instanceof Error ? caught.message : "Attendance could not be loaded.");
      });
    return () => abort.abort();
  }, [api, day, version]);

  const activeStaff = useMemo(() => staff.filter((member) => member.active), [staff]);
  const rows = useMemo(() => summarizeAttendance(activeStaff, records), [activeStaff, records]);
  if (!api) return null;
  const present = rows.filter((row) => row.firstSeen).length;

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
    const blob = new Blob([attendanceCsv(rows, day, (iso) => timeFormat.format(new Date(iso)))], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `attendance-${site?.name ?? "site"}-${day}.csv`.replaceAll(/[^\w.-]+/g, "_");
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
        <div className="heading-actions">
          <label className="site-filter-label" htmlFor="attendance-day">Day</label>
          <input id="attendance-day" className="live-date" type="date" value={day} max={today()} onChange={(event) => setDay(event.target.value || today())} />
          <button type="button" className="live-primary" onClick={exportCsv} disabled={!rows.length}><Icon name="file" size={14} />Export CSV</button>
        </div>
      </section>
      {error && <p className="live-banner">{error}</p>}
      <div className="live-zone-layout">
        <section className="panel live-card">
          <header><div><h2>{present} of {activeStaff.length} present</h2><p>{new Date(dayBounds(day).from).toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" })}</p></div></header>
          <table className="live-table">
            <thead><tr><th>Name</th><th>Code</th><th>First seen</th><th>Last seen</th><th>Camera</th></tr></thead>
            <tbody>
              {rows.length === 0 && <tr><td colSpan={5} className="live-muted">No staff enrolled yet.</td></tr>}
              {rows.map((row) => {
                const last = records.filter((record) => record.staff_id === row.staffId).at(-1);
                return (
                  <tr key={row.staffId} className={row.firstSeen ? "" : "absent"}>
                    <td>{row.name}</td>
                    <td>{row.employeeCode || "—"}</td>
                    <td>{row.firstSeen ? timeFormat.format(new Date(row.firstSeen)) : "Absent"}</td>
                    <td>{row.lastSeen ? timeFormat.format(new Date(row.lastSeen)) : "—"}</td>
                    <td>{last ? names.cameras[last.camera_id] ?? "Camera" : "—"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
        <aside className="panel live-zone-side">
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
