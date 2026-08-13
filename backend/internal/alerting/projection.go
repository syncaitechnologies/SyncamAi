// Package alerting builds the tenant-scoped operator queue from accepted events.
package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
)

const ConsumerName = "alert-projector-v1"

type Alert struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	EventID    string     `json:"event_id"`
	SiteID     string     `json:"site_id"`
	CameraID   string     `json:"camera_id"`
	ZoneID     string     `json:"zone_id"`
	EventType  string     `json:"event_type"`
	Severity   string     `json:"severity"`
	Status     string     `json:"status"`
	Confidence float64    `json:"confidence"`
	OccurredAt time.Time  `json:"occurred_at"`
	CreatedAt  time.Time  `json:"created_at"`
	AckedAt    *time.Time `json:"acked_at,omitempty"`
	AckedBy    string     `json:"acked_by,omitempty"`
}

type Repository interface {
	List(context.Context, string) ([]Alert, error)
	ListSite(context.Context, string, string) ([]Alert, error)
	Get(context.Context, string, string) (Alert, error)
	Acknowledge(context.Context, AcknowledgeCommand) (AcknowledgeResult, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) List(ctx context.Context, tenantID string) ([]Alert, error) {
	return r.list(ctx, tenantID, "")
}

func (r *PostgresRepository) ListSite(ctx context.Context, tenantID, siteID string) ([]Alert, error) {
	if _, err := uuid.Parse(siteID); err != nil {
		return nil, fmt.Errorf("invalid alert site: %w", err)
	}
	return r.list(ctx, tenantID, siteID)
}

func (r *PostgresRepository) list(ctx context.Context, tenantID, siteID string) ([]Alert, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres alert repository is unavailable")
	}
	tx, err := beginTenantTx(ctx, r.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := `
		SELECT alert_id::text, tenant_id::text, event_id::text, site_id::text,
			camera_id::text, zone_id::text, event_type, severity, status,
			confidence, occurred_at, created_at, acked_at, COALESCE(acked_by, '')
		FROM alerts.alerts
		WHERE ($1::uuid IS NULL OR site_id = $1::uuid)
		ORDER BY priority DESC, occurred_at, alert_id
		LIMIT 100`
	var siteArgument any
	if siteID != "" {
		siteArgument = siteID
	}
	rows, err := tx.Query(ctx, query, siteArgument)
	if err != nil {
		return nil, fmt.Errorf("list alert queue: %w", err)
	}
	defer rows.Close()
	alerts := make([]Alert, 0)
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert queue: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert queue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit alert queue read: %w", err)
	}
	return alerts, nil
}

type Projector struct{ pool transactionPool }

func NewProjector(pool transactionPool) *Projector { return &Projector{pool: pool} }

// Publish implements outbox.Publisher with an idempotent local alert projection.
func (p *Projector) Publish(ctx context.Context, message outbox.Message) error {
	if p == nil || p.pool == nil {
		return errors.New("alert projector is unavailable")
	}
	if message.Topic != eventing.OutboxTopic {
		return fmt.Errorf("unsupported outbox topic %q", message.Topic)
	}
	var event eventing.DetectionEvent
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return fmt.Errorf("decode detection event: %w", err)
	}
	if event.TenantID != message.TenantID {
		return errors.New("outbox and event tenant mismatch")
	}
	tx, err := beginTenantTx(ctx, p.pool, message.TenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var receiptID string
	err = tx.QueryRow(ctx, `
		INSERT INTO messaging.consumer_receipts (tenant_id, consumer_name, message_id)
		VALUES ($1::uuid, $2, $3::uuid)
		ON CONFLICT DO NOTHING
		RETURNING message_id::text`, message.TenantID, ConsumerName, message.MessageID).Scan(&receiptID)
	if err == pgx.ErrNoRows {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("record alert consumer receipt: %w", err)
	}
	severity, priority := classify(event.EventType)
	alertID := uuid.NewString()
	createdAt := time.Now().UTC()
	alert := Alert{
		ID: alertID, TenantID: event.TenantID, EventID: event.EventID,
		SiteID: event.SiteID, CameraID: event.CameraID, ZoneID: event.ZoneID,
		EventType: event.EventType, Severity: severity, Status: "unacknowledged",
		Confidence: event.Confidence, OccurredAt: event.OccurredAt, CreatedAt: createdAt,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO alerts.alerts (
			alert_id, tenant_id, event_id, site_id, camera_id, zone_id,
			event_type, severity, status, confidence, priority, occurred_at, created_at
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid,
			$7, $8, 'unacknowledged', $9, $10, $11, $12)`,
		alert.ID, alert.TenantID, alert.EventID, alert.SiteID, alert.CameraID,
		alert.ZoneID, alert.EventType, alert.Severity, alert.Confidence, priority,
		alert.OccurredAt, alert.CreatedAt,
	); err != nil {
		return fmt.Errorf("project alert: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: alert.TenantID, ActorID: "system:alert-projector", Action: "alert.created",
		ResourceType: "alert", ResourceID: alert.ID, RequestID: message.MessageID,
		AfterState: alert, OccurredAt: alert.CreatedAt,
	}); err != nil {
		return fmt.Errorf("audit alert projection: %w", err)
	}
	if _, err := realtime.Append(ctx, tx, realtime.Event{
		TenantID: alert.TenantID, SiteID: alert.SiteID,
		Topic: realtime.TopicAlertCreated, Payload: map[string]any{"alert": alert},
	}); err != nil {
		return fmt.Errorf("publish alert projection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit alert projection: %w", err)
	}
	return nil
}

func beginTenantTx(ctx context.Context, pool transactionPool, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid alert tenant: %w", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin alert transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set alert tenant context: %w", err)
	}
	return tx, nil
}

func classify(eventType string) (string, int) {
	switch eventType {
	case "weapon_review", "fire_review", "smoke_review", "fall_review", "fight_review":
		return "critical", 500
	case "intrusion", "restricted_zone", "abandoned_object_review":
		return "high", 400
	case "loitering", "vehicle_activity", "ppe_review", "camera_health":
		return "medium", 300
	case "attendance_review":
		return "info", 100
	default:
		return "low", 200
	}
}

type MemoryRepository struct {
	Alerts []Alert
}

func (r *MemoryRepository) List(_ context.Context, tenantID string) ([]Alert, error) {
	if r == nil {
		return nil, errors.New("memory alert repository is unavailable")
	}
	result := make([]Alert, 0)
	for _, alert := range r.Alerts {
		if alert.TenantID == tenantID {
			result = append(result, alert)
		}
	}
	return result, nil
}

func (r *MemoryRepository) ListSite(_ context.Context, tenantID, siteID string) ([]Alert, error) {
	if r == nil {
		return nil, errors.New("memory alert repository is unavailable")
	}
	result := make([]Alert, 0)
	for _, alert := range r.Alerts {
		if alert.TenantID == tenantID && alert.SiteID == siteID {
			result = append(result, alert)
		}
	}
	return result, nil
}
