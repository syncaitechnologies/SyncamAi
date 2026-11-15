package livefeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

// Session describes one person starting to watch one camera.
type Session struct {
	TenantID, SiteID, CameraID string
	ActorID, ActorEmail        string
	RequestID                  string
	StartedAt                  time.Time
}

// Auditor records live-view sessions. Viewing must fail closed when the
// record cannot be written.
type Auditor interface {
	LiveViewStarted(ctx context.Context, session Session) error
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresAuditor appends "camera.live_view.started" to the tenant's
// hash-chained audit log.
type PostgresAuditor struct{ pool transactionPool }

func NewPostgresAuditor(pool transactionPool) *PostgresAuditor {
	return &PostgresAuditor{pool: pool}
}

func (a *PostgresAuditor) LiveViewStarted(ctx context.Context, session Session) error {
	if a == nil || a.pool == nil {
		return errors.New("live-view audit is unavailable")
	}
	if _, err := uuid.Parse(session.TenantID); err != nil {
		return fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return fmt.Errorf("begin live-view audit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", session.TenantID); err != nil {
		return fmt.Errorf("set live-view audit tenant: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: session.TenantID, ActorID: session.ActorID, Action: "camera.live_view.started",
		ResourceType: "camera", ResourceID: session.CameraID, RequestID: session.RequestID,
		AfterState: map[string]string{"site_id": session.SiteID, "viewer_email": session.ActorEmail, "mode": "near_live_frames"},
		OccurredAt: session.StartedAt,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit live-view audit: %w", err)
	}
	return nil
}
