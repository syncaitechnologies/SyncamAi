package livefeed

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Live view is metered per workspace (ADR-017): each workspace has a monthly
// allowance of viewing minutes (identity.tenants.live_view_minutes; NULL is
// unlimited, 0 is off), and the API counts viewing time per viewer per camera
// in platform.live_view_usage. Months are calendar months in UTC.
//
// The meter keeps recent usage in memory and writes it to the database every
// flush interval, so usage is accurate to about that interval. It is per
// process, like the hub (one Render instance).

// usageCacheTTL is how long a workspace's stored usage and limit are reused
// before they are read again, so allowance changes apply within a minute.
const usageCacheTTL = time.Minute

// Usage is a workspace's live-view allowance and use for the current month.
type Usage struct {
	// LimitMinutes is nil when live view is unlimited.
	LimitMinutes *int
	Used         time.Duration
	Month        time.Time
}

// Limited reports whether the workspace has a monthly allowance.
func (u Usage) Limited() bool { return u.LimitMinutes != nil }

// Remaining is the viewing time left this month (zero when used up).
func (u Usage) Remaining() time.Duration {
	if u.LimitMinutes == nil {
		return 0
	}
	left := time.Duration(*u.LimitMinutes)*time.Minute - u.Used
	if left < 0 {
		return 0
	}
	return left
}

// Exhausted reports whether a limited workspace has no viewing time left.
func (u Usage) Exhausted() bool { return u.Limited() && u.Remaining() <= 0 }

// ResetsAt is the start of the next month (UTC).
func (u Usage) ResetsAt() time.Time { return u.Month.AddDate(0, 1, 0) }

// UsageStore reads allowances and adds viewing time.
type UsageStore interface {
	Load(ctx context.Context, tenantID string, month time.Time) (limitMinutes *int, usedSeconds int64, err error)
	Add(ctx context.Context, tenantID string, month time.Time, seconds int64) error
}

type usageKey struct {
	tenantID string
	month    time.Time
}

type cachedUsage struct {
	month    time.Time
	limit    *int
	stored   time.Duration
	loadedAt time.Time
}

// Meter enforces and records live-view allowances.
type Meter struct {
	store   UsageStore
	now     func() time.Time
	mu      sync.Mutex
	cache   map[string]cachedUsage
	pending map[usageKey]time.Duration
}

// NewMeter returns a meter backed by store, using the wall clock.
func NewMeter(store UsageStore) *Meter { return NewMeterWithClock(store, time.Now) }

// NewMeterWithClock returns a meter with an injectable clock for tests.
func NewMeterWithClock(store UsageStore, now func() time.Time) *Meter {
	return &Meter{store: store, now: now, cache: map[string]cachedUsage{}, pending: map[usageKey]time.Duration{}}
}

func monthStart(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Usage returns the workspace's allowance and this month's use, including
// viewing time not yet written to the database.
func (m *Meter) Usage(ctx context.Context, tenantID string) (Usage, error) {
	if m == nil || m.store == nil {
		return Usage{}, errors.New("live-view meter is unavailable")
	}
	now := m.now()
	month := monthStart(now)
	m.mu.Lock()
	cached, ok := m.cache[tenantID]
	fresh := ok && cached.month.Equal(month) && now.Sub(cached.loadedAt) < usageCacheTTL
	m.mu.Unlock()
	if !fresh {
		limit, used, err := m.store.Load(ctx, tenantID, month)
		if err != nil {
			return Usage{}, err
		}
		cached = cachedUsage{month: month, limit: limit, stored: time.Duration(used) * time.Second, loadedAt: now}
		m.mu.Lock()
		m.cache[tenantID] = cached
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return Usage{LimitMinutes: cached.limit, Used: cached.stored + m.pending[usageKey{tenantID, month}], Month: month}, nil
}

// Charge adds viewing time and returns the updated usage.
func (m *Meter) Charge(ctx context.Context, tenantID string, viewed time.Duration) (Usage, error) {
	if viewed > 0 && m != nil {
		m.mu.Lock()
		m.pending[usageKey{tenantID, monthStart(m.now())}] += viewed
		m.mu.Unlock()
	}
	return m.Usage(ctx, tenantID)
}

// Flush writes viewing time counted since the last flush. Time that could not
// be written is kept and retried on the next flush.
func (m *Meter) Flush(ctx context.Context) error {
	if m == nil || m.store == nil {
		return nil
	}
	m.mu.Lock()
	batch := m.pending
	m.pending = map[usageKey]time.Duration{}
	m.mu.Unlock()
	var failed error
	for key, viewed := range batch {
		seconds := int64(viewed / time.Second)
		remainder := viewed - time.Duration(seconds)*time.Second
		if seconds > 0 {
			if err := m.store.Add(ctx, key.tenantID, key.month, seconds); err != nil {
				failed = errors.Join(failed, err)
				remainder = viewed
				seconds = 0
			}
		}
		m.mu.Lock()
		if remainder > 0 {
			m.pending[key] += remainder
		}
		if seconds > 0 {
			if cached, ok := m.cache[key.tenantID]; ok && cached.month.Equal(key.month) {
				cached.stored += time.Duration(seconds) * time.Second
				m.cache[key.tenantID] = cached
			}
		}
		m.mu.Unlock()
	}
	return failed
}

// Run flushes every interval until ctx ends, then flushes once more.
func (m *Meter) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.Flush(final); err != nil {
				log.Printf("live-view usage could not be saved at shutdown: %v", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := m.Flush(ctx); err != nil {
				log.Printf("live-view usage could not be saved; retrying: %v", err)
			}
		}
	}
}

// PostgresUsage stores allowances in identity.tenants and usage in
// platform.live_view_usage, both under the tenant's row-level security.
type PostgresUsage struct{ pool transactionPool }

func NewPostgresUsage(pool transactionPool) *PostgresUsage { return &PostgresUsage{pool: pool} }

func (p *PostgresUsage) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, errors.New("live-view usage store is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin live-view usage: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set live-view usage tenant: %w", err)
	}
	return tx, nil
}

func (p *PostgresUsage) Load(ctx context.Context, tenantID string, month time.Time) (*int, int64, error) {
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var limit *int32
	var used int64
	err = tx.QueryRow(ctx, `
		SELECT t.live_view_minutes,
		       COALESCE((SELECT u.seconds_used FROM platform.live_view_usage u
		                 WHERE u.tenant_id = t.id AND u.month = $2::date), 0)
		FROM identity.tenants t WHERE t.id = $1::uuid`, tenantID, month.Format("2006-01-02")).Scan(&limit, &used)
	if err != nil {
		return nil, 0, fmt.Errorf("read live-view usage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit live-view usage read: %w", err)
	}
	if limit == nil {
		return nil, used, nil
	}
	minutes := int(*limit)
	return &minutes, used, nil
}

func (p *PostgresUsage) Add(ctx context.Context, tenantID string, month time.Time, seconds int64) error {
	tx, err := p.begin(ctx, tenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.live_view_usage (tenant_id, month, seconds_used)
		VALUES ($1::uuid, $2::date, $3)
		ON CONFLICT (tenant_id, month) DO UPDATE
		SET seconds_used = platform.live_view_usage.seconds_used + EXCLUDED.seconds_used,
		    updated_at = clock_timestamp()`, tenantID, month.Format("2006-01-02"), seconds); err != nil {
		return fmt.Errorf("add live-view usage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit live-view usage: %w", err)
	}
	return nil
}
