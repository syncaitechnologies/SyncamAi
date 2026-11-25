import { useEffect, useMemo, useState } from "react";

import { quickRanges, rangeDays, rangeLabel, rangeProblem } from "./attendance-model";
import { Icon } from "./icon";
import {
  averageDiningMinutes,
  barPercent,
  busiestHour,
  dailySeries,
  doorNames,
  durationLabel,
  hourLabel,
  hourSeries,
  insightsCsv,
  tableRows,
  totalVisitors,
  turnover,
  type InsightsSummary,
} from "./insights-model";
import type { WorkspaceData } from "./use-workspace";

function todayIn(timeZone: string | undefined) {
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  } catch {
    return new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  }
}

// Insights (ADR-020): anonymous visitor and table counts from door counter
// and table zones. No faces and nothing that identifies a person.
export function InsightsWorkspace({ workspace }: { workspace: Pick<WorkspaceData, "api" | "site"> }) {
  const { api, site } = workspace;
  const today = todayIn(site?.timezone);
  const [from, setFrom] = useState(() => quickRanges(today)[2]!.from);
  const [to, setTo] = useState(today);
  const [summary, setSummary] = useState<InsightsSummary | null>(null);
  const [error, setError] = useState("");
  const problem = rangeProblem(from, to);

  useEffect(() => {
    if (!api?.siteId || problem) return;
    const abort = new AbortController();
    setError("");
    api.insights(api.siteId, from, to, abort.signal)
      .then(setSummary)
      .catch((caught: unknown) => {
        if (!abort.signal.aborted) setError(caught instanceof Error ? caught.message : "Insights could not be loaded.");
      });
    return () => abort.abort();
  }, [api, from, to, problem]);

  const dayCount = useMemo(() => (problem ? 0 : rangeDays(from, to).length), [from, to, problem]);
  if (!api) return null;
  const empty = summary !== null && summary.zones.length === 0;
  const daily = summary ? dailySeries(summary, from, to) : [];
  const hours = summary ? hourSeries(summary) : [];
  const busiest = summary ? busiestHour(summary) : null;
  const visitors = summary ? totalVisitors(summary) : 0;
  const tables = summary ? tableRows(summary, dayCount) : [];
  const dailyMax = Math.max(0, ...daily.map((day) => day.entries));
  const hourMax = Math.max(0, ...hours.map((hour) => hour.entries));

  function exportCsv() {
    if (!summary) return;
    const url = URL.createObjectURL(new Blob([insightsCsv(summary, from, to)], { type: "text/csv" }));
    const link = document.createElement("a");
    link.href = url;
    const period = from === to ? from : `${from}_to_${to}`;
    link.download = `insights-${site?.name ?? "site"}-${period}.csv`.replaceAll(/[^\w.-]+/g, "_");
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="live-page">
      <section className="page-heading live-heading">
        <div>
          <div className="heading-kicker"><span className="live-pill"><span />LIVE</span><span>Anonymous counts · no faces</span></div>
          <h1>Insights</h1>
          <p>How many people visit, when it gets busy, and how your tables are used. Only counts are kept; nobody is recognised or recorded.</p>
        </div>
        <div className="heading-actions attendance-range">
          <label className="site-filter-label" htmlFor="insights-from">From</label>
          <input id="insights-from" className="live-date" type="date" value={from} max={to || today} onChange={(event) => setFrom(event.target.value)} />
          <label className="site-filter-label" htmlFor="insights-to">To</label>
          <input id="insights-to" className="live-date" type="date" value={to} min={from} max={today} onChange={(event) => setTo(event.target.value)} />
          <button type="button" className="live-primary" onClick={exportCsv} disabled={!summary || empty || Boolean(problem)}><Icon name="file" size={14} />Export CSV</button>
        </div>
      </section>
      <div className="attendance-quick" role="group" aria-label="Quick date ranges">
        {quickRanges(today).map((range) => (
          <button key={range.label} type="button" aria-pressed={range.from === from && range.to === to}
            className={range.from === from && range.to === to ? "live-link attendance-quick-on" : "live-link"}
            onClick={() => { setFrom(range.from); setTo(range.to); }}>
            {range.label}
          </button>
        ))}
      </div>
      {problem && <p className="live-banner">{problem}</p>}
      {error && <p className="live-banner">{error}</p>}
      {!summary && !error && !problem && <p className="live-muted insights-empty" role="status">Loading insights…</p>}
      {empty && (
        <p className="live-banner insights-empty">
          Nothing counted yet. In Zones &amp; rules, draw a <strong>Door counter</strong> across your entrance and a <strong>Table</strong> around each table.
          Counting starts within a minute on site box 0.1.7 or later.
        </p>
      )}
      {summary && !empty && (
        <>
          <div className="insights-stats">
            <div className="panel insights-stat">
              <span>Visitors</span>
              <strong>{visitors.toLocaleString()}</strong>
              <small>{dayCount > 1 ? `${Math.round(visitors / dayCount).toLocaleString()} a day on average` : rangeLabel(from, to)}</small>
            </div>
            <div className="panel insights-stat">
              <span>Busiest hour</span>
              <strong>{busiest ? hourLabel(busiest.hour) : "–"}</strong>
              <small>{busiest ? `${busiest.entries.toLocaleString()} people came in` : "No visitors counted yet"}</small>
            </div>
            <div className="panel insights-stat">
              <span>Average dining time</span>
              <strong>{durationLabel(averageDiningMinutes(summary))}</strong>
              <small>From the first minute seated to leaving the table</small>
            </div>
            <div className="panel insights-stat">
              <span>Table turnover</span>
              <strong>{turnover(summary, dayCount) ?? "–"}</strong>
              <small>Groups per table per day</small>
            </div>
          </div>
          <div className="insights-grid">
            <section className="panel live-card" aria-labelledby="insights-daily">
              <header><div><h2 id="insights-daily">Visitors per day</h2><p>{rangeLabel(from, to)}{doorNames(summary).length ? ` · ${doorNames(summary).join(", ")}` : ""}</p></div></header>
              <div className="insights-bars" role="img" aria-label={`Visitors per day, highest ${dailyMax}`}>
                {daily.map((day) => (
                  <div key={day.date} className={`insights-bar ${day.entries === dailyMax && dailyMax > 0 ? "peak" : ""}`} title={`${day.date}: ${day.entries}`}>
                    <i style={{ height: `${barPercent(day.entries, dailyMax)}%` }} />
                  </div>
                ))}
              </div>
              <div className="insights-axis"><span>{daily[0]?.date}</span><span>{daily.at(-1)?.date}</span></div>
            </section>
            <section className="panel live-card" aria-labelledby="insights-hours">
              <header><div><h2 id="insights-hours">When people come in</h2><p>All days in the range, by hour{site?.timezone ? ` (${site.timezone})` : ""}</p></div></header>
              <div className="insights-bars" role="img" aria-label={busiest ? `Busiest ${hourLabel(busiest.hour)}` : "No visitors yet"}>
                {hours.map((hour) => (
                  <div key={hour.hour} className={`insights-bar ${hour.entries === hourMax && hourMax > 0 ? "peak" : ""}`} title={`${hourLabel(hour.hour)}: ${hour.entries}`}>
                    <i style={{ height: `${barPercent(hour.entries, hourMax)}%` }} />
                  </div>
                ))}
              </div>
              <div className="insights-axis"><span>12 am</span><span>6 am</span><span>12 pm</span><span>6 pm</span><span>11 pm</span></div>
            </section>
          </div>
          <section className="panel live-card insights-tables" aria-labelledby="insights-tables" style={{ margin: "0 38px 18px" }}>
            <header><div><h2 id="insights-tables">Tables</h2><p>A group counts once someone has been seated for a minute. Occupancy is the share of open hours (hours with anyone counted) the table was in use.</p></div></header>
            <div className="attendance-table-wrap">
              <table className="live-table">
                <thead><tr><th>Table</th><th>Groups</th><th>Per day</th><th>Average time</th><th>Hours in use</th><th>Occupancy</th><th>Most seated</th></tr></thead>
                <tbody>
                  {tables.length === 0 && <tr><td colSpan={7} className="live-muted">No tables yet. Draw a Table zone around each table in Zones &amp; rules.</td></tr>}
                  {tables.map((row) => (
                    <tr key={row.zoneId}>
                      <td>{row.name}</td>
                      <td>{row.sessions}</td>
                      <td>{row.sessionsPerDay}</td>
                      <td>{durationLabel(row.averageMinutes)}</td>
                      <td>{row.occupiedHours}</td>
                      <td>{row.occupancyPercent === null ? "–" : `${row.occupancyPercent}%`}</td>
                      <td>{row.peakPeople}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
          <p className="live-muted insights-empty">
            Counts are estimates: people hidden behind others can be counted twice or missed, and staff walking through the door are counted too.
          </p>
        </>
      )}
    </div>
  );
}
