package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/boxsetup"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/features"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/livefeed"
)

// Near-live view: frames flow only while someone authorised is watching, and
// every new viewing session is written to the audit log before any frame is
// served (raw_video:read, Operator+ with the raw_video data class).
func (s *Server) registerLiveViewRoutes() {
	s.mux.Handle("POST /v1/cameras/{id}/live", s.authenticate(http.HandlerFunc(s.startLiveView)))
	s.mux.Handle("GET /v1/cameras/{id}/live/frame", s.authenticate(http.HandlerFunc(s.liveFrame)))
	s.mux.Handle("DELETE /v1/cameras/{id}/live", s.authenticate(http.HandlerFunc(s.stopLiveView)))
	s.mux.Handle("GET /v1/live-view/usage", s.authenticate(http.HandlerFunc(s.liveViewUsage)))
	s.mux.Handle("GET /v1/site-ingest/live", s.authenticateSite(http.HandlerFunc(s.siteLiveRequests)))
	s.mux.Handle("PUT /v1/site-ingest/cameras/{id}/live", s.authenticateSite(http.HandlerFunc(s.uploadLiveFrame)))
}

// liveCamera authorises raw-video access to one camera.
func (s *Server) liveCamera(w http.ResponseWriter, r *http.Request) (identity.Principal, string, device.Camera, bool) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityRawVideoRead)
	if !ok {
		return identity.Principal{}, "", device.Camera{}, false
	}
	if s.siteIngest.Live == nil || s.siteIngest.LiveAudit == nil || s.cameras == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view is unavailable.")
		return identity.Principal{}, "", device.Camera{}, false
	}
	camera, ok := s.authorizedCamera(w, r, principal, tenantID)
	if !ok {
		return identity.Principal{}, "", device.Camera{}, false
	}
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityRawVideoRead, TenantID: tenantID, SiteID: camera.SiteID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identity.Principal{}, "", device.Camera{}, false
	}
	return principal, tenantID, camera, true
}

func (s *Server) startLiveView(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, camera, ok := s.liveCamera(w, r)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	hub := s.siteIngest.Live
	// The monthly allowance is checked before anything is audited or sent.
	usage, ok := s.liveUsage(w, r, tenantID)
	if !ok {
		return
	}
	if usage.Exhausted() {
		writeLiveViewLimit(w, usage)
		return
	}
	if !s.featureAllows(r.Context(), tenantID, features.LiveView, "", camera.ID) {
		writeLiveViewOff(w)
		return
	}
	state, watching := hub.Watching(tenantID, camera.SiteID, camera.ID, principal.UserID)
	if watching {
		if usage, ok = s.chargeLiveView(w, r, tenantID, state.Elapsed); !ok {
			return
		}
		if state.Paused() {
			// "Still watching?": a paused session continues as a new one.
			hub.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
			watching = false
		}
	}
	if !watching {
		// Audit first: no session (and so no frame) without a record of it.
		err := s.siteIngest.LiveAudit.LiveViewStarted(r.Context(), livefeed.Session{
			TenantID: tenantID, SiteID: camera.SiteID, CameraID: camera.ID,
			ActorID: principal.UserID, ActorEmail: principal.Email, RequestID: requestID, StartedAt: time.Now().UTC(),
		})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view could not be recorded, so it was not started.")
			return
		}
		if _, ok := hub.Watch(tenantID, camera.SiteID, camera.ID, principal.UserID); !ok {
			writeError(w, http.StatusTooManyRequests, "LIVE_VIEW_BUSY", "Too many people are watching this camera.")
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"camera_id": camera.ID, "viewer": principal.Email, "audited": true,
		"expires_in_seconds": int(livefeed.ViewerTTL / time.Second), "poll_interval_ms": 500,
		"max_continuous_seconds": int(livefeed.MaxContinuous / time.Second),
		"remaining_seconds":      remainingSeconds(usage),
	}})
}

