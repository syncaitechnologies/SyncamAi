package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/configdelivery"
)

const configStatusBodyLimit = 4 << 10

type deviceConfigurationStatusRequest struct {
	Revision     int64  `json:"revision"`
	State        string `json:"state"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func (s *Server) pushZoneConfiguration(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.zoneRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.configuration == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	zone, ok := s.authorizedZone(w, r, principal, tenantID)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	listed, err := s.zones.List(r.Context(), tenantID, zone.SiteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return
	}
	payload, err := json.Marshal(map[string]any{"zones": listed})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	revision, err := s.configuration.Publish(r.Context(), configdelivery.PublishCommand{TenantID: tenantID, SiteID: zone.SiteID, ActorID: principal.UserID, RequestID: requestID, Payload: payload})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": revision, "meta": map[string]any{"delivery": "edge pull with heartbeat revision hint"}})
}

func (s *Server) listConfigurationRevisions(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.configurationRequestContext(w, r, authz.CapabilityConfigRead)
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
	listed, err := s.configuration.List(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	visible := make([]configdelivery.Revision, 0, len(listed))
	for _, revision := range listed {
		if authz.CanAccessSite(principal, tenantID, revision.SiteID) {
			visible = append(visible, revision)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

func (s *Server) getDeviceConfigurationStatus(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.configurationRequestContext(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if _, err := uuid.Parse(deviceID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	status, err := s.configuration.GetStatus(r.Context(), tenantID, deviceID)
	if errors.Is(err, configdelivery.ErrDeviceNotFound) || (err == nil && !authz.CanAccessSite(principal, tenantID, status.SiteID)) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}

func (s *Server) pullDeviceConfiguration(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.verifiedDeviceRequest(w, r)
	if !ok {
		return
	}
	if s.configuration == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_revision")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "after_revision must be a non-negative integer.")
			return
		}
		after = parsed
	}
	result, err := s.configuration.Pull(r.Context(), deviceID, after)
	if errors.Is(err, configdelivery.ErrDeviceNotFound) {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	if result.Revision == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result.Revision})
}

func (s *Server) reportDeviceConfiguration(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.verifiedDeviceRequest(w, r)
	if !ok {
		return
	}
	if s.configuration == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	if _, ok := correlationHeaderOnly(w, r); !ok {
		return
	}
	var input deviceConfigurationStatusRequest
	if !decodeBodyLimit(w, r, &input, configStatusBodyLimit) {
		return
	}
	input.State, input.ErrorMessage = strings.TrimSpace(input.State), strings.TrimSpace(input.ErrorMessage)
	status, err := s.configuration.Report(r.Context(), configdelivery.ReportCommand{DeviceID: deviceID, Revision: input.Revision, State: input.State, ErrorMessage: input.ErrorMessage})
	if errors.Is(err, configdelivery.ErrDeviceNotFound) {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	}
	if errors.Is(err, configdelivery.ErrRevisionNotFound) || errors.Is(err, configdelivery.ErrInvalidStatus) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Configuration status fields are invalid.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}

func (s *Server) verifiedDeviceRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.deviceVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device heartbeat service unavailable.")
		return "", false
	}
	verifiedDeviceID, err := s.deviceVerifier.VerifyDevice(r)
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if err != nil || verifiedDeviceID != deviceID {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return "", false
	}
	return deviceID, true
}

func (s *Server) configurationRequestContext(w http.ResponseWriter, r *http.Request, capability authz.Capability) (identityPrincipal, string, bool) {
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
	if s.configuration == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration delivery unavailable.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}
