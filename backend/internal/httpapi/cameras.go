package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

type createCameraRequest struct {
	SiteID       string   `json:"site_id"`
	SerialNumber string   `json:"serial_number"`
	Name         string   `json:"name"`
	GroupName    string   `json:"group_name"`
	Tags         []string `json:"tags"`
}

type updateCameraRequest struct {
	ConfigVersion   int64     `json:"config_version"`
	Name            *string   `json:"name"`
	GroupName       *string   `json:"group_name"`
	Tags            *[]string `json:"tags"`
	LifecycleStatus *string   `json:"lifecycle_status"`
}

func (s *Server) listCameras(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.cameraRequestContext(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	siteID := strings.TrimSpace(r.URL.Query().Get("site_id"))
	if siteID != "" {
		if _, err := uuid.Parse(siteID); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "site_id must be a UUID.")
			return
		}
		if !authz.CanAccessSite(principal, tenantID, siteID) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return
		}
	}
	cameras := make([]device.Camera, 0)
	if siteID != "" || principal.HasRole(identity.RoleSuperAdmin) {
		listed, err := s.cameras.List(r.Context(), tenantID, siteID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
			return
		}
		cameras = append(cameras, listed...)
	} else {
		// Apply site scope in the repository query before its limit. Filtering a
		// tenant-wide limited result afterward could hide authorized cameras.
		for _, authorizedSiteID := range principal.SiteIDs {
			listed, err := s.cameras.List(r.Context(), tenantID, authorizedSiteID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
				return
			}
			cameras = append(cameras, listed...)
		}
	}
	visible := make([]device.Camera, 0, len(cameras))
	for _, camera := range cameras {
		if camera.TenantID == tenantID && authz.CanAccessSite(principal, tenantID, camera.SiteID) {
			visible = append(visible, camera)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

func (s *Server) getCamera(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.cameraRequestContext(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	cameraID := r.PathValue("id")
	if _, err := uuid.Parse(cameraID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	camera, err := s.cameras.Get(r.Context(), tenantID, cameraID)
	if errors.Is(err, device.ErrCameraNotFound) || (err == nil && !authz.CanAccessSite(principal, tenantID, camera.SiteID)) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": camera})
}

func (s *Server) createCamera(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.cameraRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	idempotencyKey, requestID, ok := mutationHeaders(w, r)
	if !ok {
		return
	}
	var input createCameraRequest
	if !decodeBody(w, r, &input) {
		return
	}
	normalizeCreateCamera(&input)
	if !validCameraCreate(input) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Camera fields are invalid.")
		return
	}
	if !authz.CanAccessSite(principal, tenantID, input.SiteID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	result, err := s.cameras.Create(r.Context(), device.CreateCameraCommand{
		TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, IdempotencyKey: idempotencyKey,
		SiteID: input.SiteID, SerialNumber: input.SerialNumber, Name: input.Name, GroupName: input.GroupName, Tags: input.Tags,
	})
	switch {
	case errors.Is(err, device.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Idempotency-Key was already used for a different request.")
		return
	case errors.Is(err, device.ErrSerialConflict):
		writeError(w, http.StatusConflict, "RESOURCE_CONFLICT", "Camera serial number already exists for this tenant.")
		return
	case errors.Is(err, device.ErrSiteNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": result.Camera})
}

func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.cameraRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	camera, ok := s.authorizedCamera(w, r, principal, tenantID)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	var input updateCameraRequest
	if !decodeBody(w, r, &input) {
		return
	}
	if !validCameraUpdate(&input) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Camera update fields are invalid.")
		return
	}
	updated, err := s.cameras.Update(r.Context(), device.UpdateCameraCommand{
		TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, CameraID: camera.ID,
		ExpectedVersion: input.ConfigVersion, Name: input.Name, GroupName: input.GroupName, Tags: input.Tags, LifecycleStatus: input.LifecycleStatus,
	})
	switch {
	case errors.Is(err, device.ErrCameraNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
	case errors.Is(err, device.ErrVersionConflict), errors.Is(err, device.ErrLifecycleConflict):
		writeError(w, http.StatusConflict, "RESOURCE_CONFLICT", "Camera version or lifecycle state conflicts with this update.")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"data": updated})
	}
}

func (s *Server) retireCamera(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.cameraRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	camera, ok := s.authorizedCamera(w, r, principal, tenantID)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	retired, err := s.cameras.Retire(r.Context(), device.RetireCameraCommand{TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, CameraID: camera.ID})
	if errors.Is(err, device.ErrCameraNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": retired})
}

func (s *Server) cameraRequestContext(w http.ResponseWriter, r *http.Request, capability authz.Capability) (identityPrincipal, string, bool) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return identityPrincipal{}, "", false
	}
	tenantID, err := requestTenant(r, principal)
	if errors.Is(err, authz.ErrDenied) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identityPrincipal{}, "", false
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Tenant header is required.")
		return identityPrincipal{}, "", false
	}
	if err := authz.Authorize(principal, authz.Request{Capability: capability, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identityPrincipal{}, "", false
	}
	if s.cameras == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}

// Alias keeps the helper signature compact while retaining the verified principal type.
type identityPrincipal = identity.Principal

func (s *Server) authorizedCamera(w http.ResponseWriter, r *http.Request, principal identityPrincipal, tenantID string) (device.Camera, bool) {
	cameraID := r.PathValue("id")
	if _, err := uuid.Parse(cameraID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return device.Camera{}, false
	}
	camera, err := s.cameras.Get(r.Context(), tenantID, cameraID)
	if errors.Is(err, device.ErrCameraNotFound) || (err == nil && !authz.CanAccessSite(principal, tenantID, camera.SiteID)) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return device.Camera{}, false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Camera repository unavailable.")
		return device.Camera{}, false
	}
	return camera, true
}

func mutationHeaders(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" || len(key) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "A valid Idempotency-Key is required.")
		return "", "", false
	}
	requestID, ok := correlationHeaderOnly(w, r)
	return key, requestID, ok
}

func correlationHeaderOnly(w http.ResponseWriter, r *http.Request) (string, bool) {
	requestID, err := correlationID(r.Header.Get(correlationHeader))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "X-Correlation-Id must be a UUIDv4.")
		return "", false
	}
	w.Header().Set(correlationHeader, requestID)
	return requestID, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body is invalid.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body must contain one JSON object.")
		return false
	}
	return true
}