func (s *Server) liveFrame(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, camera, ok := s.liveCamera(w, r)
	if !ok {
		return
	}
	hub := s.siteIngest.Live
	state, watching := hub.Watching(tenantID, camera.SiteID, camera.ID, principal.UserID)
	if !watching {
		writeError(w, http.StatusConflict, "LIVE_SESSION_REQUIRED", "Start a live view session first.")
		return
	}
	usage, ok := s.chargeLiveView(w, r, tenantID, state.Elapsed)
	if !ok {
		hub.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
		return
	}
	if usage.Exhausted() {
		hub.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
		writeLiveViewLimit(w, usage)
		return
	}
	if !s.featureAllows(r.Context(), tenantID, features.LiveView, "", camera.ID) {
		hub.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
		writeLiveViewOff(w)
		return
	}
	if state.Paused() {
		hub.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
		writeError(w, http.StatusConflict, "LIVE_VIEW_PAUSED", "Live view paused after 5 minutes. Press continue if you are still watching.")
		return
	}
	data, at, ok := hub.Latest(tenantID, camera.SiteID, camera.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "LIVE_FRAME_PENDING", "Waiting for the site box to send a frame.")
		return
	}
	if usage.Limited() {
		w.Header().Set("X-Live-View-Remaining-Seconds", strconv.FormatInt(int64(usage.Remaining()/time.Second), 10))
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Captured-At", at.UTC().Format(time.RFC3339Nano))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) stopLiveView(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, camera, ok := s.liveCamera(w, r)
	if !ok {
		return
	}
	s.siteIngest.Live.Stop(tenantID, camera.SiteID, camera.ID, principal.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// liveUsage returns the workspace's live-view allowance. Without a meter
// (local tests) live view is unlimited; with one, a read failure fails closed.
func (s *Server) liveUsage(w http.ResponseWriter, r *http.Request, tenantID string) (livefeed.Usage, bool) {
	if s.siteIngest.LiveMeter == nil {
		return livefeed.Usage{}, true
	}
	usage, err := s.siteIngest.LiveMeter.Usage(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view allowance could not be checked, so live view was not started.")
		return livefeed.Usage{}, false
	}
	return usage, true
}

// chargeLiveView adds viewing time to the workspace's monthly use.
func (s *Server) chargeLiveView(w http.ResponseWriter, r *http.Request, tenantID string, viewed time.Duration) (livefeed.Usage, bool) {
	if s.siteIngest.LiveMeter == nil {
		return livefeed.Usage{}, true
	}
	usage, err := s.siteIngest.LiveMeter.Charge(r.Context(), tenantID, viewed)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view allowance could not be checked, so live view was stopped.")
		return livefeed.Usage{}, false
	}
	return usage, true
}

func remainingSeconds(usage livefeed.Usage) any {
	if !usage.Limited() {
		return nil
	}
	return int64(usage.Remaining() / time.Second)
}

// writeLiveViewOff answers when the workspace has switched live view off
// (ADR-019); its admins can switch it back on in Features.
func writeLiveViewOff(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "LIVE_VIEW_OFF", "Live view is switched off for this workspace. An admin can turn it on in Features.")
}

func writeLiveViewLimit(w http.ResponseWriter, usage livefeed.Usage) {
	message := "This workspace's live view minutes for this month are used up. Contact SyncCam to add more."
	if usage.LimitMinutes != nil && *usage.LimitMinutes == 0 {
		message = "Live view is not included for this workspace. Contact SyncCam to add it."
	}
	writeError(w, http.StatusForbidden, "LIVE_VIEW_LIMIT", message)
}

// liveViewUsage reports this month's live-view allowance and use.
func (s *Server) liveViewUsage(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	if s.siteIngest.LiveMeter == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view usage is unavailable.")
		return
	}
	usage, err := s.siteIngest.LiveMeter.Usage(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view usage is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"limit_minutes": usage.LimitMinutes, "used_seconds": int64(usage.Used / time.Second),
		"month": usage.Month.Format("2006-01-02"), "resets_at": usage.ResetsAt(),
		"max_continuous_seconds": int(livefeed.MaxContinuous / time.Second),
	}})
}

func (s *Server) siteLiveRequests(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	cameras := []string{}
	if s.siteIngest.Live != nil {
		cameras = s.siteIngest.Live.Wanted(scope.TenantID, scope.SiteID)
	}
	// The same poll carries "Find cameras" tasks for this box.
	tasks := []boxsetup.Delivery{}
	if s.siteIngest.Setup != nil {
		tasks = s.siteIngest.Setup.Deliver(scope.TenantID, scope.SiteID)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"cameras": cameras, "fps": 2, "tasks": tasks}})
}

func (s *Server) uploadLiveFrame(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.siteIngest.Live == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Live view is unavailable.")
		return
	}
	cameraID := r.PathValue("id")
	if _, err := uuid.Parse(cameraID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	data, ok := readJPEG(w, r)
	if !ok {
		return
	}
	// Only cameras someone is watching in this site are accepted; the hub
	// keys frames by the site from the verified key, never by the client.
	if !s.siteIngest.Live.Put(scope.TenantID, scope.SiteID, cameraID, data) {
		writeError(w, http.StatusConflict, "LIVE_NOT_WANTED", "Nobody is watching this camera.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
