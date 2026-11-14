// Package attendance keeps the staff roster, recorded consent and sightings
// reported by site boxes. Face templates never reach this service.
package attendance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrStaffNotFound = errors.New("staff member not found")
	ErrSiteNotFound  = errors.New("site not found")
)

type Staff struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	SiteID            string    `json:"site_id"`
	DisplayName       string    `json:"display_name"`
	EmployeeCode      string    `json:"employee_code,omitempty"`
	ConsentRecordedAt time.Time `json:"consent_recorded_at"`
	ConsentRecordedBy string    `json:"consent_recorded_by"`
	Active            bool      `json:"active"`
	CreatedAt         time.Time `json:"created_at"`
}

type Record struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	SiteID     string    `json:"site_id"`
	StaffID    string    `json:"staff_id"`
	StaffName  string    `json:"staff_name,omitempty"`
	CameraID   string    `json:"camera_id"`
	OccurredAt time.Time `json:"occurred_at"`
	Confidence float64   `json:"confidence"`
}

type CreateStaffCommand struct {
	TenantID, SiteID, ActorID, DisplayName, EmployeeCode string
}

type Repository interface {
	ListStaff(ctx context.Context, tenantID, siteID string) ([]Staff, error)
	CreateStaff(ctx context.Context, command CreateStaffCommand) (Staff, error)
	SetStaffActive(ctx context.Context, tenantID, staffID string, active bool) (Staff, error)
	RecordSighting(ctx context.Context, record Record) (bool, error)
	ListRecords(ctx context.Context, tenantID, siteID string, from, to time.Time) ([]Record, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("attendance repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin attendance transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set attendance tenant context: %w", err)
	}
	return tx, nil
}

const staffSelect = `SELECT id::text, tenant_id::text, site_id::text, display_name, employee_code, consent_recorded_at, consent_recorded_by, active, created_at FROM attendance.staff`

type rowScanner interface{ Scan(...any) error }

func scanStaff(row rowScanner) (Staff, error) {
	var staff Staff
	err := row.Scan(&staff.ID, &staff.TenantID, &staff.SiteID, &staff.DisplayName, &staff.EmployeeCode, &staff.ConsentRecordedAt, &staff.ConsentRecordedBy, &staff.Active, &staff.CreatedAt)
	return staff, err
}

func (r *PostgresRepository) ListStaff(ctx context.Context, tenantID, siteID string) ([]Staff, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, staffSelect+` WHERE ($1::uuid IS NULL OR site_id = $1::uuid) ORDER BY display_name, id LIMIT 1000`, nullable(siteID))
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()
	staff := make([]Staff, 0)
	for rows.Next() {
		member, err := scanStaff(rows)
		if err != nil {
			return nil, fmt.Errorf("scan staff: %w", err)
		}
		staff = append(staff, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate staff: %w", err)
	}
	return staff, tx.Commit(ctx)
}