func normalizeCreateCamera(input *createCameraRequest) {
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.SerialNumber = strings.TrimSpace(input.SerialNumber)
	input.Name = strings.TrimSpace(input.Name)
	input.GroupName = strings.TrimSpace(input.GroupName)
}

func validCameraCreate(input createCameraRequest) bool {
	if _, err := uuid.Parse(input.SiteID); err != nil {
		return false
	}
	return len(input.SerialNumber) > 0 && len(input.SerialNumber) <= 128 && len(input.Name) > 0 && len(input.Name) <= 120 && len(input.GroupName) <= 120 && validTags(input.Tags)
}

func validCameraUpdate(input *updateCameraRequest) bool {
	if input.ConfigVersion <= 0 || (input.Name == nil && input.GroupName == nil && input.Tags == nil && input.LifecycleStatus == nil) {
		return false
	}
	if input.Name != nil {
		*input.Name = strings.TrimSpace(*input.Name)
		if *input.Name == "" || len(*input.Name) > 120 {
			return false
		}
	}
	if input.GroupName != nil {
		*input.GroupName = strings.TrimSpace(*input.GroupName)
		if len(*input.GroupName) > 120 {
			return false
		}
	}
	if input.Tags != nil && !validTags(*input.Tags) {
		return false
	}
	if input.LifecycleStatus != nil {
		*input.LifecycleStatus = strings.TrimSpace(*input.LifecycleStatus)
		if !map[string]bool{"pending": true, "active": true, "offline": true}[*input.LifecycleStatus] {
			return false
		}
	}
	return true
}

func validTags(tags []string) bool {
	if len(tags) > 32 {
		return false
	}
	for _, tag := range tags {
		if len(strings.TrimSpace(tag)) == 0 || len(strings.TrimSpace(tag)) > 64 {
			return false
		}
	}
	return true
}
