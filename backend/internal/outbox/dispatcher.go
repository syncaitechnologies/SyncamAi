// Package outbox leases durable publish intents and dispatches them safely.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Message struct {
	MessageID    string
	TenantID     string
	Topic        string
	PartitionKey string
	Payload      json.RawMessage
	Headers      json.RawMessage
	OccurredAt   time.Time
}

type Store interface {
	Claim(context.Context, string, string, int) ([]Message, error)
	MarkPublished(context.Context, string, string, string) error
	MarkFailed(context.Context, string, string, string, string) error
}

type Publisher interface {
	Publish(context.Context, Message) error
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct{ pool transactionPool }

func NewPostgresStore(pool transactionPool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Claim(ctx context.Context, tenantID, workerID string, limit int) ([]Message, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres outbox store is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid worker tenant: %w", err)
	}
	if _, err := uuid.Parse(workerID); err != nil {
		return nil, fmt.Errorf("invalid worker identifier: %w", err)
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("outbox claim limit must be between 1 and 100")
	}
	tx, err := beginTenantTx(ctx, s.pool, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		WITH candidates AS (
			SELECT message_id
			FROM messaging.outbox_messages
			WHERE tenant_id = $1::uuid
			  AND published_at IS NULL
			  AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
			ORDER BY created_at, message_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE messaging.outbox_messages AS message
		SET lease_owner = $3::uuid,
			lease_expires_at = clock_timestamp() + interval '60 seconds',
			publish_attempts = publish_attempts + 1,
			last_error = NULL
		FROM candidates
		WHERE message.tenant_id = $1::uuid AND message.message_id = candidates.message_id
		RETURNING message.message_id::text, message.tenant_id::text, message.topic,
			message.partition_key, message.payload, message.headers, message.occurred_at`, tenantID, limit, workerID)
	if err != nil {
		return nil, fmt.Errorf("claim outbox messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.MessageID, &message.TenantID, &message.Topic, &message.PartitionKey, &message.Payload, &message.Headers, &message.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan outbox message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox messages: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
	}
	return messages, nil
}

func (s *PostgresStore) MarkPublished(ctx context.Context, tenantID, workerID, messageID string) error {
	return s.finish(ctx, tenantID, workerID, messageID, "")
}

func (s *PostgresStore) MarkFailed(ctx context.Context, tenantID, workerID, messageID, failure string) error {
	if len(failure) > 2000 {
		failure = failure[:2000]
	}
	return s.finish(ctx, tenantID, workerID, messageID, failure)
}

func (s *PostgresStore) finish(ctx context.Context, tenantID, workerID, messageID, failure string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres outbox store is unavailable")
	}
	if _, err := uuid.Parse(workerID); err != nil {
		return fmt.Errorf("invalid worker identifier: %w", err)
	}
	if _, err := uuid.Parse(messageID); err != nil {
		return fmt.Errorf("invalid outbox message identifier: %w", err)
	}
	tx, err := beginTenantTx(ctx, s.pool, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command := `UPDATE messaging.outbox_messages
		SET published_at = clock_timestamp(), lease_owner = NULL, lease_expires_at = NULL, last_error = NULL
		WHERE tenant_id = $1::uuid AND message_id = $2::uuid AND lease_owner = $3::uuid AND published_at IS NULL`
	args := []any{tenantID, messageID, workerID}
	if failure != "" {
		command = `UPDATE messaging.outbox_messages
			SET lease_owner = NULL, lease_expires_at = NULL, last_error = $4
			WHERE tenant_id = $1::uuid AND message_id = $2::uuid AND lease_owner = $3::uuid AND published_at IS NULL`
		args = append(args, failure)
	}
	tag, err := tx.Exec(ctx, command, args...)
	if err != nil {
		return fmt.Errorf("finish outbox message: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("outbox lease was lost")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox completion: %w", err)
	}
	return nil
}

func beginTenantTx(ctx context.Context, pool transactionPool, tenantID string) (pgx.Tx, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid worker tenant: %w", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, fmt.Errorf("begin outbox transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set outbox tenant context: %w", err)
	}
	return tx, nil
}

type Dispatcher struct {
	Store     Store
	Publisher Publisher
	WorkerID  string
	BatchSize int
}

type Result struct{ Claimed, Published, Failed int }

func (d Dispatcher) DispatchTenant(ctx context.Context, tenantID string) (Result, error) {
	if d.Store == nil || d.Publisher == nil {
		return Result{}, errors.New("outbox dispatcher dependencies are required")
	}
	batchSize := d.BatchSize
	if batchSize == 0 {
		batchSize = 25
	}
	messages, err := d.Store.Claim(ctx, tenantID, d.WorkerID, batchSize)
	if err != nil {
		return Result{}, err
	}
	result := Result{Claimed: len(messages)}
	var failures []error
	for _, message := range messages {
		if err := d.Publisher.Publish(ctx, message); err != nil {
			result.Failed++
			if markErr := d.Store.MarkFailed(ctx, tenantID, d.WorkerID, message.MessageID, err.Error()); markErr != nil {
				failures = append(failures, fmt.Errorf("publish %s: %v; release lease: %w", message.MessageID, err, markErr))
			} else {
				failures = append(failures, fmt.Errorf("publish %s: %w", message.MessageID, err))
			}
			continue
		}
		if err := d.Store.MarkPublished(ctx, tenantID, d.WorkerID, message.MessageID); err != nil {
			result.Failed++
			failures = append(failures, err)
			continue
		}
		result.Published++
	}
	return result, errors.Join(failures...)
}
