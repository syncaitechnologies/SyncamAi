package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/features"
)

// Feature switches (ADR-019): the owner decides which features each plan
// includes; the workspace's super admins switch included features on or off,
// for all cameras or chosen ones. Site boxes read the result with their
// configuration, and the ingest guard drops events from switched-off
// features so older boxes are covered too.
func (s *Server) registerFeatureRoutes() {
	s.mux.Handle("GET /v1/features", s.authenticate(http.HandlerFunc(s.listFeatures)))
	s.mux.Handle("PATCH /v1/features/{key}", s.authenticate(http.HandlerFunc(s.updateFeature)))
	s.mux.Handle("PUT /v1/admin/workspaces/{id}/features", s.authenticate(http.HandlerFunc(s.adminSetFeatures)))
}

// activityWindow is how far back the Features page counts alerts.
const activityWindow = 30 * 24 * time.Hour

type featureView struct {
	Key       features.Key    `json:"key"`
	Status    features.Status `json:"status"`
	PerCamera bool            `json:"per_camera"`
	Included  bool            `json:"included"`
	Enabled   bool            `json:"enabled"`
	On        bool            `json:"on"`
	// Cameras is nil when the feature runs on every camera.
	Cameras []string `json:"cameras"`
	// BoxReady is nil when no site box has reported what it has loaded.
	BoxReady   *bool          `json:"box_ready"`
	Activity30 *int           `json:"activity_30d"`
	UpdatedAt  *time.Time     `json:"updated_at"`
	LiveView   map[string]any `json:"live_view,omitempty"`
}

func (s *Server) featureService(w http.ResponseWriter) (*features.Service, bool) {
	if s.siteIngest.Features == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Feature settings are unavailable.")
		return nil, false
	}
	return s.siteIngest.Features, true
}

func activityCount(key features.Key, alerts map[string]int, sightings int) *int {
	if key == features.Attendance {
		return &sightings
	}
	definition, ok := features.Lookup(key)
	if !ok || len(definition.EventTypes) == 0 {
		return nil
	}
	total := 0
	for _, eventType := range definition.EventTypes {
		total += alerts[eventType]
	}
	return &total
}

