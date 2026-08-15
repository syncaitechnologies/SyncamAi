// Package eventing persists normalized tenant events and their publish intent.
package eventing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

var (
	ErrDedupeConflict = errors.New("dedupe key reused with a different event")
	ErrEventConflict  = errors.New("event identifier already exists")
	ErrSiteNotFound   = errors.New("event site not found")
)

const OutboxTopic = "detection-events-v1"

type DetectionEvent struct {
	EventID             string    `json:"event_id"`
	TenantID            string    `json:"tenant_id"`
	DedupeKey           string    `json:"dedupe_key"`
	OccurredAt          time.Time `json:"occurred_at"`
	SiteID              string    `json:"site_id"`
	CameraID            string    `json:"camera_id"`
	ZoneID              string    `json:"zone_id"`
	EventType           string    `json:"event_type"`
	ModelVersion        string    `json:"model_version"`
	Confidence          float64   `json:"confidence"`
	EvidenceRefs        []string  `json:"evidence_refs"`
	RequiresHumanReview bool      `json:"requires_human_review"`
	ReviewState         string    `json:"review_state"`
	ObservedBehavior    string    `json:"observed_behavior,omitempty"`
	SubjectClass        string    `json:"subject_class,omitempty"`
}

type IngestCommand struct {
	ActorID   string
	RequestID string
	Event     DetectionEvent
}

type IngestResult struct {
	EventID  string `json:"event_id"`
	Accepted bool   `json:"accepted"`
	Replayed bool   `json:"-"`
}

type Repository interface {
	Ingest(context.Context, IngestCommand) (IngestResult, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Ingest commits the event, one outbox message, and one audit event atomically.
func (r *PostgresRepository) Ingest(ctx context.Context, command IngestCommand) (IngestResult, error) {
	if r == nil || r.pool == nil {
		return IngestResult{}, fmt.Errorf("postgres event repository is unavailable")
	}
	event := normalizeEvent(command.Event)
	if _, err := uuid.Parse(event.TenantID); err != nil {
		return IngestResult{}, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return IngestResult{}, fmt.Errorf("begin event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", event.TenantID); err != nil {
		return IngestResult{}, fmt.Errorf("set event tenant context: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", event.TenantID+":"+event.DedupeKey); err != nil {
		return IngestResult{}, fmt.Errorf("lock event dedupe key: %w", err)
	}

	payload, requestHash, err := canonicalPayload(event)
	if err != nil {
		return IngestResult{}, err
	}
	var storedID, storedHash string
	err = tx.QueryRow(ctx, `
		SELECT event_id::text, request_hash
		FROM events.detection_events
		WHERE tenant_id = $1::uuid AND dedupe_key = $2`, event.TenantID, event.DedupeKey,
	).Scan(&storedID, &storedHash)
	if err == nil {
		if storedHash != requestHash {
			return IngestResult{}, ErrDedupeConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return IngestResult{}, fmt.Errorf("commit event replay: %w", err)
		}
		return IngestResult{EventID: storedID, Accepted: true, Replayed: true}, nil
	}
	if err != pgx.ErrNoRows {
		return IngestResult{}, fmt.Errorf("read event dedupe key: %w", err)
	}

	evidence, err := json.Marshal(event.EvidenceRefs)
	if err != nil {
		return IngestResult{}, fmt.Errorf("encode evidence references: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO events.detection_events (
			event_id, tenant_id, dedupe_key, request_hash, occurred_at, site_id,
			camera_id, zone_id, event_type, model_version, confidence, evidence_refs,
			requires_human_review, review_state, payload
		) VALUES (
			$1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
			$7::uuid, $8::uuid, $9, $10, $11, $12::jsonb,
			$13, $14, $15::jsonb
		)`, event.EventID, event.TenantID, event.DedupeKey, requestHash, event.OccurredAt,
		event.SiteID, event.CameraID, event.ZoneID, event.EventType, event.ModelVersion,
		event.Confidence, evidence, event.RequiresHumanReview, event.ReviewState, payload,
	); err != nil {
		return IngestResult{}, classifyInsertError(err)
	}
	headers, err := json.Marshal(map[string]string{"event_version": "1", "content_type": "application/avro+json"})
	if err != nil {
		return IngestResult{}, fmt.Errorf("encode outbox headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messaging.outbox_messages (
			message_id, tenant_id, aggregate_type, aggregate_id, topic,
			partition_key, payload, headers, occurred_at
		) VALUES ($1::uuid, $2::uuid, 'detection_event', $3::uuid, $4, $5, $6::jsonb, $7::jsonb, $8)`,
		uuid.NewString(), event.TenantID, event.EventID, OutboxTopic,
		event.TenantID+":"+event.CameraID, payload, headers, event.OccurredAt,
	); err != nil {
		return IngestResult{}, fmt.Errorf("store event outbox message: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: event.TenantID, ActorID: command.ActorID, Action: "event.accepted",
		ResourceType: "detection_event", ResourceID: event.EventID, RequestID: command.RequestID,
		AfterState: event, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return IngestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, fmt.Errorf("commit event ingestion: %w", err)
	}
	return IngestResult{EventID: event.EventID, Accepted: true}, nil
}

func canonicalPayload(event DetectionEvent) ([]byte, string, error) {
	event = normalizeEvent(event)
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, "", fmt.Errorf("encode canonical event: %w", err)
	}
	hash := sha256.Sum256(payload)
	return payload, hex.EncodeToString(hash[:]), nil
}

func normalizeEvent(event DetectionEvent) DetectionEvent {
	event.EventID = strings.TrimSpace(event.EventID)
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.DedupeKey = strings.TrimSpace(event.DedupeKey)
	event.SiteID = strings.TrimSpace(event.SiteID)
	event.CameraID = strings.TrimSpace(event.CameraID)
	event.ZoneID = strings.TrimSpace(event.ZoneID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.ModelVersion = strings.TrimSpace(event.ModelVersion)
	event.ObservedBehavior = strings.TrimSpace(event.ObservedBehavior)
	event.SubjectClass = strings.TrimSpace(strings.ToLower(event.SubjectClass))
	event.OccurredAt = event.OccurredAt.UTC()
	if event.EvidenceRefs == nil {
		event.EvidenceRefs = []string{}
	}
	for index := range event.EvidenceRefs {
		event.EvidenceRefs[index] = strings.TrimSpace(event.EvidenceRefs[index])
	}
	return event
}

func classifyInsertError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrSiteNotFound
		case "23505":
			return ErrEventConflict
		}
	}
	return fmt.Errorf("insert detection event: %w", err)
}

type memoryReplay struct {
	hash    string
	eventID string
}

type MemoryRepository struct {
	mu      sync.Mutex
	replays map[string]memoryReplay
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{replays: make(map[string]memoryReplay)}
}

func (r *MemoryRepository) Ingest(_ context.Context, command IngestCommand) (IngestResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event := normalizeEvent(command.Event)
	_, hash, err := canonicalPayload(event)
	if err != nil {
		return IngestResult{}, err
	}
	key := event.TenantID + ":" + event.DedupeKey
	if stored, ok := r.replays[key]; ok {
		if stored.hash != hash {
			return IngestResult{}, ErrDedupeConflict
		}
		return IngestResult{EventID: stored.eventID, Accepted: true, Replayed: true}, nil
	}
	r.replays[key] = memoryReplay{hash: hash, eventID: event.EventID}
	return IngestResult{EventID: event.EventID, Accepted: true}, nil
}
