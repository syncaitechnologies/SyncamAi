package features

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-memory Store for tests and local development.
type MemoryStore struct {
	mu           sync.Mutex
	Snapshots    map[string]Snapshot
	Caps         map[string][]Key
	Alerts       map[string]map[string]int
	Sightings    map[string]int
	Cameras      map[string]map[string]bool
	Changes      []Change
	FailLoad     error
	LoadRequests int
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		Snapshots: map[string]Snapshot{}, Caps: map[string][]Key{}, Alerts: map[string]map[string]int{},
		Sightings: map[string]int{}, Cameras: map[string]map[string]bool{},
	}
}

func (m *MemoryStore) Load(_ context.Context, tenantID string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoadRequests++
	if m.FailLoad != nil {
		return Snapshot{}, m.FailLoad
	}
	snapshot := m.Snapshots[tenantID]
	copied := Snapshot{Plan: snapshot.Plan, LiveViewMinutes: snapshot.LiveViewMinutes, Included: map[Key]bool{}, Settings: map[Key]Setting{}}
	for key, value := range snapshot.Included {
		copied.Included[key] = value
	}
	for key, value := range snapshot.Settings {
		copied.Settings[key] = value
	}
	return copied, nil
}

func (m *MemoryStore) Save(_ context.Context, tenantID string, setting Setting, change Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := m.Snapshots[tenantID]
	if snapshot.Settings == nil {
		snapshot.Settings = map[Key]Setting{}
	}
	snapshot.Settings[setting.Feature] = setting
	m.Snapshots[tenantID] = snapshot
	m.Changes = append(m.Changes, change)
	return nil
}

func (m *MemoryStore) Capabilities(_ context.Context, tenantID string) (map[Key]bool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, reported := m.Caps[tenantID]
	result := map[Key]bool{}
	for _, key := range keys {
		result[key] = true
	}
	return result, reported, nil
}

func (m *MemoryStore) Activity(_ context.Context, tenantID string, _ time.Time) (map[string]int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]int{}
	for key, value := range m.Alerts[tenantID] {
		result[key] = value
	}
	return result, m.Sightings[tenantID], nil
}

func (m *MemoryStore) CameraExists(_ context.Context, tenantID, cameraID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Cameras[tenantID][cameraID], nil
}
