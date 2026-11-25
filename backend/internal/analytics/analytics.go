// Package analytics keeps anonymous visitor and table counts (ADR-020):
// people crossing door_line zones and seated sessions at table zones, per
// zone per hour. It never stores pictures, track ids, positions or anything
// that identifies a person.
package analytics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ZoneHour is one site box run's running totals for one zone and hour.
type ZoneHour struct {
	ZoneID          string    `json:"zone_id"`
	HourStart       time.Time `json:"hour_start"`
	RunID           string    `json:"run_id"`
	Entries         int       `json:"entries"`
	Exits           int       `json:"exits"`
	OccupiedSeconds int       `json:"occupied_seconds"`
	Sessions        int       `json:"sessions"`
	SessionSeconds  int64     `json:"session_seconds"`
	PeakPeople      int       `json:"peak_people"`
}

// DayVisits is the door count for one site-local day.
type DayVisits struct {
	Date    string `json:"date"`
	Entries int    `json:"entries"`
	Exits   int    `json:"exits"`
}

// HourVisits is the door count for one hour of the day (0–23, site-local)
// summed over the range.
type HourVisits struct {
	Hour    int `json:"hour"`
	Entries int `json:"entries"`
}

// ZoneTotals sums one zone over the range.
type ZoneTotals struct {
	ZoneID          string `json:"zone_id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Entries         int    `json:"entries"`
	Exits           int    `json:"exits"`
	OccupiedSeconds int64  `json:"occupied_seconds"`
	Sessions        int    `json:"sessions"`
	SessionSeconds  int64  `json:"session_seconds"`
	PeakPeople      int    `json:"peak_people"`
}

// Summary is what the Insights page shows for a range.
type Summary struct {
	Days  []DayVisits  `json:"days"`
	Hours []HourVisits `json:"hours"`
	Zones []ZoneTotals `json:"zones"`
}

// Store saves hourly counts and summarises them.
type Store interface {
	SaveHours(ctx context.Context, tenantID, siteID string, rows []ZoneHour) error
	Summary(ctx context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) (Summary, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresStore keeps counts in platform.zone_hours under the workspace's
// row-level security.
type PostgresStore struct{ pool transactionPool }

func NewPostgresStore(pool transactionPool) *PostgresStore { return &PostgresStore{pool: pool} }

func (p *PostgresStore) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, errors.New("analytics store is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin analytics: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set analytics tenant: %w", err)
	}
	return tx, nil
}

// SaveHours replaces each row's totals, so retries are harmless and separate
// runs of the site box add up in Summary.
func (p *PostgresStore) SaveHours(ctx context.Context, tenantID, siteID string, rows []ZoneHour) error {
	tx, err := p.begin(ctx, tenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, row := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO platform.zone_hours (tenant_id, site_id, zone_id, hour_start, run_id, entries, exits, occupied_seconds, sessions, session_seconds, peak_people)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (zone_id, hour_start, run_id) DO UPDATE SET
				entries = EXCLUDED.entries, exits = EXCLUDED.exits, occupied_seconds = EXCLUDED.occupied_seconds,
				sessions = EXCLUDED.sessions, session_seconds = EXCLUDED.session_seconds, peak_people = EXCLUDED.peak_people,
				updated_at = clock_timestamp()`,
			tenantID, siteID, row.ZoneID, row.HourStart, row.RunID, row.Entries, row.Exits, row.OccupiedSeconds, row.Sessions, row.SessionSeconds, row.PeakPeople); err != nil {
			return fmt.Errorf("save zone hour: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Summary groups by the site's local day and hour of day.
// shortcut: a whole UTC hour is assigned to the local day and hour it starts
// in, so sites on a half-hour offset (India) see :30 boundaries rounded down;
// store quarter-hours if that matters.
func (p *PostgresStore) Summary(ctx context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) (Summary, error) {
	if location == nil {
		location = time.UTC
	}
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := Summary{Days: []DayVisits{}, Hours: []HourVisits{}, Zones: []ZoneTotals{}}
	zone := location.String()

	rows, err := tx.Query(ctx, `
		SELECT to_char((hour_start AT TIME ZONE $4)::date, 'YYYY-MM-DD'), sum(entries)::int, sum(exits)::int
		FROM platform.zone_hours WHERE site_id = $1::uuid AND hour_start >= $2 AND hour_start < $3
		GROUP BY 1 ORDER BY 1`, siteID, from, to, zone)
	if err != nil {
		return Summary{}, fmt.Errorf("summarise days: %w", err)
	}
	for rows.Next() {
		var day DayVisits
		if err := rows.Scan(&day.Date, &day.Entries, &day.Exits); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("read days: %w", err)
		}
		result.Days = append(result.Days, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read days: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT extract(hour FROM hour_start AT TIME ZONE $4)::int, sum(entries)::int
		FROM platform.zone_hours WHERE site_id = $1::uuid AND hour_start >= $2 AND hour_start < $3
		GROUP BY 1 ORDER BY 1`, siteID, from, to, zone)
	if err != nil {
		return Summary{}, fmt.Errorf("summarise hours: %w", err)
	}
	for rows.Next() {
		var hour HourVisits
		if err := rows.Scan(&hour.Hour, &hour.Entries); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("read hours: %w", err)
		}
		result.Hours = append(result.Hours, hour)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read hours: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT h.zone_id::text, COALESCE(z.name, 'Removed zone'), COALESCE(z.kind, ''),
		       sum(h.entries)::int, sum(h.exits)::int, sum(h.occupied_seconds)::bigint, sum(h.sessions)::int,
		       sum(h.session_seconds)::bigint, max(h.peak_people)::int
		FROM platform.zone_hours AS h LEFT JOIN config.zones AS z ON z.id = h.zone_id
		WHERE h.site_id = $1::uuid AND h.hour_start >= $2 AND h.hour_start < $3
		GROUP BY 1, 2, 3 ORDER BY 2, 1`, siteID, from, to)
	if err != nil {
		return Summary{}, fmt.Errorf("summarise zones: %w", err)
	}
	for rows.Next() {
		var totals ZoneTotals
		if err := rows.Scan(&totals.ZoneID, &totals.Name, &totals.Kind, &totals.Entries, &totals.Exits, &totals.OccupiedSeconds,
			&totals.Sessions, &totals.SessionSeconds, &totals.PeakPeople); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("read zones: %w", err)
		}
		result.Zones = append(result.Zones, totals)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read zones: %w", err)
	}
	return result, tx.Commit(ctx)
}

// MemoryStore is an in-memory Store for tests and local development.
type MemoryStore struct {
	mu    sync.Mutex
	rows  map[string]ZoneHour
	site  map[string]string // zone -> tenant|site
	Names map[string][2]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]ZoneHour{}, site: map[string]string{}, Names: map[string][2]string{}}
}