func (s *Server) featureViews(ctx context.Context, service *features.Service, tenantID string) ([]featureView, error) {
	snapshot, err := service.Snapshot(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	effective := features.Resolve(snapshot)
	capabilities, reported, err := service.Store().Capabilities(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	alerts, sightings, err := service.Store().Activity(ctx, tenantID, time.Now().UTC().Add(-activityWindow))
	if err != nil {
		return nil, err
	}
	views := make([]featureView, 0, len(effective))
	for _, definition := range features.Catalog() {
		item := effective[definition.Key]
		view := featureView{
			Key: definition.Key, Status: definition.Status, PerCamera: definition.PerCamera,
			Included: item.Included, Enabled: item.Enabled, On: item.On(), Cameras: item.Cameras,
		}
		if definition.Status != features.StatusComingSoon {
			view.Activity30 = activityCount(definition.Key, alerts, sightings)
		}
		if definition.NeedsBox && reported {
			ready := capabilities[definition.Key]
			view.BoxReady = &ready
		}
		if setting, ok := snapshot.Settings[definition.Key]; ok {
			updated := setting.UpdatedAt
			view.UpdatedAt = &updated
		}
		if definition.Key == features.LiveView {
			view.LiveView = map[string]any{"limit_minutes": snapshot.LiveViewMinutes}
			if s.siteIngest.LiveMeter != nil {
				if usage, err := s.siteIngest.LiveMeter.Usage(ctx, tenantID); err == nil {
					view.LiveView["used_seconds"] = int64(usage.Used / time.Second)
				}
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Server) listFeatures(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	service, ok := s.featureService(w)
	if !ok {
		return
	}
	views, err := s.featureViews(r.Context(), service, tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Feature settings are unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "meta": map[string]any{"count": len(views)}})
}

func (s *Server) updateFeature(w http.ResponseWriter, r *http.Request) {
	// Switching features can stop security alerts, so only the workspace's
	// super admins (signed in with their authenticator) may do it.
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityTenantManage)
	if !ok {
		return
	}
	service, ok := s.featureService(w)
	if !ok {
		return
	}
	key := features.Key(r.PathValue("key"))
	if _, known := features.Lookup(key); !known {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if !decodeBodyLimit(w, r, &raw, 64<<10) {
		return
	}
	var enabled *bool
	var cameras []string
	setCameras := false
	if len(raw) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Nothing to change.")
		return
	}
	for field, value := range raw {
		switch field {
		case "enabled":
			var parsed bool
			if err := json.Unmarshal(value, &parsed); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "enabled must be true or false.")
				return
			}
			enabled = &parsed
		case "cameras":
			setCameras = true
			if strings.TrimSpace(string(value)) == "null" {
				continue
			}
			if err := json.Unmarshal(value, &cameras); err != nil || cameras == nil || len(cameras) > 1024 {
				writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "cameras must be a list of camera ids, or null for all cameras.")
				return
			}
			for _, cameraID := range cameras {
				if _, err := uuid.Parse(cameraID); err != nil {
					writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "cameras must be a list of camera ids, or null for all cameras.")
					return
				}
			}
		default:
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Unknown setting: "+field+".")
			return
		}
	}
	_, err := service.Update(r.Context(), tenantID, key, enabled, cameras, setCameras, principal.UserID, requestID)
	switch {
	case errors.Is(err, features.ErrNotIncluded):
		writeError(w, http.StatusForbidden, "FEATURE_NOT_INCLUDED", "This feature is not included in your plan.")
		return
	case errors.Is(err, features.ErrNotSwitchable):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "This feature cannot be changed that way.")
		return
	case errors.Is(err, features.ErrUnknownCamera):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "A chosen camera is not in this workspace.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Feature settings are unavailable.")
		return
	}
	views, err := s.featureViews(r.Context(), service, tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Feature settings are unavailable.")
		return
	}
	for _, view := range views {
		if view.Key == key {
			w.Header().Set(correlationHeader, requestID)
			writeJSON(w, http.StatusOK, map[string]any{"data": view})
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
}

// planFeatures fills each workspace's included plan features for the owner
// screen: the catalog default unless the owner chose otherwise.
func planFeatures(overrides map[string]bool) map[string]bool {
	result := map[string]bool{}
	for _, definition := range features.Catalog() {
		if !features.PlanControlled(definition.Key) {
			continue
		}
		included := features.DefaultIncluded(definition.Key)
		if value, ok := overrides[string(definition.Key)]; ok {
			included = value
		}
		result[string(definition.Key)] = included
	}
	return result
}

func (s *Server) adminSetFeatures(w http.ResponseWriter, r *http.Request) {
	principal, admin, ok := s.operator(w, r)
	if !ok {
		return
	}
	repository, ok := admin.(interface {
		SetPlanFeatures(ctx context.Context, actorID, requestID, tenantID string, changes map[string]bool) error
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Workspace administration is unavailable.")
		return
	}
	tenantID := r.PathValue("id")
	if _, err := uuid.Parse(tenantID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	requestID, err := correlationID(r.Header.Get(correlationHeader))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "X-Correlation-Id must be a UUIDv4.")
		return
	}
	var changes map[string]bool
	if !decodeBodyLimit(w, r, &changes, 4<<10) {
		return
	}
	if len(changes) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Nothing to change.")
		return
	}
	for key := range changes {
		if !features.PlanControlled(features.Key(key)) {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Unknown feature: "+key+".")
			return
		}
	}
	if err := repository.SetPlanFeatures(r.Context(), principal.UserID, requestID, tenantID, changes); err != nil {
		writeAdminError(w, err)
		return
	}
	s.siteIngest.Features.Invalidate(tenantID)
	w.Header().Set(correlationHeader, requestID)
	w.WriteHeader(http.StatusNoContent)
}

// siteFeatures is the effective feature state a site box applies, limited to
// that site's cameras. It returns nil when the feature system is not wired
// (local development), which older boxes and new ones treat as "all on".
func (s *Server) siteFeatures(ctx context.Context, tenantID string, siteCameras []siteCamera) (map[string]any, error) {
	if s.siteIngest.Features == nil {
		return nil, nil
	}
	effective, err := s.siteIngest.Features.Effective(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	onSite := map[string]struct{}{}
	for _, camera := range siteCameras {
		onSite[camera.ID] = struct{}{}
	}
	result := map[string]any{}
	for _, definition := range features.Catalog() {
		if definition.Status == features.StatusComingSoon {
			continue
		}
		item := effective[definition.Key]
		entry := map[string]any{"on": item.On(), "cameras": nil}
		if item.Cameras != nil {
			chosen := []string{}
			for _, cameraID := range item.Cameras {
				if _, ok := onSite[cameraID]; ok {
					chosen = append(chosen, cameraID)
				}
			}
			entry["cameras"] = chosen
		}
		result[string(definition.Key)] = entry
	}
	return result, nil
}

// featureAllows is the ingest guard. If feature state cannot be read the
// event is let through: a missed intrusion is worse than an unwanted alert.
func (s *Server) featureAllows(ctx context.Context, tenantID string, key features.Key, eventType, cameraID string) bool {
	if s.siteIngest.Features == nil {
		return true
	}
	var allowed bool
	var err error
	if eventType != "" {
		allowed, err = s.siteIngest.Features.AllowsEvent(ctx, tenantID, eventType, cameraID)
	} else {
		allowed, err = s.siteIngest.Features.Allows(ctx, tenantID, key, cameraID)
	}
	if err != nil {
		log.Printf("feature settings unavailable; letting the event through: %v", err)
		return true
	}
	return allowed
}

func writeFeatureOff(w http.ResponseWriter, id string) {
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"id": id, "ignored": "feature_off"}})
}
