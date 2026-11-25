// Package retention permanently deletes alerts six months after they were
// raised, together with their snapshots, action history and source events.
// The six-month window is enforced by the database functions, so this job can
// never delete anything younger.
package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

// DefaultBatchSize is how many expired alerts are handled per database call.
const DefaultBatchSize = 200

// maxBatchesPerRun bounds one run (200 x 50 = 10,000 alerts); anything left
// over is picked up by the next daily run.
const maxBatchesPerRun = 50

// ExpiredAlert is an alert past the retention window and its evidence references.
type ExpiredAlert struct {
	AlertID      string
	TenantID     string
	EvidenceRefs []string
}

// Store lists and deletes expired alerts across all workspaces.
type Store interface {
	Expired(ctx context.Context, limit int) ([]ExpiredAlert, error)
	Purge(ctx context.Context, alertIDs []string) (int, error)
}

// EvidenceDeleter removes a stored snapshot.
type EvidenceDeleter interface {
	Delete(ctx context.Context, tenantID, ref string) error
}

// Purger deletes expired alerts. Snapshot files are deleted first; an alert
// whose snapshot could not be deleted is kept and retried on the next run, so
// a file is never left without the record that points at it.
type Purger struct {
	Store     Store
	Evidence  EvidenceDeleter // nil when snapshot storage is not configured
	BatchSize int
	Logf      func(string, ...any)
}

func (p *Purger) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}

// Run purges shortly after start-up and then every interval until ctx ends.
func (p *Purger) Run(ctx context.Context, firstDelay, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(firstDelay):
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if removed, err := p.RunOnce(ctx); err != nil {
			p.logf("alert retention: %v (deleted %d alerts before the error)", err, removed)
		} else if removed > 0 {
			p.logf("alert retention: deleted %d alerts older than six months", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce deletes expired alerts in batches and returns how many were deleted.
func (p *Purger) RunOnce(ctx context.Context) (int, error) {
	if p == nil || p.Store == nil {
		return 0, errors.New("alert retention store is unavailable")
	}
	size := p.BatchSize
	if size <= 0 {
		size = DefaultBatchSize
	}
	total := 0
	for batch := 0; batch < maxBatchesPerRun; batch++ {
		expired, err := p.Store.Expired(ctx, size)
		if err != nil {
			return total, err
		}
		if len(expired) == 0 {
			return total, nil
		}
		ready := make([]string, 0, len(expired))
		for _, alert := range expired {
			if p.deleteEvidence(ctx, alert) {
				ready = append(ready, alert.AlertID)
			}
		}
		if len(ready) == 0 {
			// Every snapshot in this batch failed; stop and retry tomorrow.
			return total, errors.New("no snapshots in the oldest batch could be deleted")
		}
		removed, err := p.Store.Purge(ctx, ready)
		total += removed
		if err != nil {
			return total, err
		}
		if len(expired) < size || len(ready) < len(expired) {
			return total, nil
		}
	}
	return total, nil
}

func (p *Purger) deleteEvidence(ctx context.Context, alert ExpiredAlert) bool {
	for _, ref := range alert.EvidenceRefs {
		if !strings.HasPrefix(ref, siteingest.SnapshotRefPrefix) || p.Evidence == nil {
			continue
		}
		err := p.Evidence.Delete(ctx, alert.TenantID, ref)
		if err != nil && !errors.Is(err, siteingest.ErrEvidenceNotFound) {
			p.logf("alert retention: keeping alert %s, snapshot delete failed: %v", alert.AlertID, err)
			return false
		}
	}
	return true
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresStore calls alerts.expired_alert_evidence and alerts.purge_expired_alerts.
type PostgresStore struct{ pool transactionPool }

func NewPostgresStore(pool transactionPool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Expired(ctx context.Context, limit int) ([]ExpiredAlert, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("alert retention store is unavailable")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin expired alert read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT alert_id::text, tenant_id::text, evidence_refs
		FROM alerts.expired_alert_evidence($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired alerts: %w", err)
	}
	expired := []ExpiredAlert{}
	for rows.Next() {
		var alert ExpiredAlert
		if err := rows.Scan(&alert.AlertID, &alert.TenantID, &alert.EvidenceRefs); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read expired alert: %w", err)
		}
		expired = append(expired, alert)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read expired alerts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit expired alert read: %w", err)
	}
	return expired, nil
}

func (s *PostgresStore) Purge(ctx context.Context, alertIDs []string) (int, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("alert retention store is unavailable")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return 0, fmt.Errorf("begin alert purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var removed int
	if err := tx.QueryRow(ctx, `SELECT alerts.purge_expired_alerts($1::uuid[])`, alertIDs).Scan(&removed); err != nil {
		return 0, fmt.Errorf("purge expired alerts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit alert purge: %w", err)
	}
	return removed, nil
}
