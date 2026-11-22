package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/boxsetup"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

// "Find cameras": the dashboard queues work for a site's box and reads the
// results; the box picks tasks up with its poll and posts results back.
func (s *Server) registerSetupRoutes() {
	s.mux.Handle("GET /v1/sites/{id}/setup/box", s.authenticate(http.HandlerFunc(s.setupBox)))
	s.mux.Handle("POST /v1/sites/{id}/setup/tasks", s.authenticate(http.HandlerFunc(s.createSetupTask)))
	s.mux.Handle("GET /v1/sites/{id}/setup/tasks/{task}", s.authenticate(http.HandlerFunc(s.getSetupTask)))
	s.mux.Handle("POST /v1/site-ingest/setup/tasks/{task}/result", s.authenticateSite(http.HandlerFunc(s.completeSetupTask)))
}

// setupSite authorises camera set-up (config:write) for the site in the path.
// Results contain camera snapshots, so reading them needs the same right.
func (s *Server) setupSite(w http.ResponseWriter, r *http.Request) (identity.Principal, string, string, bool) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return identity.Principal{}, "", "", false
	}
	if s.siteIngest.Setup == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera set-up is unavailable.")
		return identity.Principal{}, "", "", false
	}
	siteID := r.PathValue("id")
	if _, err := uuid.Parse(siteID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identity.Principal{}, "", "", false
	}
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: siteID}); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identity.Principal{}, "", "", false
	}
	return principal, tenantID, siteID, true
}

type setupBoxView struct {
	Paired     bool       `json:"paired"`
	Online     bool       `json:"online"`
	KeyID      string     `json:"key_id,omitempty"`
	Label      string     `json:"label,omitempty"`
	PublicKey  string     `json:"public_key,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// setupBox reports the paired box whose public key the dashboard encrypts
// camera passwords for (the most recently seen paired, unrevoked key).
func (s *Server) setupBox(w http.ResponseWriter, r *http.Request) {
	_, tenantID, siteID, ok := s.setupSite(w, r)
	if !ok {
		return
	}
	view, ok := s.pairedBox(w, r, tenantID, siteID)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

// pairedBox finds the site's most recently seen paired, unrevoked box. A box
// is online when it sent a heartbeat (every 2 minutes) in the last 6 minutes.
func (s *Server) pairedBox(w http.ResponseWriter, r *http.Request, tenantID, siteID string) (setupBoxView, bool) {
	if s.siteIngest.Keys == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return setupBoxView{}, false
	}
	keys, err := s.siteIngest.Keys.List(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return setupBoxView{}, false
	}
	view := setupBoxView{}
	var best *siteingest.SiteKey
	for index := range keys {
		key := keys[index]
		if key.RevokedAt != nil || key.BoxPublicKey == "" {
			continue
		}
		if best == nil || (key.LastSeenAt != nil && (best.LastSeenAt == nil || key.LastSeenAt.After(*best.LastSeenAt))) {
			best = &keys[index]
		}
	}
	if best != nil {
		view = setupBoxView{Paired: true, KeyID: best.ID, Label: best.Label, PublicKey: best.BoxPublicKey, LastSeenAt: best.LastSeenAt}
		view.Online = best.LastSeenAt != nil && time.Since(*best.LastSeenAt) < 6*time.Minute
	}
	return view, true
}

type setupTaskRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type adoptPayload struct {
	DeviceID string `json:"device_id"`
	Cameras  []struct {
		CameraID string `json:"camera_id"`
		Channel  int    `json:"channel"`
	} `json:"cameras"`
}

func (s *Server) createSetupTask(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, siteID, ok := s.setupSite(w, r)
	if !ok {
		return
	}
	var input setupTaskRequest
	if !decodeBodyLimit(w, r, &input, 64<<10) {
		return
	}
	input.Type = strings.TrimSpace(input.Type)
	if !boxsetup.Types[input.Type] {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "type must be scan, probe, adopt or restart.")
		return
	}
	if len(input.Payload) > 0 && !json.Valid(input.Payload) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "payload must be JSON.")
		return
	}
	if input.Type == "adopt" && !s.validAdoption(w, r, tenantID, siteID, input.Payload) {
		return
	}
	if input.Type == boxsetup.TypeRestart {
		if trimmed := strings.TrimSpace(string(input.Payload)); trimmed != "" && trimmed != "null" && trimmed != "{}" {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "restart takes no payload.")
			return
		}
		input.Payload = nil
		// An offline box cannot receive the request; queueing it would
		// restart the box by surprise whenever it comes back.
		box, found := s.pairedBox(w, r, tenantID, siteID)
		if !found {
			return
		}
		if !box.Online {
			writeError(w, http.StatusConflict, "BOX_OFFLINE", "The site box is offline, so it cannot be restarted from here. Check that the computer is switched on, awake and connected to the internet.")
			return
		}
		log.Printf("site box restart requested tenant=%s site=%s by=%s", tenantID, siteID, principal.UserID)
	}
	task, err := s.siteIngest.Setup.Create(tenantID, siteID, input.Type, input.Payload)
	if errors.Is(err, boxsetup.ErrTooManyTasks) {
		writeError(w, http.StatusTooManyRequests, "SETUP_BUSY", "The site box has too many set-up tasks waiting. Check that it is online.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera set-up is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]any{"data": task})
}

// validAdoption checks that every camera being bound is a camera of this site.
func (s *Server) validAdoption(w http.ResponseWriter, r *http.Request, tenantID, siteID string, raw json.RawMessage) bool {
	var payload adoptPayload
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.DeviceID) == "" || len(payload.Cameras) == 0 || len(payload.Cameras) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "adopt needs a device_id and 1-64 cameras.")
		return false
	}
	if s.cameras == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return false
	}
	for _, pick := range payload.Cameras {
		if pick.Channel < 1 || pick.Channel > 64 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "channel must be 1-64.")
			return false
		}
		if _, err := uuid.Parse(pick.CameraID); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "camera_id must be a UUID.")
			return false
		}
		camera, err := s.cameras.Get(r.Context(), tenantID, pick.CameraID)
		if err != nil || camera.SiteID != siteID {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return false
		}
	}
	return true
}

func (s *Server) getSetupTask(w http.ResponseWriter, r *http.Request) {
	_, tenantID, siteID, ok := s.setupSite(w, r)
	if !ok {
		return
	}
	task, found := s.siteIngest.Setup.Get(tenantID, siteID, r.PathValue("task"))
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Set-up task not found or expired.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": task})
}

type setupResultRequest struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func (s *Server) completeSetupTask(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.siteIngest.Setup == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera set-up is unavailable.")
		return
	}
	var input setupResultRequest
	// Up to 32 channel thumbnails of ~20 KB each, base64.
	if !decodeBodyLimit(w, r, &input, 4<<20) {
		return
	}
	if len(input.Error) > 500 || (len(input.Result) > 0 && !json.Valid(input.Result)) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Result is invalid.")
		return
	}
	err := s.siteIngest.Setup.Complete(scope.TenantID, scope.SiteID, r.PathValue("task"), input.Status, input.Result, input.Error)
	switch {
	case errors.Is(err, boxsetup.ErrBadStatus):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "status must be done or failed.")
	case errors.Is(err, boxsetup.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Set-up task not found or expired.")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera set-up is unavailable.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
