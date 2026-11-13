// Package siteingest lets an on-premises SyncCam site box send detections,
// snapshots and camera health to the control plane, and fans new alerts out
// to client notification channels such as Telegram.
package siteingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
)

// KeyPrefix marks SyncCam site keys so they are recognisable in secret scans.
const KeyPrefix = "sck_"

var (
	ErrInvalidKey   = errors.New("site key is invalid or revoked")
	ErrKeyNotFound  = errors.New("site key not found")
	ErrSiteNotFound = errors.New("site not found")
)

// Scope is everything a verified site key is allowed to act on.
type Scope struct {
	KeyID    string
	TenantID string
	SiteID   string
}

// CameraStatus is the box's latest view of one camera stream.
type CameraStatus struct {
	Online      bool       `json:"online"`
	LastFrameAt *time.Time `json:"last_frame_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// SiteKey is the listable metadata of a key. The raw key is never stored.
type SiteKey struct {
	ID           string                  `json:"id"`
	TenantID     string                  `json:"tenant_id"`
	SiteID       string                  `json:"site_id"`
	Label        string                  `json:"label"`
	CreatedAt    time.Time               `json:"created_at"`
	RevokedAt    *time.Time              `json:"revoked_at,omitempty"`
	LastSeenAt   *time.Time              `json:"last_seen_at,omitempty"`
	CameraStatus map[string]CameraStatus `json:"camera_status"`
}

type KeyStore interface {
	Resolve(ctx context.Context, rawKey string) (Scope, error)
	Create(ctx context.Context, tenantID, siteID, actorID, label string) (string, SiteKey, error)
	List(ctx context.Context, tenantID, siteID string) ([]SiteKey, error)
	Revoke(ctx context.Context, tenantID, keyID string) error
	RecordHeartbeat(ctx context.Context, scope Scope, cameras map[string]CameraStatus) error
}

// NewRawKey returns a fresh random site key.
func NewRawKey() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate site key: %w", err)
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

// HashKey returns the stored SHA-256 hex digest of a raw key.
func HashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func validRawKey(rawKey string) bool {
	return strings.HasPrefix(rawKey, KeyPrefix) && len(rawKey) >= len(KeyPrefix)+40 && len(rawKey) <= 128
}

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresKeyStore struct{ pool transactionPool }

func NewPostgresKeyStore(pool transactionPool) *PostgresKeyStore {
	return &PostgresKeyStore{pool: pool}
}

func (s *PostgresKeyStore) Resolve(ctx context.Context, rawKey string) (Scope, error) {
	if !validRawKey(rawKey) {
		return Scope{}, ErrInvalidKey
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Scope{}, fmt.Errorf("begin site key lookup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var scope Scope
	err = tx.QueryRow(ctx, `SELECT key_id::text, tenant_id::text, site_id::text FROM platform.resolve_site_key($1)`, HashKey(rawKey)).
		Scan(&scope.KeyID, &scope.TenantID, &scope.SiteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Scope{}, ErrInvalidKey
	}
	if err != nil {
		return Scope{}, fmt.Errorf("resolve site key: %w", err)
	}
	return scope, tx.Commit(ctx)
}

func (s *PostgresKeyStore) Create(ctx context.Context, tenantID, siteID, actorID, label string) (string, SiteKey, error) {
	rawKey, err := NewRawKey()
	if err != nil {
		return "", SiteKey{}, err
	}
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadWrite)
	if err != nil {
		return "", SiteKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE tenant_id = $1::uuid AND id = $2::uuid AND status <> 'retired')`, tenantID, siteID).Scan(&exists); err != nil {
		return "", SiteKey{}, fmt.Errorf("validate site key site: %w", err)
	}
	if !exists {
		return "", SiteKey{}, ErrSiteNotFound
	}
	key := SiteKey{ID: uuid.NewString(), TenantID: tenantID, SiteID: siteID, Label: strings.TrimSpace(label), CameraStatus: map[string]CameraStatus{}}
	if err := tx.QueryRow(ctx, `
		INSERT INTO platform.site_keys (id, tenant_id, site_id, label, key_hash, created_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6)
		RETURNING created_at`, key.ID, tenantID, siteID, key.Label, HashKey(rawKey), actorID).Scan(&key.CreatedAt); err != nil {
		return "", SiteKey{}, fmt.Errorf("store site key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", SiteKey{}, fmt.Errorf("commit site key: %w", err)
	}
	return rawKey, key, nil
}

