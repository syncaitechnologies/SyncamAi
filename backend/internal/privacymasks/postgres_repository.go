package privacymasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresRepository persists only privacy-mask governance metadata. It never
// reads or writes video, pixels, credentials, or executable mask content.
type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, command CreateCommand) (Request, error) {
	if err := validateCreate(command); err != nil {
		return Request{}, err
	}
	if err := validateAuditRequestID(command.RequestID); err != nil {
		return Request{}, err
	}
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	request := Request{
		ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID,
		CameraID: command.CameraID, Name: strings.TrimSpace(command.Name),
		Geometry: append([]byte(nil), command.Geometry...), Status: StatusPending,
		RequestedBy: strings.TrimSpace(command.ActorID),
	}
	if err := tx.QueryRow(ctx, `INSERT INTO config.privacy_mask_requests (
		id, tenant_id, site_id, camera_id, name, geometry, status, requested_by
	) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6::jsonb, $7, $8)
	RETURNING requested_at`, request.ID, request.TenantID, request.SiteID,
		request.CameraID, request.Name, request.Geometry, request.Status,
		request.RequestedBy).Scan(&request.RequestedAt); err != nil {
		return Request{}, fmt.Errorf("write privacy mask request: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID,
		Action: "privacy_mask.requested", ResourceType: "privacy_mask_request",
		ResourceID: request.ID, RequestID: command.RequestID, AfterState: request,
		OccurredAt: request.RequestedAt,
	}); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("commit privacy mask request: %w", err)
	}
	return clone(request), nil
}

func (r *PostgresRepository) Get(ctx context.Context, tenantID, requestID string) (Request, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	request, err := loadRequest(ctx, tx, requestID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("read privacy mask request: %w", err)
	}
	approvals, err := loadApprovals(ctx, tx, requestID)
	if err != nil {
		return Request{}, fmt.Errorf("read privacy mask approvals: %w", err)
	}
	request.Approvals = approvals
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("commit privacy mask read: %w", err)
	}
	return clone(request), nil
}

func (r *PostgresRepository) Approve(ctx context.Context, command ApproveCommand) (Request, error) {
	if err := validateApprove(command); err != nil {
		return Request{}, err
	}
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := loadRequest(ctx, tx, command.RequestID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("lock privacy mask request: %w", err)
	}
	approvals, err := loadApprovals(ctx, tx, command.RequestID)
	if err != nil {
		return Request{}, fmt.Errorf("read privacy mask approvals: %w", err)
	}
	current.Approvals = approvals
	if command.ActorID == current.RequestedBy {
		return Request{}, ErrRequesterCannotApprove
	}
	for _, approval := range approvals {
		if approval.ApproverID == command.ActorID {
			if err := tx.Commit(ctx); err != nil {
				return Request{}, fmt.Errorf("commit replayed privacy mask approval: %w", err)
			}
			return clone(current), nil
		}
	}
	if current.Status == StatusApproved || len(approvals) >= 2 {
		return Request{}, ErrAlreadyApproved
	}
	next := clone(current)
	approval := Approval{ApproverID: command.ActorID}
	if err := tx.QueryRow(ctx, `INSERT INTO config.privacy_mask_approvals (
		request_id, tenant_id, approver_id
	) VALUES ($1::uuid, $2::uuid, $3) RETURNING approved_at`, command.RequestID,
		command.TenantID, command.ActorID).Scan(&approval.ApprovedAt); err != nil {
		return Request{}, fmt.Errorf("write privacy mask approval: %w", err)
	}
	next.Approvals = append(next.Approvals, approval)
	if len(next.Approvals) == 2 {
		var finalizedAt time.Time
		if err := tx.QueryRow(ctx, `UPDATE config.privacy_mask_requests
			SET status = $2, approved_at = clock_timestamp()
			WHERE id = $1::uuid RETURNING approved_at`, command.RequestID,
			StatusApproved).Scan(&finalizedAt); err != nil {
			return Request{}, fmt.Errorf("finalize privacy mask approval: %w", err)
		}
		next.Status = StatusApproved
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID,
		Action: "privacy_mask.approval.recorded", ResourceType: "privacy_mask_request",
		ResourceID: command.RequestID, RequestID: command.AuditRequestID,
		BeforeState: current, AfterState: next, OccurredAt: approval.ApprovedAt,
	}); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("commit privacy mask approval: %w", err)
	}
	return clone(next), nil
}

const privacyMaskRequestSelect = `SELECT id::text, tenant_id::text, site_id::text,
	camera_id::text, name, geometry, status, requested_by, requested_at
	FROM config.privacy_mask_requests`

type rowScanner interface{ Scan(...any) error }

func loadRequest(ctx context.Context, tx pgx.Tx, requestID string, forUpdate bool) (Request, error) {
	query := privacyMaskRequestSelect + " WHERE id = $1::uuid"
	if forUpdate {
		query += " FOR UPDATE"
	}
	return scanRequest(tx.QueryRow(ctx, query, requestID))
}

func loadApprovals(ctx context.Context, tx pgx.Tx, requestID string) ([]Approval, error) {
	rows, err := tx.Query(ctx, `SELECT approver_id, approved_at
		FROM config.privacy_mask_approvals WHERE request_id = $1::uuid
		ORDER BY approved_at, approver_id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	approvals := make([]Approval, 0, 2)
	for rows.Next() {
		var approval Approval
		if err := rows.Scan(&approval.ApproverID, &approval.ApprovedAt); err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return approvals, nil
}

func scanRequest(row rowScanner) (Request, error) {
	var request Request
	if err := row.Scan(&request.ID, &request.TenantID, &request.SiteID, &request.CameraID,
		&request.Name, &request.Geometry, &request.Status, &request.RequestedBy,
		&request.RequestedAt); err != nil {
		return Request{}, err
	}
	return request, nil
}

func (r *PostgresRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres privacy mask repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin privacy mask transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set privacy mask tenant context: %w", err)
	}
	return tx, nil
}

func validateApprove(command ApproveCommand) error {
	if _, err := uuid.Parse(command.RequestID); err != nil {
		return errors.New("privacy mask request identifier must be a UUID")
	}
	if strings.TrimSpace(command.ActorID) == "" || len(strings.TrimSpace(command.ActorID)) > 128 {
		return errors.New("privacy mask approver is required")
	}
	return validateAuditRequestID(command.AuditRequestID)
}

func validateAuditRequestID(requestID string) error {
	parsed, err := uuid.Parse(requestID)
	if err != nil || parsed.Version() != 4 {
		return errors.New("privacy mask audit request identifier must be a UUIDv4")
	}
	return nil
}
