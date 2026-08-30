// Package realtime provides the durable five-minute replay boundary used by
// the composite Phase 1 WebSocket gateway.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	TopicAlertCreated = "alerts.created"
	TopicAlertState   = "alerts.state"
	ReplayLimit       = 100
	MaxReplayWindow   = 8192
)

type Event struct {
	TenantID string
	SiteID   string
	Topic    string
	Payload  any
}

type Message struct {
	TenantID string          `json:"-"`
	SiteID   string          `json:"-"`
	Sequence int64           `json:"seq"`
	Topic    string          `json:"topic"`
	Payload  json.RawMessage `json:"payload"`
	Created  time.Time       `json:"ts"`
}

type Replay struct {
	Messages        []Message
	CurrentSequence int64
	Gap             bool
}

type Repository interface {
	Current(context.Context, string, string) (int64, error)
	Replay(context.Context, string, string, int64) (Replay, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Append stores a realtime envelope in the caller's transaction so the state
// transition and its notification can never diverge.
func Append(ctx context.Context, tx pgx.Tx, event Event) (int64, error) {
	if tx == nil {
		return 0, errors.New("realtime transaction is required")
	}
	if _, err := uuid.Parse(event.TenantID); err != nil {
		return 0, fmt.Errorf("invalid realtime tenant: %w", err)
	}
	if _, err := uuid.Parse(event.SiteID); err != nil {
		return 0, fmt.Errorf("invalid realtime site: %w", err)
	}
	if event.Topic != TopicAlertCreated && event.Topic != TopicAlertState {
		return 0, fmt.Errorf("unsupported realtime topic %q", event.Topic)
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return 0, fmt.Errorf("encode realtime payload: %w", err)
	}
	var sequence int64
	err = tx.QueryRow(ctx, `
		INSERT INTO syncam_realtime.site_sequences (tenant_id, site_id, last_sequence)
		VALUES ($1::uuid, $2::uuid, 1)
		ON CONFLICT (tenant_id, site_id) DO UPDATE
		SET last_sequence = syncam_realtime.site_sequences.last_sequence + 1,
			updated_at = clock_timestamp()
		RETURNING last_sequence`, event.TenantID, event.SiteID).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("allocate realtime sequence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO syncam_realtime.messages (tenant_id, site_id, sequence, topic, payload)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::jsonb)`,
		event.TenantID, event.SiteID, sequence, event.Topic, payload,
	); err != nil {
		return 0, fmt.Errorf("append realtime message: %w", err)
	}
	return sequence, nil
}

func (r *PostgresRepository) Current(ctx context.Context, tenantID, siteID string) (int64, error) {
	tx, err := r.begin(ctx, tenantID, siteID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sequence int64
	err = tx.QueryRow(ctx, `
		SELECT last_sequence
		FROM syncam_realtime.site_sequences
		WHERE tenant_id = $1::uuid AND site_id = $2::uuid`, tenantID, siteID).Scan(&sequence)
	if err == pgx.ErrNoRows {
		sequence = 0
	} else if err != nil {
		return 0, fmt.Errorf("read realtime sequence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit realtime sequence read: %w", err)
	}
	return sequence, nil
}

func (r *PostgresRepository) Replay(ctx context.Context, tenantID, siteID string, after int64) (Replay, error) {
	if after < 0 {
		return Replay{}, errors.New("realtime sequence cannot be negative")
	}
	tx, err := r.begin(ctx, tenantID, siteID)
	if err != nil {
		return Replay{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := Replay{Messages: make([]Message, 0)}
	err = tx.QueryRow(ctx, `
		SELECT last_sequence
		FROM syncam_realtime.site_sequences
		WHERE tenant_id = $1::uuid AND site_id = $2::uuid`, tenantID, siteID).Scan(&result.CurrentSequence)
	if err == pgx.ErrNoRows {
		result.CurrentSequence = 0
	} else if err != nil {
		return Replay{}, fmt.Errorf("read realtime sequence: %w", err)
	}
	if after > result.CurrentSequence {
		return Replay{}, errors.New("realtime resume sequence is ahead of the stream")
	}
	var oldest int64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(min(sequence), 0)
		FROM syncam_realtime.messages
		WHERE tenant_id = $1::uuid AND site_id = $2::uuid
			AND expires_at > clock_timestamp()`, tenantID, siteID).Scan(&oldest)
	if err != nil {
		return Replay{}, fmt.Errorf("read realtime replay floor: %w", err)
	}
	if after < result.CurrentSequence && (oldest == 0 || after < oldest-1) {
		result.Gap = true
	} else if result.CurrentSequence-after > MaxReplayWindow {
		result.Gap = true
	}
	if !result.Gap {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id::text, site_id::text, sequence, topic, payload, created_at
			FROM syncam_realtime.messages
			WHERE tenant_id = $1::uuid AND site_id = $2::uuid
				AND sequence > $3 AND expires_at > clock_timestamp()
			ORDER BY sequence
			LIMIT $4`, tenantID, siteID, after, ReplayLimit)
		if err != nil {
			return Replay{}, fmt.Errorf("replay realtime messages: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var message Message
			if err := rows.Scan(&message.TenantID, &message.SiteID, &message.Sequence, &message.Topic, &message.Payload, &message.Created); err != nil {
				return Replay{}, fmt.Errorf("scan realtime message: %w", err)
			}
			result.Messages = append(result.Messages, message)
		}
		if err := rows.Err(); err != nil {
			return Replay{}, fmt.Errorf("iterate realtime messages: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Replay{}, fmt.Errorf("commit realtime replay: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) begin(ctx context.Context, tenantID, siteID string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres realtime repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid realtime tenant: %w", err)
	}
	if _, err := uuid.Parse(siteID); err != nil {
		return nil, fmt.Errorf("invalid realtime site: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin realtime transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set realtime tenant context: %w", err)
	}
	return tx, nil
}
