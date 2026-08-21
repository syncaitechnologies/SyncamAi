// Package configdelivery owns immutable, site-scoped configuration revisions
// and each edge device's reported application state.
package configdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrDeviceNotFound   = errors.New("edge device not found")
	ErrRevisionNotFound = errors.New("configuration revision not found")
	ErrInvalidStatus    = errors.New("configuration application status is invalid")
)

const (
	StatusApplied = "applied"
	StatusFailed  = "failed"
)

// Revision is an immutable complete site snapshot. Payload intentionally
// contains configuration metadata only; it never contains footage, stream
// credentials, certificate material, or privacy-mask pixels.
type Revision struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	SiteID      string          `json:"site_id"`
	Number      int64           `json:"number"`
	Payload     json.RawMessage `json:"payload"`
	ContentHash string          `json:"content_hash"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
}

type PublishCommand struct {
	TenantID, SiteID, ActorID, RequestID string
	Payload                              json.RawMessage
}

// DeviceStatus is the edge-reported outcome for a particular revision.
// ErrorMessage is bounded and must be credential-safe before it reaches this
// boundary.
type DeviceStatus struct {
	DeviceID     string     `json:"device_id"`
	TenantID     string     `json:"tenant_id"`
	SiteID       string     `json:"site_id"`
	Revision     int64      `json:"revision"`
	State        string     `json:"state"`
	ErrorMessage string     `json:"error_message,omitempty"`
	ReportedAt   time.Time  `json:"reported_at"`
	AppliedAt    *time.Time `json:"applied_at,omitempty"`
}

type ReportCommand struct {
	DeviceID, State, ErrorMessage string
	Revision                      int64
}

type PullResult struct {
	Revision *Revision `json:"revision,omitempty"`
}

// Repository separates user-authorized revision publishing from
// certificate-authenticated device pull/report operations.
type Repository interface {
	Publish(context.Context, PublishCommand) (Revision, error)
	List(context.Context, string, string) ([]Revision, error)
	Pull(context.Context, string, int64) (PullResult, error)
	DesiredRevision(context.Context, string) (int64, error)
	Report(context.Context, ReportCommand) (DeviceStatus, error)
	GetStatus(context.Context, string, string) (DeviceStatus, error)
}

type DeviceBinding struct{ ID, TenantID, SiteID string }

// MemoryRepository is deterministic and deliberately models the same
// site-scoped visibility rules as the production repository.
type MemoryRepository struct {
	mu        sync.Mutex
	revisions []Revision
	devices   map[string]DeviceBinding
	statuses  map[string]DeviceStatus
	now       func() time.Time
}

func NewMemoryRepository(devices []DeviceBinding) *MemoryRepository {
	bindings := make(map[string]DeviceBinding, len(devices))
	for _, device := range devices {
		bindings[device.ID] = device
	}
	return &MemoryRepository{devices: bindings, statuses: make(map[string]DeviceStatus), now: func() time.Time { return time.Now().UTC() }}
}

func (r *MemoryRepository) Publish(_ context.Context, command PublishCommand) (Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payload, hash, err := normalizePayload(command.Payload)
	if err != nil {
		return Revision{}, err
	}
	var number int64
	for _, revision := range r.revisions {
		if revision.TenantID == command.TenantID && revision.SiteID == command.SiteID && revision.Number > number {
			number = revision.Number
		}
	}
	now := r.now().UTC()
	revision := Revision{ID: revisionID(command.TenantID, command.SiteID, number+1), TenantID: command.TenantID, SiteID: command.SiteID, Number: number + 1, Payload: payload, ContentHash: hash, CreatedBy: command.ActorID, CreatedAt: now}
	r.revisions = append(r.revisions, revision)
	return cloneRevision(revision), nil
}

func (r *MemoryRepository) List(_ context.Context, tenantID, siteID string) ([]Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Revision, 0)
	for _, revision := range r.revisions {
		if revision.TenantID == tenantID && (siteID == "" || revision.SiteID == siteID) {
			result = append(result, cloneRevision(revision))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SiteID == result[j].SiteID {
			return result[i].Number > result[j].Number
		}
		return result[i].SiteID < result[j].SiteID
	})
	return result, nil
}

func (r *MemoryRepository) Pull(_ context.Context, deviceID string, afterRevision int64) (PullResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[deviceID]
	if !ok {
		return PullResult{}, ErrDeviceNotFound
	}
	latest := latest(r.revisions, device.TenantID, device.SiteID)
	if latest == nil || latest.Number <= afterRevision {
		return PullResult{}, nil
	}
	copy := cloneRevision(*latest)
	return PullResult{Revision: &copy}, nil
}

func (r *MemoryRepository) DesiredRevision(_ context.Context, deviceID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[deviceID]
	if !ok {
		return 0, ErrDeviceNotFound
	}
	if revision := latest(r.revisions, device.TenantID, device.SiteID); revision != nil {
		return revision.Number, nil
	}
	return 0, nil
}

func (r *MemoryRepository) Report(_ context.Context, command ReportCommand) (DeviceStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[command.DeviceID]
	if !ok {
		return DeviceStatus{}, ErrDeviceNotFound
	}
	if err := validateReport(command); err != nil {
		return DeviceStatus{}, err
	}
	revision := latest(r.revisions, device.TenantID, device.SiteID)
	if revision == nil || command.Revision > revision.Number {
		return DeviceStatus{}, ErrRevisionNotFound
	}
	if previous, exists := r.statuses[device.ID]; exists && command.Revision < previous.Revision {
		return DeviceStatus{}, ErrInvalidStatus
	}
	now := r.now().UTC()
	status := DeviceStatus{DeviceID: device.ID, TenantID: device.TenantID, SiteID: device.SiteID, Revision: command.Revision, State: command.State, ErrorMessage: strings.TrimSpace(command.ErrorMessage), ReportedAt: now}
	if status.State == StatusApplied {
		status.AppliedAt = &now
	}
	r.statuses[device.ID] = status
	return cloneStatus(status), nil
}

func (r *MemoryRepository) GetStatus(_ context.Context, tenantID, deviceID string) (DeviceStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.statuses[deviceID]
	if !ok || status.TenantID != tenantID {
		return DeviceStatus{}, ErrDeviceNotFound
	}
	return cloneStatus(status), nil
}

func latest(revisions []Revision, tenantID, siteID string) *Revision {
	var selected *Revision
	for index := range revisions {
		candidate := &revisions[index]
		if candidate.TenantID == tenantID && candidate.SiteID == siteID && (selected == nil || candidate.Number > selected.Number) {
			selected = candidate
		}
	}
	return selected
}

func normalizePayload(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, "", errors.New("configuration payload is invalid")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, "", err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, "", errors.New("configuration payload must be an object")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func validateReport(command ReportCommand) error {
	if command.Revision < 1 || (command.State != StatusApplied && command.State != StatusFailed) || len(strings.TrimSpace(command.ErrorMessage)) > 512 || (command.State == StatusApplied && strings.TrimSpace(command.ErrorMessage) != "") {
		return ErrInvalidStatus
	}
	return nil
}

func revisionID(_ string, _ string, _ int64) string { return uuid.NewString() }
func cloneRevision(value Revision) Revision {
	value.Payload = append(json.RawMessage(nil), value.Payload...)
	return value
}
func cloneStatus(value DeviceStatus) DeviceStatus {
	if value.AppliedAt != nil {
		copy := *value.AppliedAt
		value.AppliedAt = &copy
	}
	return value
}
