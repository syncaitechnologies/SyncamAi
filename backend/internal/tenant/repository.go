// Package tenant defines tenant-scoped domain resources and persistence ports.
package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

var (
	// ErrIdempotencyConflict means a key was reused with a different payload.
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	// ErrSiteConflict means the requested site conflicts with tenant state.
	ErrSiteConflict = errors.New("site conflicts with existing tenant state")
	// ErrTenantNotFound means the verified tenant has not been provisioned.
	ErrTenantNotFound = errors.New("tenant not found")
)

// Site is a facility owned by exactly one tenant.
type Site struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	Address   string    `json:"address,omitempty"`
	Timezone  string    `json:"timezone"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateSiteCommand carries verified identity and replay boundaries into one transaction.
type CreateSiteCommand struct {
	TenantID       string
	ActorID        string
	RequestID      string
	IdempotencyKey string
	Name           string
	Address        string
	Timezone       string
}

// CreateSiteResult reports whether the exact stored response was replayed.
type CreateSiteResult struct {
	Site     Site
	Replayed bool
}

// Repository always receives the verified tenant claim as its partition key.
type Repository interface {
	ListSites(context.Context, string) ([]Site, error)
	CreateSite(context.Context, CreateSiteCommand) (CreateSiteResult, error)
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresRepository enforces tenant context in transaction-local RLS settings.
type PostgresRepository struct {
	pool transactionPool
}

// NewPostgresRepository binds the tenant repository to a non-superuser pool.
func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ListSites returns only rows visible under the transaction's verified tenant setting.
func (r *PostgresRepository) ListSites(ctx context.Context, tenantID string) ([]Site, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("postgres tenant repository is unavailable")
	}
	tx, err := beginTenantTx(ctx, r.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id::text, tenant_id::text, name, COALESCE(address, ''), timezone, status, created_at
		FROM config.sites
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenant sites: %w", err)
	}
	defer rows.Close()
	result := make([]Site, 0)
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.TenantID, &site.Name, &site.Address, &site.Timezone, &site.Status, &site.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant site: %w", err)
		}
		result = append(result, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant sites: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit site list: %w", err)
	}
	return result, nil
}

// CreateSite atomically persists the site, exact replay response, and audit event.
func (r *PostgresRepository) CreateSite(ctx context.Context, command CreateSiteCommand) (CreateSiteResult, error) {
	if r == nil || r.pool == nil {
		return CreateSiteResult{}, fmt.Errorf("postgres tenant repository is unavailable")
	}
	tx, err := beginTenantTx(ctx, r.pool, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return CreateSiteResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := command.TenantID + ":" + command.IdempotencyKey
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return CreateSiteResult{}, fmt.Errorf("lock idempotency key: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM platform.idempotency_keys
		WHERE tenant_id = $1::uuid AND idempotency_key = $2 AND expires_at <= clock_timestamp()`,
		command.TenantID, command.IdempotencyKey,
	); err != nil {
		return CreateSiteResult{}, fmt.Errorf("expire idempotency key: %w", err)
	}

	requestHash, err := hashCreateSite(command)
	if err != nil {
		return CreateSiteResult{}, err
	}
	var storedHash string
	var storedResponse []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response_body
		FROM platform.idempotency_keys
		WHERE tenant_id = $1::uuid AND idempotency_key = $2`,
		command.TenantID, command.IdempotencyKey,
	).Scan(&storedHash, &storedResponse)
	if err == nil {
		if storedHash != requestHash {
			return CreateSiteResult{}, ErrIdempotencyConflict
		}
		var site Site
		if err := json.Unmarshal(storedResponse, &site); err != nil {
			return CreateSiteResult{}, fmt.Errorf("decode idempotent site response: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return CreateSiteResult{}, fmt.Errorf("commit site replay: %w", err)
		}
		return CreateSiteResult{Site: site, Replayed: true}, nil
	}
	if err != pgx.ErrNoRows {
		return CreateSiteResult{}, fmt.Errorf("read idempotency key: %w", err)
	}

	site := Site{
		ID: uuid.NewString(), TenantID: command.TenantID, Name: strings.TrimSpace(command.Name),
		Address: strings.TrimSpace(command.Address), Timezone: command.Timezone, Status: "provisioning",
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO config.sites (id, tenant_id, name, address, timezone, created_by)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, ''), $5, $6)
		RETURNING created_at`,
		site.ID, site.TenantID, site.Name, site.Address, site.Timezone, command.ActorID,
	).Scan(&site.CreatedAt); err != nil {
		return CreateSiteResult{}, classifyCreateError(err)
	}

	response, err := json.Marshal(site)
	if err != nil {
		return CreateSiteResult{}, fmt.Errorf("encode site response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.idempotency_keys (
			tenant_id, idempotency_key, request_hash, response_status,
			resource_type, resource_id, response_body
		) VALUES ($1::uuid, $2, $3, 201, 'site', $4::uuid, $5::jsonb)`,
		command.TenantID, command.IdempotencyKey, requestHash, site.ID, response,
	); err != nil {
		return CreateSiteResult{}, fmt.Errorf("store idempotent site response: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "site.created",
		ResourceType: "site", ResourceID: site.ID, RequestID: command.RequestID,
		BeforeState: nil, AfterState: site, OccurredAt: site.CreatedAt,
	}); err != nil {
		return CreateSiteResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateSiteResult{}, fmt.Errorf("commit site creation: %w", err)
	}
	return CreateSiteResult{Site: site}, nil
}

func beginTenantTx(ctx context.Context, pool transactionPool, tenantID string, accessMode pgx.TxAccessMode) (pgx.Tx, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: accessMode})
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant transaction context: %w", err)
	}
	return tx, nil
}

func hashCreateSite(command CreateSiteCommand) (string, error) {
	payload, err := json.Marshal(struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		Timezone string `json:"timezone"`
	}{Name: strings.TrimSpace(command.Name), Address: strings.TrimSpace(command.Address), Timezone: command.Timezone})
	if err != nil {
		return "", fmt.Errorf("hash site request: %w", err)
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

func classifyCreateError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrTenantNotFound
		case "23505":
			return ErrSiteConflict
		}
	}
	return fmt.Errorf("insert tenant site: %w", err)
}

// MemoryRepository is for deterministic unit tests only.
type MemoryRepository struct {
	mu          sync.Mutex
	sites       []Site
	idempotency map[string]memoryReplay
}

type memoryReplay struct {
	hash string
	site Site
}

// NewMemoryRepository copies seed data so callers cannot mutate repository state.
func NewMemoryRepository(sites []Site) *MemoryRepository {
	return &MemoryRepository{sites: append([]Site(nil), sites...), idempotency: make(map[string]memoryReplay)}
}

// ListSites returns only rows whose tenant key matches the verified claim.
func (r *MemoryRepository) ListSites(_ context.Context, tenantID string) ([]Site, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Site, 0)
	for _, site := range r.sites {
		if site.TenantID == tenantID {
			result = append(result, site)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// CreateSite provides deterministic idempotency behavior for HTTP unit tests.
func (r *MemoryRepository) CreateSite(_ context.Context, command CreateSiteCommand) (CreateSiteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hash, err := hashCreateSite(command)
	if err != nil {
		return CreateSiteResult{}, err
	}
	key := command.TenantID + ":" + command.IdempotencyKey
	if replay, ok := r.idempotency[key]; ok {
		if replay.hash != hash {
			return CreateSiteResult{}, ErrIdempotencyConflict
		}
		return CreateSiteResult{Site: replay.site, Replayed: true}, nil
	}
	site := Site{ID: uuid.NewString(), TenantID: command.TenantID, Name: strings.TrimSpace(command.Name), Address: strings.TrimSpace(command.Address), Timezone: command.Timezone, Status: "provisioning", CreatedAt: time.Now().UTC()}
	r.sites = append(r.sites, site)
	r.idempotency[key] = memoryReplay{hash: hash, site: site}
	return CreateSiteResult{Site: site}, nil
}
