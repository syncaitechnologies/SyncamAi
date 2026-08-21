package configdelivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Publish(ctx context.Context, command PublishCommand) (Revision, error) {
	payload, hash, err := normalizePayload(command.Payload)
	if err != nil {
		return Revision{}, err
	}
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Revision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", command.TenantID+":"+command.SiteID+":configuration"); err != nil {
		return Revision{}, fmt.Errorf("lock configuration revisions: %w", err)
	}
	var siteExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE id = $1::uuid AND status <> 'retired')`, command.SiteID).Scan(&siteExists); err != nil {
		return Revision{}, fmt.Errorf("validate configuration site: %w", err)
	}
	if !siteExists {
		return Revision{}, ErrRevisionNotFound
	}
	var last int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) FROM config.configuration_revisions WHERE site_id = $1::uuid`, command.SiteID).Scan(&last); err != nil {
		return Revision{}, fmt.Errorf("read latest configuration revision: %w", err)
	}
	revision := Revision{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, Number: last + 1, Payload: payload, ContentHash: hash, CreatedBy: command.ActorID}
	if err := tx.QueryRow(ctx, `INSERT INTO config.configuration_revisions (id, tenant_id, site_id, revision, payload, content_hash, created_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::jsonb, $6, $7) RETURNING created_at`, revision.ID, revision.TenantID, revision.SiteID, revision.Number, revision.Payload, revision.ContentHash, revision.CreatedBy).Scan(&revision.CreatedAt); err != nil {
		return Revision{}, fmt.Errorf("write configuration revision: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{TenantID: command.TenantID, ActorID: command.ActorID, Action: "configuration.published", ResourceType: "configuration_revision", ResourceID: revision.ID, RequestID: command.RequestID, AfterState: revision, OccurredAt: revision.CreatedAt}); err != nil {
		return Revision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Revision{}, fmt.Errorf("commit configuration revision: %w", err)
	}
	return revision, nil
}

func (r *PostgresRepository) List(ctx context.Context, tenantID, siteID string) ([]Revision, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query, args := revisionSelect+" ORDER BY site_id, revision DESC LIMIT 500", []any{}
	if siteID != "" {
		query, args = revisionSelect+" WHERE site_id = $1::uuid ORDER BY revision DESC LIMIT 500", []any{siteID}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list configuration revisions: %w", err)
	}
	defer rows.Close()
	result := make([]Revision, 0)
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate configuration revisions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit configuration revision list: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) Pull(ctx context.Context, deviceID string, afterRevision int64) (PullResult, error) {
	if r == nil || r.pool == nil {
		return PullResult{}, errors.New("postgres configuration repository is unavailable")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return PullResult{}, fmt.Errorf("begin configuration pull: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	revision, err := scanRevision(tx.QueryRow(ctx, `SELECT revision_id::text, tenant_id::text, site_id::text, revision, payload, content_hash, created_by, created_at FROM edge.pull_device_configuration($1::uuid, $2)`, deviceID, afterRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return PullResult{}, fmt.Errorf("commit empty configuration pull: %w", err)
		}
		return PullResult{}, nil
	}
	if err != nil {
		return PullResult{}, classifyDeviceError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PullResult{}, fmt.Errorf("commit configuration pull: %w", err)
	}
	return PullResult{Revision: &revision}, nil
}

func (r *PostgresRepository) DesiredRevision(ctx context.Context, deviceID string) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("postgres configuration repository is unavailable")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, fmt.Errorf("begin desired configuration revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT edge.desired_device_configuration_revision($1::uuid)`, deviceID).Scan(&revision); err != nil {
		return 0, classifyDeviceError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit desired configuration revision: %w", err)
	}
	return revision, nil
}

func (r *PostgresRepository) Report(ctx context.Context, command ReportCommand) (DeviceStatus, error) {
	if r == nil || r.pool == nil {
		return DeviceStatus{}, errors.New("postgres configuration repository is unavailable")
	}
	if err := validateReport(command); err != nil {
		return DeviceStatus{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return DeviceStatus{}, fmt.Errorf("begin configuration report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	status, err := scanStatus(tx.QueryRow(ctx, `SELECT device_id::text, tenant_id::text, site_id::text, revision, state, COALESCE(error_message, ''), reported_at, applied_at FROM edge.report_device_configuration($1::uuid, $2, $3::varchar(16), NULLIF($4, '')::varchar(512))`, command.DeviceID, command.Revision, command.State, command.ErrorMessage))
	if err != nil {
		return DeviceStatus{}, classifyDeviceError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceStatus{}, fmt.Errorf("commit configuration report: %w", err)
	}
	return status, nil
}

func (r *PostgresRepository) GetStatus(ctx context.Context, tenantID, deviceID string) (DeviceStatus, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return DeviceStatus{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	status, err := scanStatus(tx.QueryRow(ctx, `SELECT device_id::text, tenant_id::text, site_id::text, revision, state, COALESCE(error_message, ''), reported_at, applied_at FROM config.device_configuration_statuses WHERE device_id = $1::uuid`, deviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceStatus{}, ErrDeviceNotFound
	}
	if err != nil {
		return DeviceStatus{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceStatus{}, fmt.Errorf("commit configuration status: %w", err)
	}
	return status, nil
}

const revisionSelect = `SELECT id::text, tenant_id::text, site_id::text, revision, payload, content_hash, created_by, created_at FROM config.configuration_revisions`

type rowScanner interface{ Scan(...any) error }

func scanRevision(row rowScanner) (Revision, error) {
	var revision Revision
	if err := row.Scan(&revision.ID, &revision.TenantID, &revision.SiteID, &revision.Number, &revision.Payload, &revision.ContentHash, &revision.CreatedBy, &revision.CreatedAt); err != nil {
		return Revision{}, err
	}
	return revision, nil
}
func scanStatus(row rowScanner) (DeviceStatus, error) {
	var status DeviceStatus
	var applied pgtype.Timestamptz
	if err := row.Scan(&status.DeviceID, &status.TenantID, &status.SiteID, &status.Revision, &status.State, &status.ErrorMessage, &status.ReportedAt, &applied); err != nil {
		return DeviceStatus{}, err
	}
	if applied.Valid {
		status.AppliedAt = &applied.Time
	}
	return status, nil
}

func (r *PostgresRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres configuration repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin configuration transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set configuration tenant context: %w", err)
	}
	return tx, nil
}

func classifyDeviceError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "28000" || postgresError.Code == "22023") {
		return ErrDeviceNotFound
	}
	return fmt.Errorf("configuration delivery: %w", err)
}
