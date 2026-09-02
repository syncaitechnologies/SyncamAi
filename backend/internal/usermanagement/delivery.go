package usermanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const invitationDeliveryAction = "invite"

var ErrDeliveryLeaseLost = errors.New("lifecycle delivery lease was lost")

// DeliveryRequest is a leased, durable intent. ProviderOperationID is stable
// across retries and must be used by a provider implementation as its
// idempotency key. It contains no credential or bearer token.
type DeliveryRequest struct {
	ID                  string
	TenantID            string
	RequestID           string
	Action              string
	Payload             json.RawMessage
	ProviderOperationID string
}

// DeliveryStore persists the lease state around an externally observable
// provider operation. Implementations must never be callable by a browser.
type DeliveryStore interface {
	Claim(context.Context, string, string, int) ([]DeliveryRequest, error)
	MarkDelivered(context.Context, string, string, string) error
	MarkFailed(context.Context, string, string, string, string) error
}

// InvitationProvider is deliberately provider-neutral. No Supabase Admin
// client is configured or invoked by this package.
type InvitationProvider interface {
	DeliverInvitation(context.Context, DeliveryRequest) error
}

type deliveryPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresDeliveryStore leases requests tenant-by-tenant under the existing
// transaction-local RLS boundary. Expired leases become claimable again.
type PostgresDeliveryStore struct{ pool deliveryPool }

func NewPostgresDeliveryStore(pool deliveryPool) *PostgresDeliveryStore {
	return &PostgresDeliveryStore{pool: pool}
}

