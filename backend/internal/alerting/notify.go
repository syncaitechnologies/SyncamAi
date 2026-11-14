package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrEvidenceNotFound is returned by EvidenceRefs when the alert is not
// visible in the tenant.
var ErrEvidenceNotFound = errors.New("alert evidence not found")

// Notifier is told about each newly projected alert after its transaction
// commits. Replayed outbox messages never reach it, so a client receives at
// most one notification per alert. Implementations must not block for long.
type Notifier interface {
	AlertCreated(ctx context.Context, alert Alert, evidenceRefs []string)
}

// NewProjectorWithNotifier builds the alert projector with post-commit
// notifications (for example Telegram photo alerts).
func NewProjectorWithNotifier(pool transactionPool, notifier Notifier) *Projector {
	return &Projector{pool: pool, notifier: notifier}
}

// EvidenceReader returns the evidence references recorded with an alert's
// source event. References are opaque storage keys, never public URLs.
type EvidenceReader interface {
	EvidenceRefs(ctx context.Context, tenantID, alertID string) ([]string, error)
}

func (r *PostgresRepository) EvidenceRefs(ctx context.Context, tenantID, alertID string) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres alert repository is unavailable")
	}
	if _, err := uuid.Parse(alertID); err != nil {
		return nil, ErrEvidenceNotFound
	}
	tx, err := beginTenantTx(ctx, r.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var siteID string
	var raw []byte
	err = tx.QueryRow(ctx, `
		SELECT a.site_id::text, COALESCE(e.evidence_refs, '[]'::jsonb)
		FROM alerts.alerts AS a
		LEFT JOIN events.detection_events AS e
			ON e.tenant_id = a.tenant_id AND e.event_id = a.event_id
		WHERE a.tenant_id = $1::uuid AND a.alert_id = $2::uuid`, tenantID, alertID).Scan(&siteID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEvidenceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read alert evidence: %w", err)
	}
	var refs []string
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("decode alert evidence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit alert evidence read: %w", err)
	}
	return refs, nil
}
