package siteingest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PairingCodeTTL is how long a site box pairing code can be redeemed.
const PairingCodeTTL = 30 * time.Minute

var (
	// ErrPairingInvalid covers unknown, used and expired codes alike, so a
	// caller learns nothing about which codes exist.
	ErrPairingInvalid = errors.New("pairing code is invalid, used or expired")
	// ErrPublicKeyInvalid means the box sent something other than a P-256 key.
	ErrPublicKeyInvalid = errors.New("box public key must be a PEM P-256 public key")
)

// PairingStore issues short pairing codes and turns a redeemed code into a
// site key for the box that typed it.
type PairingStore interface {
	CreatePairingCode(ctx context.Context, tenantID, siteID, actorID string) (string, time.Time, error)
	RedeemPairingCode(ctx context.Context, code, publicKey, label string) (string, Scope, error)
}

// NormalizePairingCode accepts "k7q4-xm92", "K7Q4 XM92" and "K7Q4XM92".
func NormalizePairingCode(code string) string {
	var builder strings.Builder
	for _, char := range strings.ToUpper(code) {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

// FormatPairingCode shows a code as two groups of four for easy typing.
func FormatPairingCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// ValidateBoxPublicKey accepts an empty key or a PEM-encoded P-256 public key.
func ValidateBoxPublicKey(publicKey string) error {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return nil
	}
	if len(publicKey) > 1024 {
		return ErrPublicKeyInvalid
	}
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil || block.Type != "PUBLIC KEY" {
		return ErrPublicKeyInvalid
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ErrPublicKeyInvalid
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return ErrPublicKeyInvalid
	}
	return nil
}

func (s *PostgresKeyStore) CreatePairingCode(ctx context.Context, tenantID, siteID, actorID string) (string, time.Time, error) {
	code, err := NewLinkCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(PairingCodeTTL)
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadWrite)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE tenant_id = $1::uuid AND id = $2::uuid AND status <> 'retired')`, tenantID, siteID).Scan(&exists); err != nil {
		return "", time.Time{}, fmt.Errorf("validate pairing site: %w", err)
	}
	if !exists {
		return "", time.Time{}, ErrSiteNotFound
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.pairing_codes (code_hash, tenant_id, site_id, created_by, expires_at)
		VALUES ($1, $2::uuid, $3::uuid, $4, $5)`, HashKey(code), tenantID, siteID, actorID, expiresAt); err != nil {
		return "", time.Time{}, fmt.Errorf("store pairing code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit pairing code: %w", err)
	}
	return code, expiresAt, nil
}

func (s *PostgresKeyStore) RedeemPairingCode(ctx context.Context, code, publicKey, label string) (string, Scope, error) {
	code = NormalizePairingCode(code)
	if len(code) != 8 {
		return "", Scope{}, ErrPairingInvalid
	}
	if err := ValidateBoxPublicKey(publicKey); err != nil {
		return "", Scope{}, err
	}
	rawKey, err := NewRawKey()
	if err != nil {
		return "", Scope{}, err
	}
	scope := Scope{KeyID: uuid.NewString()}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return "", Scope{}, fmt.Errorf("begin pairing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT tenant_id::text, site_id::text FROM platform.redeem_pairing_code($1, $2::uuid, $3, $4, $5)`,
		HashKey(code), scope.KeyID, HashKey(rawKey), strings.TrimSpace(label), strings.TrimSpace(publicKey)).Scan(&scope.TenantID, &scope.SiteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", Scope{}, ErrPairingInvalid
	}
	if err != nil {
		return "", Scope{}, fmt.Errorf("redeem pairing code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", Scope{}, fmt.Errorf("commit pairing: %w", err)
	}
	return rawKey, scope, nil
}

type memoryPairing struct {
	tenantID, siteID string
	expiresAt        time.Time
	used             bool
}

func (s *MemoryKeyStore) CreatePairingCode(_ context.Context, tenantID, siteID, _ string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sites[tenantID+":"+siteID] {
		return "", time.Time{}, ErrSiteNotFound
	}
	code, err := NewLinkCode()
	if err != nil {
		return "", time.Time{}, err
	}
	if s.pairings == nil {
		s.pairings = map[string]*memoryPairing{}
	}
	expiresAt := time.Now().UTC().Add(PairingCodeTTL)
	s.pairings[HashKey(code)] = &memoryPairing{tenantID: tenantID, siteID: siteID, expiresAt: expiresAt}
	return code, expiresAt, nil
}

// ExpirePairingCode makes a code expire now (tests only).
func (s *MemoryKeyStore) ExpirePairingCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pairing, ok := s.pairings[HashKey(NormalizePairingCode(code))]; ok {
		pairing.expiresAt = time.Now().Add(-time.Second)
	}
}

func (s *MemoryKeyStore) RedeemPairingCode(ctx context.Context, code, publicKey, label string) (string, Scope, error) {
	code = NormalizePairingCode(code)
	if err := ValidateBoxPublicKey(publicKey); err != nil {
		return "", Scope{}, err
	}
	s.mu.Lock()
	pairing, ok := s.pairings[HashKey(code)]
	if !ok || pairing.used || time.Now().After(pairing.expiresAt) {
		s.mu.Unlock()
		return "", Scope{}, ErrPairingInvalid
	}
	pairing.used = true
	s.mu.Unlock()
	if strings.TrimSpace(label) == "" {
		label = "Site box"
	}
	rawKey, key, err := s.Create(ctx, pairing.tenantID, pairing.siteID, "pairing", label)
	if err != nil {
		return "", Scope{}, err
	}
	s.mu.Lock()
	key.BoxPublicKey = strings.TrimSpace(publicKey)
	s.keys[key.ID] = key
	s.mu.Unlock()
	return rawKey, Scope{KeyID: key.ID, TenantID: key.TenantID, SiteID: key.SiteID}, nil
}
