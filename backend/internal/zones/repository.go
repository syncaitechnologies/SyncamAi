// Package zones owns tenant-scoped, versioned zone configuration metadata.
package zones

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
	ErrNotFound            = errors.New("zone not found")
	ErrSiteNotFound        = errors.New("site not found")
	ErrVersionConflict     = errors.New("zone configuration version conflict")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
)

// Zone contains geometry and rule type metadata only; it never contains video or pixel data.
type Zone struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	SiteID        string          `json:"site_id"`
	CameraID      string          `json:"camera_id,omitempty"`
	Floor         string          `json:"floor,omitempty"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Geometry      json.RawMessage `json:"geometry"`
	Enabled       bool            `json:"enabled"`
	ConfigVersion int64           `json:"config_version"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type CreateCommand struct {
	TenantID, ActorID, RequestID, IdempotencyKey string
	SiteID, CameraID, Floor, Name, Kind          string
	Geometry                                      json.RawMessage
	Enabled                                       bool
}

type CreateResult struct { Zone Zone; Replayed bool }

type UpdateCommand struct {
	TenantID, ActorID, RequestID, ZoneID string
	ExpectedVersion                      int64
	Name, Floor                          *string
	Geometry                             *json.RawMessage
	Enabled                              *bool
}

type Repository interface {
	List(context.Context, string, string) ([]Zone, error)
	Get(context.Context, string, string) (Zone, error)
	Create(context.Context, CreateCommand) (CreateResult, error)
	Update(context.Context, UpdateCommand) (Zone, error)
}

type replay struct { hash string; zone Zone }

// MemoryRepository is deterministic and used by unit and HTTP boundary tests.
type MemoryRepository struct {
	mu sync.Mutex
	zones []Zone
	idempotency map[string]replay
	now func() time.Time
}

func NewMemoryRepository(seed []Zone) *MemoryRepository {
	copySeed := make([]Zone, len(seed))
	for i := range seed { copySeed[i] = clone(seed[i]) }
	return &MemoryRepository{zones: copySeed, idempotency: make(map[string]replay), now: func() time.Time { return time.Now().UTC() }}
}

func (r *MemoryRepository) List(_ context.Context, tenantID, siteID string) ([]Zone, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	result := make([]Zone, 0)
	for _, zone := range r.zones { if zone.TenantID == tenantID && (siteID == "" || zone.SiteID == siteID) { result = append(result, clone(zone)) } }
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *MemoryRepository) Get(_ context.Context, tenantID, zoneID string) (Zone, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	if index := r.find(tenantID, zoneID); index >= 0 { return clone(r.zones[index]), nil }
	return Zone{}, ErrNotFound
}

func (r *MemoryRepository) Create(_ context.Context, command CreateCommand) (CreateResult, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	hash, err := hashCreate(command); if err != nil { return CreateResult{}, err }
	key := command.TenantID + ":" + command.IdempotencyKey
	if stored, ok := r.idempotency[key]; ok { if stored.hash != hash { return CreateResult{}, ErrIdempotencyConflict }; return CreateResult{Zone: clone(stored.zone), Replayed: true}, nil }
	now := r.now()
	zone := Zone{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, CameraID: strings.TrimSpace(command.CameraID), Floor: strings.TrimSpace(command.Floor), Name: strings.TrimSpace(command.Name), Kind: strings.TrimSpace(command.Kind), Geometry: append(json.RawMessage(nil), command.Geometry...), Enabled: command.Enabled, ConfigVersion: 1, CreatedAt: now, UpdatedAt: now}
	r.zones = append(r.zones, zone)
	r.idempotency[key] = replay{hash: hash, zone: clone(zone)}
	return CreateResult{Zone: clone(zone)}, nil
}

func (r *MemoryRepository) Update(_ context.Context, command UpdateCommand) (Zone, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	index := r.find(command.TenantID, command.ZoneID)
	if index < 0 { return Zone{}, ErrNotFound }
	current := r.zones[index]
	if current.ConfigVersion != command.ExpectedVersion { return Zone{}, ErrVersionConflict }
	next := clone(current)
	if command.Name != nil { next.Name = strings.TrimSpace(*command.Name) }
	if command.Floor != nil { next.Floor = strings.TrimSpace(*command.Floor) }
	if command.Geometry != nil { next.Geometry = append(json.RawMessage(nil), (*command.Geometry)...)}
	if command.Enabled != nil { next.Enabled = *command.Enabled }
	if !same(current, next) { next.ConfigVersion++; next.UpdatedAt = r.now(); r.zones[index] = next }
	return clone(next), nil
}

func (r *MemoryRepository) find(tenantID, zoneID string) int { for i, zone := range r.zones { if zone.TenantID == tenantID && zone.ID == zoneID { return i } }; return -1 }
func clone(zone Zone) Zone { zone.Geometry = append(json.RawMessage(nil), zone.Geometry...); return zone }
func same(a, b Zone) bool { return a.Name == b.Name && a.Floor == b.Floor && a.Enabled == b.Enabled && string(a.Geometry) == string(b.Geometry) }

func hashCreate(command CreateCommand) (string, error) {
	payload, err := json.Marshal(struct { SiteID, CameraID, Floor, Name, Kind string; Geometry json.RawMessage; Enabled bool }{command.SiteID, strings.TrimSpace(command.CameraID), strings.TrimSpace(command.Floor), strings.TrimSpace(command.Name), strings.TrimSpace(command.Kind), command.Geometry, command.Enabled})
	if err != nil { return "", err }; sum := sha256.Sum256(payload); return hex.EncodeToString(sum[:]), nil
}