func (s *PostgresDeliveryStore) Claim(ctx context.Context, tenantID, workerID string, limit int) ([]DeliveryRequest, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres lifecycle delivery store is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid lifecycle delivery tenant: %w", err)
	}
	if _, err := uuid.Parse(workerID); err != nil {
		return nil, fmt.Errorf("invalid lifecycle delivery worker: %w", err)
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("lifecycle delivery claim limit must be between 1 and 100")
	}

	tx, err := s.begin(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM identity.lifecycle_delivery_requests
			WHERE tenant_id = $1::uuid
			  AND (
				status IN ('pending', 'failed')
				OR (status = 'delivering' AND lease_expires_at <= clock_timestamp())
			  )
			ORDER BY created_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE identity.lifecycle_delivery_requests AS request
		SET status = 'delivering',
			lease_owner = $3::uuid,
			lease_expires_at = clock_timestamp() + interval '60 seconds',
			delivery_attempts = request.delivery_attempts + 1,
			provider_operation_id = COALESCE(request.provider_operation_id, 'lifecycle:' || request.id::text),
			last_error = NULL,
			updated_at = clock_timestamp()
		FROM candidates
		WHERE request.tenant_id = $1::uuid AND request.id = candidates.id
		RETURNING request.id::text, request.tenant_id::text, request.request_id::text,
			request.action, request.payload, request.provider_operation_id`, tenantID, limit, workerID)
	if err != nil {
		return nil, fmt.Errorf("claim lifecycle delivery requests: %w", err)
	}
	defer rows.Close()
	requests := make([]DeliveryRequest, 0)
	for rows.Next() {
		var request DeliveryRequest
		if err := rows.Scan(&request.ID, &request.TenantID, &request.RequestID, &request.Action, &request.Payload, &request.ProviderOperationID); err != nil {
			return nil, fmt.Errorf("scan lifecycle delivery request: %w", err)
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lifecycle delivery requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit lifecycle delivery claim: %w", err)
	}
	return requests, nil
}

func (s *PostgresDeliveryStore) MarkDelivered(ctx context.Context, tenantID, workerID, requestID string) error {
	return s.finish(ctx, tenantID, workerID, requestID, "")
}

func (s *PostgresDeliveryStore) MarkFailed(ctx context.Context, tenantID, workerID, requestID, failure string) error {
	if len(failure) > 2000 {
		failure = failure[:2000]
	}
	if failure == "" {
		failure = "provider delivery failed"
	}
	return s.finish(ctx, tenantID, workerID, requestID, failure)
}

func (s *PostgresDeliveryStore) finish(ctx context.Context, tenantID, workerID, requestID, failure string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres lifecycle delivery store is unavailable")
	}
	if _, err := uuid.Parse(workerID); err != nil {
		return fmt.Errorf("invalid lifecycle delivery worker: %w", err)
	}
	if _, err := uuid.Parse(requestID); err != nil {
		return fmt.Errorf("invalid lifecycle delivery request: %w", err)
	}
	tx, err := s.begin(ctx, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := `UPDATE identity.lifecycle_delivery_requests
		SET status = 'delivered', delivered_at = clock_timestamp(), lease_owner = NULL,
			lease_expires_at = NULL, last_error = NULL, updated_at = clock_timestamp()
		WHERE tenant_id = $1::uuid AND id = $2::uuid AND lease_owner = $3::uuid
			AND status = 'delivering'`
	args := []any{tenantID, requestID, workerID}
	if failure != "" {
		query = `UPDATE identity.lifecycle_delivery_requests
			SET status = 'failed', lease_owner = NULL, lease_expires_at = NULL,
				last_error = $4, updated_at = clock_timestamp()
			WHERE tenant_id = $1::uuid AND id = $2::uuid AND lease_owner = $3::uuid
				AND status = 'delivering'`
		args = append(args, failure)
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("finish lifecycle delivery request: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrDeliveryLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit lifecycle delivery completion: %w", err)
	}
	return nil
}

func (s *PostgresDeliveryStore) begin(ctx context.Context, tenantID string) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, fmt.Errorf("begin lifecycle delivery transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set lifecycle delivery tenant context: %w", err)
	}
	return tx, nil
}

// DeliveryWorker performs only the lease-and-provider boundary. A separate
// deployment supplies the provider; this repository intentionally has none.
type DeliveryWorker struct {
	Store     DeliveryStore
	Provider  InvitationProvider
	WorkerID  string
	BatchSize int
}

type DeliveryResult struct{ Claimed, Delivered, Failed int }

func (w DeliveryWorker) DispatchTenant(ctx context.Context, tenantID string) (DeliveryResult, error) {
	if w.Store == nil || w.Provider == nil {
		return DeliveryResult{}, errors.New("lifecycle delivery worker dependencies are required")
	}
	batchSize := w.BatchSize
	if batchSize == 0 {
		batchSize = 25
	}
	requests, err := w.Store.Claim(ctx, tenantID, w.WorkerID, batchSize)
	if err != nil {
		return DeliveryResult{}, err
	}
	result := DeliveryResult{Claimed: len(requests)}
	var failures []error
	for _, request := range requests {
		if request.Action != invitationDeliveryAction {
			err = fmt.Errorf("unsupported lifecycle delivery action %q", request.Action)
		} else {
			err = w.Provider.DeliverInvitation(ctx, request)
		}
		if err != nil {
			result.Failed++
			failure := safeDeliveryFailure(err)
			if markErr := w.Store.MarkFailed(ctx, tenantID, w.WorkerID, request.ID, failure); markErr != nil {
				failures = append(failures, fmt.Errorf("deliver %s: %v; release lease: %w", request.ID, err, markErr))
			} else {
				failures = append(failures, fmt.Errorf("deliver %s: %w", request.ID, err))
			}
			continue
		}
		if err := w.Store.MarkDelivered(ctx, tenantID, w.WorkerID, request.ID); err != nil {
			result.Failed++
			failures = append(failures, err)
			continue
		}
		result.Delivered++
	}
	return result, errors.Join(failures...)
}

type safeDeliveryError interface {
	error
	SafeDeliveryFailure() string
}

func safeDeliveryFailure(err error) string {
	var safe safeDeliveryError
	if errors.As(err, &safe) && safe.SafeDeliveryFailure() != "" {
		return safe.SafeDeliveryFailure()
	}
	return "provider delivery failed"
}