func (r *PostgresRepository) CreateStaff(ctx context.Context, command CreateStaffCommand) (Staff, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Staff{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	staff := Staff{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, DisplayName: strings.TrimSpace(command.DisplayName), EmployeeCode: strings.TrimSpace(command.EmployeeCode), ConsentRecordedBy: command.ActorID, Active: true}
	err = tx.QueryRow(ctx, `
		INSERT INTO attendance.staff (id, tenant_id, site_id, display_name, employee_code, consent_recorded_at, consent_recorded_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, clock_timestamp(), $6)
		RETURNING consent_recorded_at, created_at`, staff.ID, staff.TenantID, staff.SiteID, staff.DisplayName, staff.EmployeeCode, staff.ConsentRecordedBy).
		Scan(&staff.ConsentRecordedAt, &staff.CreatedAt)
	if isForeignKey(err) {
		return Staff{}, ErrSiteNotFound
	}
	if err != nil {
		return Staff{}, fmt.Errorf("create staff: %w", err)
	}
	return staff, tx.Commit(ctx)
}

func (r *PostgresRepository) SetStaffActive(ctx context.Context, tenantID, staffID string, active bool) (Staff, error) {
	if _, err := uuid.Parse(staffID); err != nil {
		return Staff{}, ErrStaffNotFound
	}
	tx, err := r.begin(ctx, tenantID, pgx.ReadWrite)
	if err != nil {
		return Staff{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	staff, err := scanStaff(tx.QueryRow(ctx, `UPDATE attendance.staff SET active = $2 WHERE id = $1::uuid RETURNING id::text, tenant_id::text, site_id::text, display_name, employee_code, consent_recorded_at, consent_recorded_by, active, created_at`, staffID, active))
	if errors.Is(err, pgx.ErrNoRows) {
		return Staff{}, ErrStaffNotFound
	}
	if err != nil {
		return Staff{}, fmt.Errorf("update staff: %w", err)
	}
	return staff, tx.Commit(ctx)
}

// RecordSighting stores one sighting of an active staff member at the site.
// A retried sighting with the same ID is accepted without a duplicate row.
func (r *PostgresRepository) RecordSighting(ctx context.Context, record Record) (bool, error) {
	tx, err := r.begin(ctx, record.TenantID, pgx.ReadWrite)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		INSERT INTO attendance.records (id, tenant_id, site_id, staff_id, camera_id, occurred_at, confidence)
		SELECT $1::uuid, $2::uuid, $3::uuid, s.id, $5::uuid, $6, $7
		FROM attendance.staff AS s
		WHERE s.id = $4::uuid AND s.site_id = $3::uuid AND s.active
		ON CONFLICT (id) DO NOTHING`, record.ID, record.TenantID, record.SiteID, record.StaffID, record.CameraID, record.OccurredAt, record.Confidence)
	if err != nil {
		return false, fmt.Errorf("record sighting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attendance.records WHERE id = $1::uuid)`, record.ID).Scan(&exists); err != nil {
			return false, fmt.Errorf("check sighting replay: %w", err)
		}
		if !exists {
			return false, ErrStaffNotFound
		}
		return false, tx.Commit(ctx)
	}
	return true, tx.Commit(ctx)
}

func (r *PostgresRepository) ListRecords(ctx context.Context, tenantID, siteID string, from, to time.Time) ([]Record, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT r.id::text, r.tenant_id::text, r.site_id::text, r.staff_id::text, s.display_name, r.camera_id::text, r.occurred_at, r.confidence
		FROM attendance.records AS r
		JOIN attendance.staff AS s ON s.tenant_id = r.tenant_id AND s.id = r.staff_id
		WHERE r.site_id = $1::uuid AND r.occurred_at >= $2 AND r.occurred_at < $3
		ORDER BY r.occurred_at
		LIMIT 5000`, siteID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list attendance: %w", err)
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		var record Record
		var confidence float32
		if err := rows.Scan(&record.ID, &record.TenantID, &record.SiteID, &record.StaffID, &record.StaffName, &record.CameraID, &record.OccurredAt, &confidence); err != nil {
			return nil, fmt.Errorf("scan attendance: %w", err)
		}
		record.Confidence = float64(confidence)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attendance: %w", err)
	}
	return records, tx.Commit(ctx)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isForeignKey(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23503"
}

// MemoryRepository is a deterministic Repository for tests.
type MemoryRepository struct {
	mu      sync.Mutex
	sites   map[string]bool
	staff   map[string]Staff
	records map[string]Record
}

// NewMemoryRepository accepts the "tenant:site" pairs that exist.
func NewMemoryRepository(sites ...string) *MemoryRepository {
	known := map[string]bool{}
	for _, site := range sites {
		known[site] = true
	}
	return &MemoryRepository{sites: known, staff: map[string]Staff{}, records: map[string]Record{}}
}

func (r *MemoryRepository) ListStaff(_ context.Context, tenantID, siteID string) ([]Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Staff, 0)
	for _, member := range r.staff {
		if member.TenantID == tenantID && (siteID == "" || member.SiteID == siteID) {
			result = append(result, member)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DisplayName < result[j].DisplayName })
	return result, nil
}

func (r *MemoryRepository) CreateStaff(_ context.Context, command CreateStaffCommand) (Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sites[command.TenantID+":"+command.SiteID] {
		return Staff{}, ErrSiteNotFound
	}
	now := time.Now().UTC()
	staff := Staff{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, DisplayName: strings.TrimSpace(command.DisplayName), EmployeeCode: strings.TrimSpace(command.EmployeeCode), ConsentRecordedAt: now, ConsentRecordedBy: command.ActorID, Active: true, CreatedAt: now}
	r.staff[staff.ID] = staff
	return staff, nil
}

func (r *MemoryRepository) SetStaffActive(_ context.Context, tenantID, staffID string, active bool) (Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	staff, ok := r.staff[staffID]
	if !ok || staff.TenantID != tenantID {
		return Staff{}, ErrStaffNotFound
	}
	staff.Active = active
	r.staff[staffID] = staff
	return staff, nil
}

func (r *MemoryRepository) RecordSighting(_ context.Context, record Record) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[record.ID]; ok {
		return false, nil
	}
	staff, ok := r.staff[record.StaffID]
	if !ok || staff.TenantID != record.TenantID || staff.SiteID != record.SiteID || !staff.Active {
		return false, ErrStaffNotFound
	}
	record.StaffName = staff.DisplayName
	r.records[record.ID] = record
	return true, nil
}

func (r *MemoryRepository) ListRecords(_ context.Context, tenantID, siteID string, from, to time.Time) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Record, 0)
	for _, record := range r.records {
		if record.TenantID == tenantID && record.SiteID == siteID && !record.OccurredAt.Before(from) && record.OccurredAt.Before(to) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OccurredAt.Before(result[j].OccurredAt) })
	return result, nil
}
