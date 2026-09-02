package usermanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

// ErrOperationUnavailable distinguishes a deliberately unimplemented lifecycle
// transition from an absent adapter. Only invitation intent is durable in this
// slice; no provider call or email is sent here.
var ErrOperationUnavailable = errors.New("user lifecycle operation is not configured")

type postgresPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresAdapter queues invitation intent in the same tenant transaction as
// its audit entry. A separate worker will later deliver it to Supabase Admin.
type PostgresAdapter struct{ pool postgresPool }

func NewPostgresAdapter(pool postgresPool) *PostgresAdapter { return &PostgresAdapter{pool: pool} }

func (a *PostgresAdapter) Invite(ctx context.Context, request InviteRequest) (Invitation, error) {
	if a == nil || a.pool == nil { return Invitation{}, ErrLifecycleUnavailable }
	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil { return Invitation{}, fmt.Errorf("begin lifecycle transaction: %w", err) }
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", request.TenantID); err != nil { return Invitation{}, fmt.Errorf("set lifecycle tenant context: %w", err) }
	payload, err := json.Marshal(map[string]string{"email": strings.TrimSpace(request.Email)})
	if err != nil { return Invitation{}, fmt.Errorf("encode invitation intent: %w", err) }
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO identity.lifecycle_delivery_requests (id, tenant_id, request_id, action, payload, created_by) VALUES ($1::uuid, $2::uuid, $3::uuid, 'invite', $4::jsonb, $5)`, id, request.TenantID, request.RequestID, payload, request.ActorID); err != nil { return Invitation{}, fmt.Errorf("queue invitation intent: %w", err) }
	if _, err := audit.Append(ctx, tx, audit.Event{TenantID: request.TenantID, ActorID: request.ActorID, Action: "identity.invitation.queued", ResourceType: "lifecycle_delivery_request", ResourceID: id, RequestID: request.RequestID, AfterState: map[string]string{"action": "invite", "email": strings.TrimSpace(request.Email)}}); err != nil { return Invitation{}, err }
	if err := tx.Commit(ctx); err != nil { return Invitation{}, fmt.Errorf("commit invitation intent: %w", err) }
	return Invitation{ID: id, Email: strings.TrimSpace(request.Email)}, nil
}

func (*PostgresAdapter) Disable(context.Context, DisableRequest) error { return ErrOperationUnavailable }
func (*PostgresAdapter) Reassign(context.Context, ReassignRequest) error { return ErrOperationUnavailable }
