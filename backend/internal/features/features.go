package features

import (
	"context"
	"errors"
	"sync"
	"time"
)

// cacheTTL is how long a workspace's feature state is reused before it is
// read again. Changes made through the API clear the cache at once; changes
// made elsewhere (the owner screen on another instance) apply within it.
const cacheTTL = time.Minute

var (
	// ErrUnavailable means feature state could not be read or written.
	ErrUnavailable = errors.New("feature settings are unavailable")
	// ErrNotIncluded means the workspace's plan does not include the feature.
	ErrNotIncluded = errors.New("feature is not included in this plan")
	// ErrNotSwitchable means the feature cannot be switched by a workspace.
	ErrNotSwitchable = errors.New("feature cannot be switched")
	// ErrUnknownCamera means a chosen camera is not in the workspace.
	ErrUnknownCamera = errors.New("camera is not in this workspace")
)

// Setting is a workspace's own switch for one feature.
type Setting struct {
	Feature Key
	Enabled bool
	// Cameras limits the feature to these cameras; nil means every camera.
	Cameras   []string
	UpdatedAt time.Time
	UpdatedBy string
}

// Snapshot is everything stored about a workspace's features.
type Snapshot struct {
	Plan string
	// LiveViewMinutes is the monthly live-view allowance (nil = unlimited,
	// 0 = not included).
	LiveViewMinutes *int
	// Included holds the owner's choices that differ from the default.
	Included map[Key]bool
	Settings map[Key]Setting
}

// Resolved is one feature's effective state for a workspace.
type Resolved struct {
	Key      Key
	Included bool
	Enabled  bool
	Cameras  []string
}

// On reports whether the feature runs anywhere in the workspace.
func (r Resolved) On() bool { return r.Included && r.Enabled }

// Allows reports whether the feature runs on one camera.
func (r Resolved) Allows(cameraID string) bool {
	if !r.On() {
		return false
	}
	if r.Cameras == nil {
		return true
	}
	for _, candidate := range r.Cameras {
		if candidate == cameraID {
			return true
		}
	}
	return false
}

// Resolve combines the plan, the owner's choices and the workspace's
// switches. A feature with no switch is on, so a new workspace gets every
// feature its plan includes.
func Resolve(snapshot Snapshot) map[Key]Resolved {
	result := make(map[Key]Resolved, len(catalog))
	for _, definition := range catalog {
		item := Resolved{Key: definition.Key}
		switch {
		case definition.Status == StatusComingSoon:
		case definition.Status == StatusAlwaysOn:
			item.Included, item.Enabled = true, true
		default:
			item.Included = DefaultIncluded(definition.Key)
			if definition.Key == LiveView {
				item.Included = snapshot.LiveViewMinutes == nil || *snapshot.LiveViewMinutes > 0
			} else if included, ok := snapshot.Included[definition.Key]; ok {
				item.Included = included
			}
			item.Enabled = true
			if setting, ok := snapshot.Settings[definition.Key]; ok {
				item.Enabled = setting.Enabled
				if definition.PerCamera && setting.Cameras != nil {
					item.Cameras = append([]string{}, setting.Cameras...)
				}
			}
		}
		result[definition.Key] = item
	}
	return result
}

// Store reads and writes feature state under the workspace's row-level
// security.
type Store interface {
	Load(ctx context.Context, tenantID string) (Snapshot, error)
	// Save writes one switch and its audit record together.
	Save(ctx context.Context, tenantID string, setting Setting, change Change) error
	// Capabilities returns the features reported by the workspace's active
	// site boxes; reported is false when no box has reported yet.
	Capabilities(ctx context.Context, tenantID string) (capabilities map[Key]bool, reported bool, err error)
	// Activity counts alerts by event type and attendance sightings since a
	// time, so the dashboard can show what each feature caught.
	Activity(ctx context.Context, tenantID string, since time.Time) (alerts map[string]int, sightings int, err error)
	// CameraExists reports whether a camera belongs to the workspace.
	CameraExists(ctx context.Context, tenantID, cameraID string) (bool, error)
}

// Change describes who changed a switch, for the audit record.
type Change struct {
	ActorID   string
	RequestID string
	Before    Setting
	HadBefore bool
}

type cached struct {
	snapshot Snapshot
	loadedAt time.Time
}

// Service caches feature state and answers "does this feature run here?".
type Service struct {
	store Store
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cached
}

