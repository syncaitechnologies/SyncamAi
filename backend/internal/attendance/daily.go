package attendance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// DayRecord is one staff member's attendance on one day, in the site's time
// zone: the first and last sighting and how many sightings in between. It is
// computed in the database, so a long range never returns raw sightings.
type DayRecord struct {
	Date         string    `json:"date"` // YYYY-MM-DD in the site's time zone
	StaffID      string    `json:"staff_id"`
	StaffName    string    `json:"staff_name"`
	EmployeeCode string    `json:"employee_code,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	Sightings    int       `json:"sightings"`
	// CameraID is the camera of the last sighting that day.
	CameraID string `json:"camera_id"`
}

// DailyLister summarises attendance per staff member per day.
type DailyLister interface {
	ListDaily(ctx context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) ([]DayRecord, error)
}

// MaxDailyRows bounds one summary; 92 days for 200 staff fits comfortably.
const MaxDailyRows = 20000

// ListDaily returns one row per staff member per site-local day with at least
// one sighting between from and to.
func (r *PostgresRepository) ListDaily(ctx context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) ([]DayRecord, error) {
	if location == nil {
		location = time.UTC
	}
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), d.staff_id::text, s.display_name, COALESCE(s.employee_code, ''),
		       d.first_seen, d.last_seen, d.sightings, d.camera_id
		FROM (
			SELECT (r.occurred_at AT TIME ZONE $4)::date AS day, r.tenant_id, r.staff_id,
			       min(r.occurred_at) AS first_seen, max(r.occurred_at) AS last_seen, count(*) AS sightings,
			       (array_agg(r.camera_id::text ORDER BY r.occurred_at DESC))[1] AS camera_id
			FROM attendance.records AS r
			WHERE r.site_id = $1::uuid AND r.occurred_at >= $2 AND r.occurred_at < $3
			GROUP BY 1, r.tenant_id, r.staff_id
		) AS d
		JOIN attendance.staff AS s ON s.tenant_id = d.tenant_id AND s.id = d.staff_id
		ORDER BY d.day DESC, s.display_name, d.staff_id
		LIMIT $5`, siteID, from, to, location.String(), MaxDailyRows)
	if err != nil {
		return nil, fmt.Errorf("list daily attendance: %w", err)
	}
	defer rows.Close()
	result := make([]DayRecord, 0)
	for rows.Next() {
		var record DayRecord
		var sightings int64
		if err := rows.Scan(&record.Date, &record.StaffID, &record.StaffName, &record.EmployeeCode, &record.FirstSeen, &record.LastSeen, &sightings, &record.CameraID); err != nil {
			return nil, fmt.Errorf("scan daily attendance: %w", err)
		}
		record.Sightings = int(sightings)
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily attendance: %w", err)
	}
	return result, tx.Commit(ctx)
}

// ListDaily on the memory repository groups the same way, for tests.
func (r *MemoryRepository) ListDaily(_ context.Context, tenantID, siteID string, location *time.Location, from, to time.Time) ([]DayRecord, error) {
	if location == nil {
		location = time.UTC
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	type key struct{ day, staff string }
	grouped := map[key]*DayRecord{}
	for _, record := range r.records {
		if record.TenantID != tenantID || record.SiteID != siteID || record.OccurredAt.Before(from) || !record.OccurredAt.Before(to) {
			continue
		}
		k := key{record.OccurredAt.In(location).Format("2006-01-02"), record.StaffID}
		day, ok := grouped[k]
		if !ok {
			staff := r.staff[record.StaffID]
			day = &DayRecord{Date: k.day, StaffID: record.StaffID, StaffName: staff.DisplayName, EmployeeCode: staff.EmployeeCode, FirstSeen: record.OccurredAt, LastSeen: record.OccurredAt, CameraID: record.CameraID}
			grouped[k] = day
		}
		day.Sightings++
		if record.OccurredAt.Before(day.FirstSeen) {
			day.FirstSeen = record.OccurredAt
		}
		if !record.OccurredAt.Before(day.LastSeen) {
			day.LastSeen = record.OccurredAt
			day.CameraID = record.CameraID
		}
	}
	result := make([]DayRecord, 0, len(grouped))
	for _, day := range grouped {
		result = append(result, *day)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date > result[j].Date
		}
		return result[i].StaffName < result[j].StaffName
	})
	return result, nil
}