func (s *PostgresKeyStore) List(ctx context.Context, tenantID, siteID string) ([]SiteKey, error) {
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT id::text, tenant_id::text, site_id::text, label, created_at, revoked_at, last_seen_at, camera_status
		FROM platform.site_keys
		WHERE ($1::uuid IS NULL OR site_id = $1::uuid)
		ORDER BY created_at DESC
		LIMIT 100`, nullableUUID(siteID))
	if err != nil {
		return nil, fmt.Errorf("list site keys: %w", err)
	}
	defer rows.Close()
	keys := make([]SiteKey, 0)
	for rows.Next() {
		var key SiteKey
		var status []byte
		if err := rows.Scan(&key.ID, &key.TenantID, &key.SiteID, &key.Label, &key.CreatedAt, &key.RevokedAt, &key.LastSeenAt, &status); err != nil {
			return nil, fmt.Errorf("scan site key: %w", err)
		}
		key.CameraStatus = map[string]CameraStatus{}
		_ = json.Unmarshal(status, &key.CameraStatus)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate site keys: %w", err)
	}
	return keys, tx.Commit(ctx)
}

func (s *PostgresKeyStore) Revoke(ctx context.Context, tenantID, keyID string) error {
	if _, err := uuid.Parse(keyID); err != nil {
		return ErrKeyNotFound
	}
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE platform.site_keys SET revoked_at = COALESCE(revoked_at, clock_timestamp()) WHERE id = $1::uuid`, keyID)
	if err != nil {
		return fmt.Errorf("revoke site key: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrKeyNotFound
	}
	return tx.Commit(ctx)
}

func (s *PostgresKeyStore) RecordHeartbeat(ctx context.Context, scope Scope, cameras map[string]CameraStatus) error {
	payload, err := json.Marshal(cameras)
	if err != nil {
		return fmt.Errorf("encode camera status: %w", err)
	}
	tx, err := beginTenantTx(ctx, s.pool, scope.TenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE platform.site_keys SET last_seen_at = clock_timestamp(), camera_status = $2::jsonb WHERE id = $1::uuid AND revoked_at IS NULL`, scope.KeyID, payload); err != nil {
		return fmt.Errorf("record site heartbeat: %w", err)
	}
	return tx.Commit(ctx)
}

func beginTenantTx(ctx context.Context, pool transactionPool, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if pool == nil {
		return nil, errors.New("site ingest store is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant: %w", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin site ingest transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set site ingest tenant context: %w", err)
	}
	return tx, nil
}

func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// MemoryKeyStore is a deterministic store for tests and local development.
type MemoryKeyStore struct {
	mu     sync.Mutex
	keys   map[string]SiteKey
	hashes map[string]string
	sites  map[string]bool
}

// NewMemoryKeyStore accepts the "tenant:site" pairs that exist.
func NewMemoryKeyStore(sites ...string) *MemoryKeyStore {
	known := make(map[string]bool, len(sites))
	for _, site := range sites {
		known[site] = true
	}
	return &MemoryKeyStore{keys: map[string]SiteKey{}, hashes: map[string]string{}, sites: known}
}

func (s *MemoryKeyStore) Resolve(_ context.Context, rawKey string) (Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validRawKey(rawKey) {
		return Scope{}, ErrInvalidKey
	}
	id, ok := s.hashes[HashKey(rawKey)]
	if !ok || s.keys[id].RevokedAt != nil {
		return Scope{}, ErrInvalidKey
	}
	key := s.keys[id]
	return Scope{KeyID: key.ID, TenantID: key.TenantID, SiteID: key.SiteID}, nil
}

func (s *MemoryKeyStore) Create(_ context.Context, tenantID, siteID, _ string, label string) (string, SiteKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sites[tenantID+":"+siteID] {
		return "", SiteKey{}, ErrSiteNotFound
	}
	rawKey, err := NewRawKey()
	if err != nil {
		return "", SiteKey{}, err
	}
	key := SiteKey{ID: uuid.NewString(), TenantID: tenantID, SiteID: siteID, Label: strings.TrimSpace(label), CreatedAt: time.Now().UTC(), CameraStatus: map[string]CameraStatus{}}
	s.keys[key.ID] = key
	s.hashes[HashKey(rawKey)] = key.ID
	return rawKey, key, nil
}

func (s *MemoryKeyStore) List(_ context.Context, tenantID, siteID string) ([]SiteKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]SiteKey, 0)
	for _, key := range s.keys {
		if key.TenantID == tenantID && (siteID == "" || key.SiteID == siteID) {
			result = append(result, key)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryKeyStore) Revoke(_ context.Context, tenantID, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[keyID]
	if !ok || key.TenantID != tenantID {
		return ErrKeyNotFound
	}
	if key.RevokedAt == nil {
		now := time.Now().UTC()
		key.RevokedAt = &now
		s.keys[keyID] = key
	}
	return nil
}

func (s *MemoryKeyStore) RecordHeartbeat(_ context.Context, scope Scope, cameras map[string]CameraStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[scope.KeyID]
	if !ok {
		return ErrKeyNotFound
	}
	now := time.Now().UTC()
	key.LastSeenAt = &now
	key.CameraStatus = cameras
	s.keys[scope.KeyID] = key
	return nil
}
