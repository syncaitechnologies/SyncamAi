package device

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const claimLifetime = 24 * time.Hour

var (
	ErrClaimInvalid         = errors.New("device claim is invalid")
	ErrClaimExpired         = errors.New("device claim has expired")
	ErrClaimConsumed        = errors.New("device claim has already been consumed")
	ErrClaimSerialMismatch  = errors.New("device serial does not match claim")
	ErrClaimTokenConfig     = errors.New("device claim signing key is invalid")
	ErrDeviceSerialConflict = errors.New("edge device serial conflicts with tenant state")
)

// EdgeDevice is the cloud registry binding for one physical edge appliance.
// Certificate material and claim-token digests are intentionally excluded.
type EdgeDevice struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	SiteID            string     `json:"site_id"`
	SerialNumber      string     `json:"serial_number"`
	HardwareTier      string     `json:"hardware_tier"`
	Model             string     `json:"model,omitempty"`
	Status            string     `json:"status"`
	CertificateStatus string     `json:"certificate_status"`
	FirmwareVersion   string     `json:"firmware_version,omitempty"`
	StoreForwardDepth int64      `json:"store_forward_depth"`
	UptimeSeconds     int64      `json:"uptime_seconds"`
	LastHeartbeat     *time.Time `json:"last_heartbeat,omitempty"`
	ActivatedAt       *time.Time `json:"activated_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// DeviceClaim is safe registry metadata. The bearer claim is returned separately
// once and is never persisted in plaintext or included in an audit state.
type DeviceClaim struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"device_id"`
	TenantID     string    `json:"tenant_id"`
	SiteID       string    `json:"site_id"`
	SerialNumber string    `json:"serial_number"`
	HardwareTier string    `json:"hardware_tier"`
	Model        string    `json:"model,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type IssueClaimCommand struct {
	TenantID       string
	ActorID        string
	RequestID      string
	IdempotencyKey string
	SiteID         string
	SerialNumber   string
	HardwareTier   string
	Model          string
}

type IssueClaimResult struct {
	Claim      DeviceClaim `json:"claim"`
	ClaimToken string      `json:"claim_token"`
	Replayed   bool        `json:"-"`
}

type ActivateDeviceCommand struct {
	DeviceID     string
	ClaimToken   string
	SerialNumber string
	RequestID    string
}

type EnrollmentRepository interface {
	IssueClaim(context.Context, IssueClaimCommand) (IssueClaimResult, error)
	Activate(context.Context, ActivateDeviceCommand) (EdgeDevice, error)
}

// ClaimTokenManager signs deterministic, opaque bearer claims. Determinism lets
// an idempotent issuance replay return the same token without storing plaintext.
type ClaimTokenManager struct{ key []byte }

func NewClaimTokenManager(key []byte) (*ClaimTokenManager, error) {
	if len(key) < 32 {
		return nil, ErrClaimTokenConfig
	}
	return &ClaimTokenManager{key: append([]byte(nil), key...)}, nil
}

func NewClaimTokenManagerFromBase64(raw string) (*ClaimTokenManager, error) {
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, ErrClaimTokenConfig
	}
	return NewClaimTokenManager(key)
}

func (m *ClaimTokenManager) Token(claimID, tenantID string) (string, error) {
	if m == nil || len(m.key) < 32 {
		return "", ErrClaimTokenConfig
	}
	claim, err := uuid.Parse(claimID)
	if err != nil {
		return "", ErrClaimInvalid
	}
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return "", ErrClaimInvalid
	}
	payload := make([]byte, 0, 32)
	payload = append(payload, claim[:]...)
	payload = append(payload, tenant[:]...)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	message := "v1." + encoded
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (m *ClaimTokenManager) Verify(token string) (claimID, tenantID string, err error) {
	if m == nil || len(m.key) < 32 {
		return "", "", ErrClaimTokenConfig
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return "", "", ErrClaimInvalid
	}
	payload, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
	if decodeErr != nil || signatureErr != nil || len(payload) != 32 || len(signature) != sha256.Size {
		return "", "", ErrClaimInvalid
	}
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", "", ErrClaimInvalid
	}
	claim, claimErr := uuid.FromBytes(payload[:16])
	tenant, tenantErr := uuid.FromBytes(payload[16:])
	if claimErr != nil || tenantErr != nil {
		return "", "", ErrClaimInvalid
	}
	return claim.String(), tenant.String(), nil
}

func claimTokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

type memoryEnrollmentClaim struct {
	claim      DeviceClaim
	tokenHash  []byte
	consumedAt *time.Time
}

type memoryEnrollmentReplay struct {
	hash  string
	claim DeviceClaim
}

type MemoryEnrollmentRepository struct {
	mu          sync.Mutex
	tokens      *ClaimTokenManager
	claims      map[string]memoryEnrollmentClaim
	devices     map[string]EdgeDevice
	idempotency map[string]memoryEnrollmentReplay
	now         func() time.Time
}

func NewMemoryEnrollmentRepository(tokens *ClaimTokenManager) *MemoryEnrollmentRepository {
	return &MemoryEnrollmentRepository{
		tokens: tokens, claims: make(map[string]memoryEnrollmentClaim), devices: make(map[string]EdgeDevice),
		idempotency: make(map[string]memoryEnrollmentReplay), now: func() time.Time { return time.Now().UTC() },
	}
}

func (r *MemoryEnrollmentRepository) IssueClaim(_ context.Context, command IssueClaimCommand) (IssueClaimResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens == nil {
		return IssueClaimResult{}, ErrClaimTokenConfig
	}
	hash, err := hashIssueClaim(command)
	if err != nil {
		return IssueClaimResult{}, err
	}
	key := command.TenantID + ":" + command.IdempotencyKey
	if replay, ok := r.idempotency[key]; ok {
		if replay.hash != hash {
			return IssueClaimResult{}, ErrIdempotencyConflict
		}
		token, err := r.tokens.Token(replay.claim.ID, replay.claim.TenantID)
		return IssueClaimResult{Claim: replay.claim, ClaimToken: token, Replayed: true}, err
	}
	serial := normalizeSerial(command.SerialNumber)
	for _, existing := range r.devices {
		if existing.TenantID == command.TenantID && existing.SerialNumber == serial {
			return IssueClaimResult{}, ErrDeviceSerialConflict
		}
	}
	now := r.now().UTC()
	deviceID, claimID := uuid.NewString(), uuid.NewString()
	device := EdgeDevice{
		ID: deviceID, TenantID: command.TenantID, SiteID: command.SiteID, SerialNumber: serial,
		HardwareTier: normalizeHardwareTier(command.HardwareTier), Model: strings.TrimSpace(command.Model),
		Status: "pending", CertificateStatus: "pending", CreatedAt: now, UpdatedAt: now,
	}
	claim := DeviceClaim{
		ID: claimID, DeviceID: deviceID, TenantID: command.TenantID, SiteID: command.SiteID,
		SerialNumber: serial, HardwareTier: device.HardwareTier, Model: device.Model,
		CreatedAt: now, ExpiresAt: now.Add(claimLifetime),
	}
	token, err := r.tokens.Token(claim.ID, claim.TenantID)
	if err != nil {
		return IssueClaimResult{}, err
	}
	r.devices[device.ID] = device
	r.claims[claim.ID] = memoryEnrollmentClaim{claim: claim, tokenHash: claimTokenHash(token)}
	r.idempotency[key] = memoryEnrollmentReplay{hash: hash, claim: claim}
	return IssueClaimResult{Claim: claim, ClaimToken: token}, nil
}

func (r *MemoryEnrollmentRepository) Activate(_ context.Context, command ActivateDeviceCommand) (EdgeDevice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	token := strings.TrimSpace(command.ClaimToken)
	claimID, tenantID, err := r.tokens.Verify(token)
	if err != nil {
		return EdgeDevice{}, err
	}
	stored, ok := r.claims[claimID]
	if !ok || stored.claim.TenantID != tenantID || !hmac.Equal(stored.tokenHash, claimTokenHash(token)) {
		return EdgeDevice{}, ErrClaimInvalid
	}
	if stored.consumedAt != nil {
		return EdgeDevice{}, ErrClaimConsumed
	}
	now := r.now().UTC()
	if !now.Before(stored.claim.ExpiresAt) {
		return EdgeDevice{}, ErrClaimExpired
	}
	if normalizeSerial(command.SerialNumber) != stored.claim.SerialNumber {
		return EdgeDevice{}, ErrClaimSerialMismatch
	}
	device, ok := r.devices[stored.claim.DeviceID]
	if !ok || device.TenantID != tenantID || device.ID != command.DeviceID {
		return EdgeDevice{}, ErrClaimInvalid
	}
	stored.consumedAt = &now
	r.claims[claimID] = stored
	device.Status = "active"
	device.ActivatedAt = &now
	device.UpdatedAt = now
	r.devices[device.ID] = device
	return device, nil
}

func hashIssueClaim(command IssueClaimCommand) (string, error) {
	payload, err := json.Marshal(struct {
		SiteID       string `json:"site_id"`
		SerialNumber string `json:"serial_number"`
		HardwareTier string `json:"hardware_tier"`
		Model        string `json:"model"`
	}{strings.TrimSpace(command.SiteID), normalizeSerial(command.SerialNumber), normalizeHardwareTier(command.HardwareTier), strings.TrimSpace(command.Model)})
	if err != nil {
		return "", fmt.Errorf("encode device claim request: %w", err)
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

func normalizeHardwareTier(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