func (m *MemoryStore) SaveHours(_ context.Context, tenantID, siteID string, rows []ZoneHour) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range rows {
		key := row.ZoneID + "|" + row.HourStart.UTC().Format(time.RFC3339) + "|" + row.RunID
		m.rows[key] = row
		m.site[row.ZoneID] = tenantID + "|" + siteID
	}
	return nil
}

func (m *MemoryStore) Summary(_ context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) (Summary, error) {
	if location == nil {
		location = time.UTC
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	days, hours, zones := map[string]*DayVisits{}, map[int]*HourVisits{}, map[string]*ZoneTotals{}
	for _, row := range m.rows {
		if m.site[row.ZoneID] != tenantID+"|"+siteID || row.HourStart.Before(from) || !row.HourStart.Before(to) {
			continue
		}
		local := row.HourStart.In(location)
		day := days[local.Format("2006-01-02")]
		if day == nil {
			day = &DayVisits{Date: local.Format("2006-01-02")}
			days[day.Date] = day
		}
		day.Entries += row.Entries
		day.Exits += row.Exits
		hour := hours[local.Hour()]
		if hour == nil {
			hour = &HourVisits{Hour: local.Hour()}
			hours[hour.Hour] = hour
		}
		hour.Entries += row.Entries
		totals := zones[row.ZoneID]
		if totals == nil {
			name := m.Names[row.ZoneID]
			if name[0] == "" {
				name[0] = "Removed zone"
			}
			totals = &ZoneTotals{ZoneID: row.ZoneID, Name: name[0], Kind: name[1]}
			zones[row.ZoneID] = totals
		}
		totals.Entries += row.Entries
		totals.Exits += row.Exits
		totals.OccupiedSeconds += int64(row.OccupiedSeconds)
		totals.Sessions += row.Sessions
		totals.SessionSeconds += row.SessionSeconds
		if row.PeakPeople > totals.PeakPeople {
			totals.PeakPeople = row.PeakPeople
		}
	}
	result := Summary{Days: []DayVisits{}, Hours: []HourVisits{}, Zones: []ZoneTotals{}}
	for _, day := range days {
		result.Days = append(result.Days, *day)
	}
	for _, hour := range hours {
		result.Hours = append(result.Hours, *hour)
	}
	for _, totals := range zones {
		result.Zones = append(result.Zones, *totals)
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	sort.Slice(result.Hours, func(i, j int) bool { return result.Hours[i].Hour < result.Hours[j].Hour })
	sort.Slice(result.Zones, func(i, j int) bool { return result.Zones[i].Name < result.Zones[j].Name })
	return result, nil
}
