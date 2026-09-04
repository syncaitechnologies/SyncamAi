package usermanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	invitationDeliveryAction  = "invite"
	disablementDeliveryAction = "disable"
)

var ErrDeliveryLeaseLost = errors.New("lifecycle delivery lease was lost")

// DeliveryRequest is a leased, durable intent. ProviderOperationID is stable
// across retries and must be used by a provider implementation as its
// idempotency key. TargetUserID is present only for lifecycle actions that
// already carry a target identity. It contains no credential or bearer token.
type DeliveryRequest struct {
	ID                  string
	TenantID            string
	RequestID           string
	Action              string
	TargetUserID        string
	Payload             json.RawMessage
	ProviderOperationID string
}

// DeliveryStore persists the lease state around an externally observable
// provider operation. Implementations must never be callable by a browser.
type DeliveryStore interface {
	Claim(context.Context, string, string, int, []string) ([]DeliveryRequest, error)
	MarkDelivered(context.Context, string, string, string) error
	MarkFailed(context.Context, string, string, string, string) error
	MarkReconciliationRequired(context.Context, string, string, string, string) error
}

// DeliveryProvider is deliberately provider-neutral. A worker may claim only
// the actions this provider explicitly supports, so one lifecycle adapter can
// never accidentally consume work that belongs to another adapter.
type DeliveryProvider interface {
	DeliveryActions() []string
	Deliver(context.Context, DeliveryRequest) error
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

func (s *PostgresDeliveryStore) Claim(ctx context.Context, tenantID, workerID string, limit int, actions []string) ([]DeliveryRequest, error) {
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
	actions, err := validatedDeliveryActions(actions)
	if err != nil {
		return nil, err
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
			  AND action = ANY($4::text[])
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
			request.action, COALESCE(request.target_user_id::text, ''), request.payload,
			request.provider_operation_id`, tenantID, limit, workerID, actions)
	if err != nil {
		return nil, fmt.Errorf("claim lifecycle delivery requests: %w", err)
	}
	defer rows.Close()
	requests := make([]DeliveryRequest, 0)
	for rows.Next() {
		var request DeliveryRequest
		if err := rows.Scan(&request.ID, &request.TenantID, &request.RequestID, &request.Action, &request.TargetUserID, &request.Payload, &request.ProviderOperationID); err != nil {
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

// MarkReconciliationRequired prevents a provider retry after an ambiguous
// result, such as a timeout after Supabase may already have sent an invite.
func (s *PostgresDeliveryStore) MarkReconciliationRequired(ctx context.Context, tenantID, workerID, requestID, reason string) error {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	if reason == "" {
		reason = "provider delivery requires reconciliation"
	}
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
	tag, err := tx.Exec(ctx, `UPDATE identity.lifecycle_delivery_requests
		SET status = 'reconciliation_required', lease_owner = NULL, lease_expires_at = NULL,
			last_error = $4, updated_at = clock_timestamp()
		WHERE tenant_id = $1::uuid AND id = $2::uuid AND lease_owner = $3::uuid
			AND status = 'delivering'`, tenantID, requestID, workerID, reason)
	if err != nil {
		return fmt.Errorf("hold lifecycle delivery for reconciliation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrDeliveryLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit lifecycle delivery reconciliation hold: %w", err)
	}
	return nil
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
	Provider  DeliveryProvider
	WorkerID  string
	BatchSize int
}

type DeliveryResult struct{ Claimed, Delivered, Failed, ReconciliationRequired int }

func (w DeliveryWorker) DispatchTenant(ctx context.Context, tenantID string) (DeliveryResult, error) {
	if w.Store == nil || w.Provider == nil {
		return DeliveryResult{}, errors.New("lifecycle delivery worker dependencies are required")
	}
	actions, err := validatedDeliveryActions(w.Provider.DeliveryActions())
	if err != nil {
		return DeliveryResult{}, err
	}
	batchSize := w.BatchSize
	if batchSize == 0 {
		batchSize = 25
	}
	requests, err := w.Store.Claim(ctx, tenantID, w.WorkerID, batchSize, actions)
	if err != nil {
		return DeliveryResult{}, err
	}
	result := DeliveryResult{Claimed: len(requests)}
	var failures []error
	for _, request := range requests {
		if err := validateDeliveryRequest(request, actions); err != nil {
			result.Failed++
			failure := safeDeliveryFailure(err)
			if markErr := w.Store.MarkFailed(ctx, tenantID, w.WorkerID, request.ID, failure); markErr != nil {
				failures = append(failures, fmt.Errorf("validate %s: %v; release lease: %w", request.ID, err, markErr))
			} else {
				failures = append(failures, fmt.Errorf("validate %s: %w", request.ID, err))
			}
			continue
		}
		err = w.Provider.Deliver(ctx, request)
		if err != nil {
			var reconciliation reconciliationRequiredError
			if errors.As(err, &reconciliation) {
				result.ReconciliationRequired++
				if markErr := w.Store.MarkReconciliationRequired(ctx, tenantID, w.WorkerID, request.ID, reconciliation.ReconciliationReason()); markErr != nil {
					failures = append(failures, fmt.Errorf("reconcile %s: %v; release lease: %w", request.ID, err, markErr))
				} else {
					failures = append(failures, fmt.Errorf("reconcile %s: %w", request.ID, err))
				}
				continue
			}
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

func validatedDeliveryActions(actions []string) ([]string, error) {
	if len(actions) == 0 {
		return nil, errors.New("lifecycle delivery provider must support at least one action")
	}
	seen := make(map[string]struct{}, len(actions))
	validated := make([]string, 0, len(actions))
	for _, action := range actions {
		if action != invitationDeliveryAction && action != disablementDeliveryAction {
			return nil, fmt.Errorf("unsupported lifecycle delivery action %q", action)
		}
		if _, exists := seen[action]; exists {
			return nil, fmt.Errorf("duplicate lifecycle delivery action %q", action)
		}
		seen[action] = struct{}{}
		validated = append(validated, action)
	}
	return validated, nil
}

// validateDeliveryRequest ensures a provider never sees a request for an
// action it did not claim, or a target identity with invalid action semantics.
func validateDeliveryRequest(request DeliveryRequest, actions []string) error {
	claimed := false
	for _, action := range actions {
		if request.Action == action {
			claimed = true
			break
		}
	}
	if !claimed {
		return errors.New("lifecycle delivery request action was not claimed by provider")
	}
	switch request.Action {
	case invitationDeliveryAction:
		if request.TargetUserID != "" {
			return errors.New("invitation delivery request must not include a target user")
		}
	case disablementDeliveryAction:
		if _, err := uuid.Parse(request.TargetUserID); err != nil {
			return errors.New("disablement delivery request requires a valid target user")
		}
	}
	return nil
}

type safeDeliveryError interface {
	error
	SafeDeliveryFailure() string
}

type reconciliationRequiredError interface {
	error
	ReconciliationReason() string
}

func safeDeliveryFailure(err error) string {
	var safe safeDeliveryError
	if errors.As(err, &safe) && safe.SafeDeliveryFailure() != "" {
		return safe.SafeDeliveryFailure()
	}
	return "provider delivery failed"
}
