package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const DeviceOfflineAfter = 90 * time.Second

var (
	ErrDeviceUnauthorized = errors.New("edge device is not authorized")
	ErrHeartbeatConflict  = errors.New("heartbeat identifier reused with different telemetry")
)

type HeartbeatCommand struct {
	DeviceID          string
	HeartbeatID       string
	ReportedAt        time.Time
	UptimeSeconds     int64
	StoreForwardDepth int64
	FirmwareVersion   string
}

type HeartbeatResult struct {
	Device     EdgeDevice `json:"device"`
	ObservedAt time.Time  `json:"observed_at"`
	Replayed   bool       `json:"-"`
}

type StatusRepository interface {
	ListDevices(context.Context, string, string, time.Time) ([]EdgeDevice, error)
	RecordHeartbeat(context.Context, HeartbeatCommand) (HeartbeatResult, error)
}

// DeviceIdentityVerifier authenticates an edge request independently of user OIDC.
type DeviceIdentityVerifier interface {
	VerifyDevice(*http.Request) (string, error)
}

// MTLSDeviceVerifier accepts only a TLS-verified client certificate whose CN is
// the canonical device UUID. Registry status is checked again by the repository.
type MTLSDeviceVerifier struct {
	Now func() time.Time
}

func (v MTLSDeviceVerifier) VerifyDevice(request *http.Request) (string, error) {
	if request == nil || request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.VerifiedChains[0]) == 0 {
		return "", ErrDeviceUnauthorized
	}
	certificate := request.TLS.VerifiedChains[0][0]
	now := time.Now().UTC()
	if v.Now != nil {
		now = v.Now().UTC()
	}
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return "", ErrDeviceUnauthorized
	}
	deviceID, err := uuid.Parse(strings.TrimSpace(certificate.Subject.CommonName))
	if err != nil {
		return "", ErrDeviceUnauthorized
	}
	return deviceID.String(), nil
}

type memoryHeartbeatReceipt struct {
	hash   string
	result HeartbeatResult
}

type MemoryStatusRepository struct {
	mu       sync.Mutex
	devices  map[string]EdgeDevice
	receipts map[string]memoryHeartbeatReceipt
	now      func() time.Time
}

func NewMemoryStatusRepository(devices []EdgeDevice) *MemoryStatusRepository {
	stored := make(map[string]EdgeDevice, len(devices))
	for _, device := range devices {
		stored[device.ID] = device
	}
	return &MemoryStatusRepository{devices: stored, receipts: make(map[string]memoryHeartbeatReceipt), now: func() time.Time { return time.Now().UTC() }}
}

func (r *MemoryStatusRepository) ListDevices(_ context.Context, tenantID, siteID string, observedAt time.Time) ([]EdgeDevice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]EdgeDevice, 0)
	for _, stored := range r.devices {
		if stored.TenantID != tenantID || (siteID != "" && stored.SiteID != siteID) || stored.Status == "retired" {
			continue
		}
		device := stored
		device.Status = EffectiveDeviceStatus(device, observedAt)
		result = append(result, device)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *MemoryStatusRepository) RecordHeartbeat(_ context.Context, command HeartbeatCommand) (HeartbeatResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[command.DeviceID]
	if !ok || device.CertificateStatus != "active" || device.Status == "pending" || device.Status == "retired" {
		return HeartbeatResult{}, ErrDeviceUnauthorized
	}
	hash, err := hashHeartbeat(command)
	if err != nil {
		return HeartbeatResult{}, err
	}
	receiptKey := command.DeviceID + ":" + command.HeartbeatID
	if receipt, exists := r.receipts[receiptKey]; exists {
		if receipt.hash != hash {
			return HeartbeatResult{}, ErrHeartbeatConflict
		}
		replayed := receipt.result
		replayed.Replayed = true
		return replayed, nil
	}
	observedAt := r.now().UTC()
	device.Status = "active"
	device.FirmwareVersion = strings.TrimSpace(command.FirmwareVersion)
	device.StoreForwardDepth = command.StoreForwardDepth
	device.UptimeSeconds = command.UptimeSeconds
	device.LastHeartbeat = &observedAt
	device.UpdatedAt = observedAt
	r.devices[device.ID] = device
	result := HeartbeatResult{Device: device, ObservedAt: observedAt}
	r.receipts[receiptKey] = memoryHeartbeatReceipt{hash: hash, result: result}
	return result, nil
}

func EffectiveDeviceStatus(device EdgeDevice, observedAt time.Time) string {
	if device.Status == "pending" || device.Status == "retired" {
		return device.Status
	}
	latest := device.ActivatedAt
	if device.LastHeartbeat != nil {
		latest = device.LastHeartbeat
	}
	if latest == nil || observedAt.Sub(latest.UTC()) > DeviceOfflineAfter {
		return "offline"
	}
	return device.Status
}

func hashHeartbeat(command HeartbeatCommand) (string, error) {
	payload, err := json.Marshal(struct {
		ReportedAt        time.Time `json:"reported_at"`
		UptimeSeconds     int64     `json:"uptime_seconds"`
		StoreForwardDepth int64     `json:"store_forward_depth"`
		FirmwareVersion   string    `json:"firmware_version"`
	}{command.ReportedAt.UTC(), command.UptimeSeconds, command.StoreForwardDepth, strings.TrimSpace(command.FirmwareVersion)})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}