// NewService returns a service backed by store, using the wall clock.
func NewService(store Store) *Service { return NewServiceWithClock(store, time.Now) }

// NewServiceWithClock returns a service with an injectable clock for tests.
func NewServiceWithClock(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now, cache: map[string]cached{}}
}

// Store returns the underlying store.
func (s *Service) Store() Store { return s.store }

// Snapshot returns the workspace's stored feature state, cached.
func (s *Service) Snapshot(ctx context.Context, tenantID string) (Snapshot, error) {
	if s == nil || s.store == nil {
		return Snapshot{}, ErrUnavailable
	}
	now := s.now()
	s.mu.Lock()
	entry, ok := s.cache[tenantID]
	s.mu.Unlock()
	if ok && now.Sub(entry.loadedAt) < cacheTTL {
		return entry.snapshot, nil
	}
	snapshot, err := s.store.Load(ctx, tenantID)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	s.cache[tenantID] = cached{snapshot: snapshot, loadedAt: now}
	s.mu.Unlock()
	return snapshot, nil
}

// Effective returns every feature's effective state for the workspace.
func (s *Service) Effective(ctx context.Context, tenantID string) (map[Key]Resolved, error) {
	snapshot, err := s.Snapshot(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return Resolve(snapshot), nil
}

// AllowsEvent reports whether an event type from a camera should become an
// alert. Event types that belong to no feature are always allowed.
func (s *Service) AllowsEvent(ctx context.Context, tenantID, eventType, cameraID string) (bool, error) {
	key, ok := ForEventType(eventType)
	if !ok {
		return true, nil
	}
	return s.Allows(ctx, tenantID, key, cameraID)
}

// Allows reports whether a feature runs on one camera.
func (s *Service) Allows(ctx context.Context, tenantID string, key Key, cameraID string) (bool, error) {
	effective, err := s.Effective(ctx, tenantID)
	if err != nil {
		return true, err
	}
	item, ok := effective[key]
	if !ok {
		return true, nil
	}
	return item.Allows(cameraID), nil
}

// Invalidate drops the cached state so the next read sees a change.
func (s *Service) Invalidate(tenantID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.cache, tenantID)
	s.mu.Unlock()
}

// Update changes a workspace's switch for one feature. enabled and cameras
// are optional: a nil enabled keeps the current value, and cameras is used
// only when setCameras is true, where a nil list means "every camera".
func (s *Service) Update(ctx context.Context, tenantID string, key Key, enabled *bool, cameras []string, setCameras bool, actorID, requestID string) (Resolved, error) {
	if s == nil || s.store == nil {
		return Resolved{}, ErrUnavailable
	}
	definition, ok := Lookup(key)
	if !ok || !Switchable(key) {
		return Resolved{}, ErrNotSwitchable
	}
	if setCameras && !definition.PerCamera {
		return Resolved{}, ErrNotSwitchable
	}
	s.Invalidate(tenantID)
	snapshot, err := s.Snapshot(ctx, tenantID)
	if err != nil {
		return Resolved{}, err
	}
	current := Resolve(snapshot)[key]
	before, hadBefore := snapshot.Settings[key]
	next := Setting{Feature: key, Enabled: current.Enabled, Cameras: current.Cameras}
	if enabled != nil {
		next.Enabled = *enabled
	}
	if setCameras {
		next.Cameras = nil
		if cameras != nil {
			seen := map[string]struct{}{}
			next.Cameras = []string{}
			for _, cameraID := range cameras {
				if _, dup := seen[cameraID]; dup {
					continue
				}
				seen[cameraID] = struct{}{}
				exists, err := s.store.CameraExists(ctx, tenantID, cameraID)
				if err != nil {
					return Resolved{}, err
				}
				if !exists {
					return Resolved{}, ErrUnknownCamera
				}
				next.Cameras = append(next.Cameras, cameraID)
			}
		}
	}
	if next.Enabled && !current.Included {
		return Resolved{}, ErrNotIncluded
	}
	next.UpdatedAt = s.now().UTC()
	next.UpdatedBy = actorID
	if err := s.store.Save(ctx, tenantID, next, Change{ActorID: actorID, RequestID: requestID, Before: before, HadBefore: hadBefore}); err != nil {
		return Resolved{}, err
	}
	s.Invalidate(tenantID)
	effective, err := s.Effective(ctx, tenantID)
	if err != nil {
		return Resolved{}, err
	}
	return effective[key], nil
}
