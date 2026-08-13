// Package audit writes reproducible, append-only audit hash chains.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Event describes one security-relevant state transition.
type Event struct {
	TenantID     string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	BeforeState  any
	AfterState   any
	OccurredAt   time.Time
}

type canonicalEvent struct {
	Version      int    `json:"version"`
	TenantID     string `json:"tenant_id"`
	ActorID      string `json:"actor_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	RequestID    string `json:"request_id"`
	OccurredAt   string `json:"occurred_at"`
	BeforeState  any    `json:"before_state"`
	AfterState   any    `json:"after_state"`
}

// Append locks one tenant-day chain, computes the next hash, and inserts it in
// the caller's transaction. Product state and its audit row therefore commit
// or roll back together.
func Append(ctx context.Context, tx pgx.Tx, event Event) ([]byte, error) {
	if tx == nil {
		return nil, fmt.Errorf("audit transaction is required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	event.OccurredAt = event.OccurredAt.UTC()
	chainDate := event.OccurredAt.Format("2006-01-02")
	chainKey := event.TenantID + ":" + chainDate
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", chainKey); err != nil {
		return nil, fmt.Errorf("lock audit chain: %w", err)
	}

	previousHash := make([]byte, sha256.Size)
	var storedPrevious []byte
	err := tx.QueryRow(ctx, `
		SELECT record_hash
		FROM audit.events
		WHERE tenant_id = $1::uuid AND chain_date = $2::date
		ORDER BY chain_sequence DESC
		LIMIT 1`, event.TenantID, chainDate).Scan(&storedPrevious)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("read audit chain head: %w", err)
	}
	if err == nil {
		copy(previousHash, storedPrevious)
	}

	canonical := canonicalEvent{
		Version: 1, TenantID: event.TenantID, ActorID: event.ActorID,
		Action: event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID,
		RequestID: event.RequestID, OccurredAt: event.OccurredAt.Format(time.RFC3339Nano),
		BeforeState: event.BeforeState, AfterState: event.AfterState,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("canonicalize audit event: %w", err)
	}
	payloadHash := sha256.Sum256(payload)
	hash := sha256.New()
	_, _ = hash.Write(previousHash)
	_, _ = hash.Write(payloadHash[:])
	_, _ = hash.Write([]byte(canonical.OccurredAt))
	recordHash := hash.Sum(nil)

	beforeState, err := json.Marshal(event.BeforeState)
	if err != nil {
		return nil, fmt.Errorf("encode audit before state: %w", err)
	}
	afterState, err := json.Marshal(event.AfterState)
	if err != nil {
		return nil, fmt.Errorf("encode audit after state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit.events (
			event_id, tenant_id, chain_date, occurred_at, actor_id, action,
			resource_type, resource_id, request_id, before_state, after_state,
			canonical_payload, previous_hash, record_hash
		) VALUES (
			$1::uuid, $2::uuid, $3::date, $4, $5, $6,
			$7, $8, $9::uuid, $10::jsonb, $11::jsonb,
			$12::jsonb, $13, $14
		)`, uuid.NewString(), event.TenantID, chainDate, event.OccurredAt,
		event.ActorID, event.Action, event.ResourceType, event.ResourceID,
		event.RequestID, beforeState, afterState, payload, previousHash, recordHash,
	); err != nil {
		return nil, fmt.Errorf("insert audit event: %w", err)
	}
	return recordHash, nil
}
