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
// transition from an absent adapter. Invitation and disablement intents are
// durable, but no provider call is sent by this transaction boundary.
var ErrOperationUnavailable = errors.New("user lifecycle operation is not configured")
var ErrCannotDisableSelf = errors.New("a user cannot disable their own membership")
var ErrLastActiveSuperAdmin = errors.New("the last active Super Admin cannot be disabled")
var ErrDisableTargetInactive = errors.New("target user is not an active tenant member")
var ErrDisableRequestConflict = errors.New("disable request identifier conflicts with a different target")

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
	var storedID string
	var storedPayload []byte
	var created bool
	if err := tx.QueryRow(ctx, `INSERT INTO identity.lifecycle_delivery_requests (id, tenant_id, request_id, action, payload, created_by) VALUES ($1::uuid, $2::uuid, $3::uuid, 'invite', $4::jsonb, $5) ON CONFLICT (tenant_id, request_id) DO UPDATE SET request_id = EXCLUDED.request_id RETURNING id::text, payload, (xmax = 0)`, id, request.TenantID, request.RequestID, payload, request.ActorID).Scan(&storedID, &storedPayload, &created); err != nil { return Invitation{}, fmt.Errorf("queue invitation intent: %w", err) }
	var stored map[string]string
	if err := json.Unmarshal(storedPayload, &stored); err != nil || stored["email"] != strings.TrimSpace(request.Email) { return Invitation{}, errors.New("invitation request identifier conflicts with a different payload") }
	if created { if _, err := audit.Append(ctx, tx, audit.Event{TenantID: request.TenantID, ActorID: request.ActorID, Action: "identity.invitation.queued", ResourceType: "lifecycle_delivery_request", ResourceID: storedID, RequestID: request.RequestID, AfterState: map[string]string{"action": "invite", "email": strings.TrimSpace(request.Email)}}); err != nil { return Invitation{}, err } }
	if err := tx.Commit(ctx); err != nil { return Invitation{}, fmt.Errorf("commit invitation intent: %w", err) }
	return Invitation{ID: storedID, Email: strings.TrimSpace(request.Email), Queued: true}, nil
}

// Disable immediately suspends only SyncCam's local membership and queues a
// separate provider revocation. It does not claim to invalidate an already
// issued provider access token; only the later provider worker can revoke
// refresh sessions, and access tokens remain valid until their expiry.
func (a *PostgresAdapter) Disable(ctx context.Context, request DisableRequest) error {
	if a == nil || a.pool == nil { return ErrLifecycleUnavailable }
	if request.ActorID == request.UserID { return ErrCannotDisableSelf }
	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil { return fmt.Errorf("begin lifecycle transaction: %w", err) }
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", request.TenantID); err != nil { return fmt.Errorf("set lifecycle tenant context: %w", err) }
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "syncam-user-disable:"+request.TenantID); err != nil { return fmt.Errorf("lock user disablement: %w", err) }

	var existingAction, existingTarget string
	err = tx.QueryRow(ctx, `SELECT action, COALESCE(target_user_id::text, '')
		FROM identity.lifecycle_delivery_requests
		WHERE tenant_id = $1::uuid AND request_id = $2::uuid`, request.TenantID, request.RequestID).Scan(&existingAction, &existingTarget)
	if err == nil {
		if existingAction != "disable" || existingTarget != request.UserID { return ErrDisableRequestConflict }
		if err := tx.Commit(ctx); err != nil { return fmt.Errorf("commit existing disablement request: %w", err) }
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) { return fmt.Errorf("read existing disablement request: %w", err) }

	var roles []string
	err = tx.QueryRow(ctx, `SELECT roles FROM identity.user_tenant_memberships
		WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
		FOR UPDATE`, request.TenantID, request.UserID).Scan(&roles)
	if errors.Is(err, pgx.ErrNoRows) { return ErrDisableTargetInactive }
	if err != nil { return fmt.Errorf("read target membership: %w", err) }
	if containsRole(roles, "super_admin") {
		var activeSuperAdmins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM identity.user_tenant_memberships
			WHERE tenant_id = $1::uuid AND status = 'active' AND roles @> ARRAY['super_admin']::text[]`, request.TenantID).Scan(&activeSuperAdmins); err != nil { return fmt.Errorf("count active Super Admins: %w", err) }
		if activeSuperAdmins <= 1 { return ErrLastActiveSuperAdmin }
	}
	if _, err := tx.Exec(ctx, `UPDATE identity.user_tenant_memberships
		SET status = 'suspended', updated_at = clock_timestamp()
		WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'`, request.TenantID, request.UserID); err != nil { return fmt.Errorf("suspend tenant membership: %w", err) }
	if _, err := tx.Exec(ctx, `UPDATE identity.user_site_memberships
		SET status = 'suspended', updated_at = clock_timestamp()
		WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'`, request.TenantID, request.UserID); err != nil { return fmt.Errorf("suspend site memberships: %w", err) }

	id := uuid.NewString()
	var deliveryID string
	if err := tx.QueryRow(ctx, `INSERT INTO identity.lifecycle_delivery_requests
		(id, tenant_id, request_id, action, target_user_id, payload, created_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'disable', $4::uuid, '{}'::jsonb, $5)
		RETURNING id::text`, id, request.TenantID, request.RequestID, request.UserID, request.ActorID).Scan(&deliveryID); err != nil { return fmt.Errorf("queue user disablement: %w", err) }
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: request.TenantID, ActorID: request.ActorID, Action: "identity.user.disable.queued",
		ResourceType: "user_tenant_membership", ResourceID: request.UserID, RequestID: request.RequestID,
		BeforeState: map[string]string{"status": "active"},
		AfterState: map[string]string{"status": "suspended", "provider_delivery_id": deliveryID, "session_revocation": "queued"},
	}); err != nil { return err }
	if err := tx.Commit(ctx); err != nil { return fmt.Errorf("commit user disablement: %w", err) }
	return nil
}

func containsRole(roles []string, wanted string) bool {
	for _, role := range roles { if role == wanted { return true } }
	return false
}
func (*PostgresAdapter) Reassign(context.Context, ReassignRequest) error { return ErrOperationUnavailable }
