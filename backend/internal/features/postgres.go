package features

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresStore keeps plan features in identity.tenant_features and switches
// in config.feature_settings, always under the workspace's row-level
// security.
type PostgresStore struct{ pool transactionPool }

// NewPostgresStore returns a store backed by pool.
func NewPostgresStore(pool transactionPool) *PostgresStore { return &PostgresStore{pool: pool} }

func (p *PostgresStore) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, ErrUnavailable
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin feature settings: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set feature settings tenant: %w", err)
	}
	return tx, nil
}

func (p *PostgresStore) Load(ctx context.Context, tenantID string) (Snapshot, error) {
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot := Snapshot{Included: map[Key]bool{}, Settings: map[Key]Setting{}}
	var minutes *int32
	if err := tx.QueryRow(ctx, `SELECT plan, live_view_minutes FROM identity.tenants WHERE id = $1::uuid`, tenantID).Scan(&snapshot.Plan, &minutes); err != nil {
		return Snapshot{}, fmt.Errorf("read workspace plan: %w", err)
	}
	if minutes != nil {
		value := int(*minutes)
		snapshot.LiveViewMinutes = &value
	}
	rows, err := tx.Query(ctx, `SELECT feature, included FROM identity.tenant_features WHERE tenant_id = $1::uuid`, tenantID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read plan features: %w", err)
	}
	for rows.Next() {
		var key string
		var included bool
		if err := rows.Scan(&key, &included); err != nil {
			rows.Close()
			return Snapshot{}, fmt.Errorf("read plan feature: %w", err)
		}
		snapshot.Included[Key(key)] = included
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("read plan features: %w", err)
	}
	rows, err = tx.Query(ctx, `
		SELECT feature, enabled, camera_ids::text[], updated_at, updated_by
		FROM config.feature_settings WHERE tenant_id = $1::uuid`, tenantID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read feature switches: %w", err)
	}
	for rows.Next() {
		var key string
		var setting Setting
		if err := rows.Scan(&key, &setting.Enabled, &setting.Cameras, &setting.UpdatedAt, &setting.UpdatedBy); err != nil {
			rows.Close()
			return Snapshot{}, fmt.Errorf("read feature switch: %w", err)
		}
		setting.Feature = Key(key)
		snapshot.Settings[setting.Feature] = setting
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("read feature switches: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("commit feature read: %w", err)
	}
	return snapshot, nil
}

func settingState(setting Setting) map[string]any {
	return map[string]any{"enabled": setting.Enabled, "cameras": setting.Cameras}
}

func (p *PostgresStore) Save(ctx context.Context, tenantID string, setting Setting, change Change) error {
	tx, err := p.begin(ctx, tenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO config.feature_settings (tenant_id, feature, enabled, camera_ids, updated_at, updated_by)
		VALUES ($1::uuid, $2, $3, $4::uuid[], $5, $6)
		ON CONFLICT (tenant_id, feature) DO UPDATE
		SET enabled = EXCLUDED.enabled, camera_ids = EXCLUDED.camera_ids,
		    updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		tenantID, string(setting.Feature), setting.Enabled, setting.Cameras, setting.UpdatedAt, setting.UpdatedBy); err != nil {
		return fmt.Errorf("save feature switch: %w", err)
	}
	var before any
	if change.HadBefore {
		before = settingState(change.Before)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: tenantID, ActorID: change.ActorID, Action: "feature.settings.changed",
		ResourceType: "feature", ResourceID: string(setting.Feature), RequestID: change.RequestID,
		BeforeState: before, AfterState: settingState(setting), OccurredAt: setting.UpdatedAt,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit feature switch: %w", err)
	}
	return nil
}

func (p *PostgresStore) Capabilities(ctx context.Context, tenantID string) (map[Key]bool, bool, error) {
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT capabilities FROM platform.site_keys
		WHERE tenant_id = $1::uuid AND revoked_at IS NULL AND capabilities IS NOT NULL`, tenantID)
	if err != nil {
		return nil, false, fmt.Errorf("read site-box capabilities: %w", err)
	}
	result := map[Key]bool{}
	reported := false
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, false, fmt.Errorf("read site-box capabilities: %w", err)
		}
		var keys []string
		if json.Unmarshal(raw, &keys) == nil {
			reported = true
			for _, key := range keys {
				result[Key(key)] = true
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read site-box capabilities: %w", err)
	}
	return result, reported, tx.Commit(ctx)
}

func (p *PostgresStore) Activity(ctx context.Context, tenantID string, since time.Time) (map[string]int, int, error) {
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT event_type, count(*) FROM alerts.alerts
		WHERE tenant_id = $1::uuid AND occurred_at >= $2 GROUP BY event_type`, tenantID, since)
	if err != nil {
		return nil, 0, fmt.Errorf("count alerts: %w", err)
	}
	alerts := map[string]int{}
	for rows.Next() {
		var eventType string
		var count int64
		if err := rows.Scan(&eventType, &count); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("count alerts: %w", err)
		}
		alerts[eventType] = int(count)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("count alerts: %w", err)
	}
	var sightings int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM attendance.records WHERE tenant_id = $1::uuid AND occurred_at >= $2`, tenantID, since).Scan(&sightings); err != nil {
		return nil, 0, fmt.Errorf("count attendance sightings: %w", err)
	}
	return alerts, int(sightings), tx.Commit(ctx)
}

func (p *PostgresStore) CameraExists(ctx context.Context, tenantID, cameraID string) (bool, error) {
	if _, err := uuid.Parse(cameraID); err != nil {
		return false, nil
	}
	tx, err := p.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM config.cameras
		               WHERE tenant_id = $1::uuid AND id = $2::uuid AND lifecycle_status <> 'retired')`,
		tenantID, cameraID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check camera: %w", err)
	}
	return exists, tx.Commit(ctx)
}
