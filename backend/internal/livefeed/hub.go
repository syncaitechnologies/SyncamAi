// Package livefeed relays near-live camera frames from a site box to the
// dashboard, only while an authorised person is watching.
//
// Video never leaves the site by default. When an operator opens a camera in
// the Camera Wall, the API records a live-view session (audited) and the site
// box sees that camera in its "wanted" list; it then uploads about two small
// JPEG frames a second until nobody has asked for that camera for ViewerTTL.
// Frames are held in memory only, newest frame per camera, and are never
// written to storage. The hub is per process, which fits the single Render
// instance; several API instances would need a shared store instead.
package livefeed

import (
	"sort"
	"sync"
	"time"
)

const (
	// ViewerTTL is how long a viewer counts as watching after its last poll.
	ViewerTTL = 10 * time.Second
	// FrameTTL is how old a frame may be before it is no longer served as live.
	FrameTTL = 10 * time.Second
	// MaxViewersPerCamera bounds memory use per camera.
	MaxViewersPerCamera = 32
	// MaxContinuous is how long one viewing session may run before it pauses
	// and the viewer has to confirm they are still watching (ADR-017).
	MaxContinuous = 5 * time.Minute
)

type cameraKey struct{ tenantID, siteID, cameraID string }

type frame struct {
	data []byte
	at   time.Time
}

type viewer struct {
	started time.Time
	seen    time.Time
}

// Session state returned by Watching: Elapsed is the viewing time since the
// viewer's previous poll (what the meter charges), Age how long this session
// has run (what the pause compares with MaxContinuous).
type WatchState struct {
	Elapsed time.Duration
	Age     time.Duration
}

// Paused reports whether the session has run for MaxContinuous or longer.
func (s WatchState) Paused() bool { return s.Age >= MaxContinuous }

// Hub holds who is watching which camera and the newest frame for each.
type Hub struct {
	mu      sync.Mutex
	now     func() time.Time
	viewers map[cameraKey]map[string]viewer
	frames  map[cameraKey]frame
}

// NewHub returns an empty hub using the wall clock.
func NewHub() *Hub { return NewHubWithClock(time.Now) }

// NewHubWithClock returns an empty hub with an injectable clock for tests.
func NewHubWithClock(now func() time.Time) *Hub {
	return &Hub{now: now, viewers: map[cameraKey]map[string]viewer{}, frames: map[cameraKey]frame{}}
}

// Watch marks viewerID as watching the camera. It reports whether this starts
// a new viewing session (the viewer was not already watching), which the
// caller audits. ok is false when the camera already has too many viewers.
func (h *Hub) Watch(tenantID, siteID, cameraID, viewerID string) (started, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := cameraKey{tenantID, siteID, cameraID}
	now := h.now()
	h.pruneLocked(key, now)
	viewers := h.viewers[key]
	if viewers == nil {
		viewers = map[string]viewer{}
		h.viewers[key] = viewers
	}
	current, watching := viewers[viewerID]
	if !watching && len(viewers) >= MaxViewersPerCamera {
		return false, false
	}
	if !watching {
		current.started = now
	}
	current.seen = now
	viewers[viewerID] = current
	return !watching, true
}

// Watching reports whether viewerID has an active session for the camera,
// and refreshes it when it does. The returned state says how much viewing
// time passed since the previous poll and how old the session is.
func (h *Hub) Watching(tenantID, siteID, cameraID, viewerID string) (WatchState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := cameraKey{tenantID, siteID, cameraID}
	now := h.now()
	h.pruneLocked(key, now)
	current, ok := h.viewers[key][viewerID]
	if !ok {
		return WatchState{}, false
	}
	state := WatchState{Elapsed: now.Sub(current.seen), Age: now.Sub(current.started)}
	if state.Elapsed < 0 {
		state.Elapsed = 0
	}
	current.seen = now
	h.viewers[key][viewerID] = current
	return state, true
}

// Stop ends viewerID's session for the camera.
func (h *Hub) Stop(tenantID, siteID, cameraID, viewerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := cameraKey{tenantID, siteID, cameraID}
	delete(h.viewers[key], viewerID)
	h.pruneLocked(key, h.now())
}

// Wanted lists the cameras of a site that someone is watching right now.
func (h *Hub) Wanted(tenantID, siteID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	wanted := []string{}
	for key := range h.viewers {
		if key.tenantID != tenantID || key.siteID != siteID {
			continue
		}
		h.pruneLocked(key, now)
		if len(h.viewers[key]) > 0 {
			wanted = append(wanted, key.cameraID)
		}
	}
	sort.Strings(wanted)
	return wanted
}

// Put stores the newest frame for a camera if someone is watching it. It
// reports whether the camera is still wanted; the site box stops when false.
func (h *Hub) Put(tenantID, siteID, cameraID string, data []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := cameraKey{tenantID, siteID, cameraID}
	now := h.now()
	h.pruneLocked(key, now)
	if len(h.viewers[key]) == 0 {
		return false
	}
	h.frames[key] = frame{data: append([]byte(nil), data...), at: now}
	return true
}

// Latest returns the newest frame for a camera if it is recent enough.
func (h *Hub) Latest(tenantID, siteID, cameraID string) ([]byte, time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.frames[cameraKey{tenantID, siteID, cameraID}]
	if !ok || h.now().Sub(current.at) > FrameTTL {
		return nil, time.Time{}, false
	}
	return current.data, current.at, true
}

// pruneLocked drops expired viewers, and the frame once nobody watches.
func (h *Hub) pruneLocked(key cameraKey, now time.Time) {
	for id, current := range h.viewers[key] {
		if now.Sub(current.seen) > ViewerTTL {
			delete(h.viewers[key], id)
		}
	}
	if len(h.viewers[key]) == 0 {
		delete(h.viewers, key)
		delete(h.frames, key)
	}
}
