package privacymasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresReleaseTransportRepository struct{ pool transactionPool }

func NewPostgresReleaseTransportRepository(pool transactionPool) *PostgresReleaseTransportRepository {
	return &PostgresReleaseTransportRepository{pool: pool}
}

func (r *PostgresReleaseTransportRepository) Pull(ctx context.Context, deviceID string, afterVersion int64) (PullReleaseResult, error) {
	if r == nil || r.pool == nil || afterVersion < 0 {
		return PullReleaseResult{}, ErrReleaseDeviceNotFound
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return PullReleaseResult{}, fmt.Errorf("begin privacy release pull: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	manifest, err := scanDeviceReleaseManifest(tx.QueryRow(ctx, `SELECT release_id::text, tenant_id::text, site_id::text, camera_id::text, request_id::text, device_id::text, version, candidate, pipeline, hil_evidence, candidate_hash, evidence_hash, created_at FROM edge.pull_privacy_mask_release($1::uuid, $2)`, deviceID, afterVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return PullReleaseResult{}, fmt.Errorf("commit empty privacy release pull: %w", err)
		}
		return PullReleaseResult{}, nil
	}
	if err != nil {
		return PullReleaseResult{}, classifyReleaseDeviceError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PullReleaseResult{}, fmt.Errorf("commit privacy release pull: %w", err)
	}
	return PullReleaseResult{Manifest: &manifest}, nil
}

func (r *PostgresReleaseTransportRepository) Report(ctx context.Context, command ReportReleaseCommand) (DeviceReleaseStatus, error) {
	if r == nil || r.pool == nil {
		return DeviceReleaseStatus{}, ErrReleaseDeviceNotFound
	}
	if err := validateReleaseReport(command); err != nil {
		return DeviceReleaseStatus{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return DeviceReleaseStatus{}, fmt.Errorf("begin privacy release report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	status, err := scanDeviceReleaseStatus(tx.QueryRow(ctx, `SELECT tenant_id::text, device_id::text, release_id::text, version, state, COALESCE(error_code, ''), reported_at, accepted_at FROM edge.report_privacy_mask_release($1::uuid, $2::uuid, $3, $4::varchar(16), NULLIF($5, '')::varchar(64))`, command.DeviceID, command.ReleaseID, command.Version, command.State, command.ErrorCode))
	if err != nil {
		return DeviceReleaseStatus{}, classifyReleaseDeviceError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceReleaseStatus{}, fmt.Errorf("commit privacy release report: %w", err)
	}
	return status, nil
}

type releaseRowScanner interface{ Scan(...any) error }

func scanDeviceReleaseManifest(row releaseRowScanner) (DeviceReleaseManifest, error) {
	var value DeviceReleaseManifest
	err := row.Scan(&value.ReleaseID, &value.TenantID, &value.SiteID, &value.CameraID, &value.RequestID, &value.DeviceID, &value.Version, &value.Candidate, &value.Pipeline, &value.HILEvidence, &value.CandidateHash, &value.EvidenceHash, &value.CreatedAt)
	return value, err
}
func scanDeviceReleaseStatus(row releaseRowScanner) (DeviceReleaseStatus, error) {
	var value DeviceReleaseStatus
	var accepted pgtype.Timestamptz
	err := row.Scan(&value.TenantID, &value.DeviceID, &value.ReleaseID, &value.Version, &value.State, &value.ErrorCode, &value.ReportedAt, &accepted)
	if accepted.Valid {
		value.AcceptedAt = &accepted.Time
	}
	return value, err
}
func classifyReleaseDeviceError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "28000" || pgErr.Code == "22023") {
		return ErrReleaseDeviceNotFound
	}
	return fmt.Errorf("privacy release transport: %w", err)
}
