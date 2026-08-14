package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

const heartbeatBodyLimit = 64 << 10

type deviceHeartbeatRequest struct {
	HeartbeatID       string    `json:"heartbeat_id"`
	ReportedAt        time.Time `json:"reported_at"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
	StoreForwardDepth int64     `json:"store_forward_depth"`
	FirmwareVersion   string    `json:"firmware_version"`
}

func (s *Server) listEdgeDevices(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.deviceStatusRequestContext(w, r)
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
	observedAt := time.Now().UTC()
	devices := make([]device.EdgeDevice, 0)
	if siteID != "" || principal.HasRole(identity.RoleSuperAdmin) {
		listed, err := s.deviceStatus.ListDevices(r.Context(), tenantID, siteID, observedAt)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device status repository unavailable.")
			return
		}
		devices = append(devices, listed...)
	} else {
		for _, authorizedSiteID := range principal.SiteIDs {
			listed, err := s.deviceStatus.ListDevices(r.Context(), tenantID, authorizedSiteID, observedAt)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device status repository unavailable.")
				return
			}
			devices = append(devices, listed...)
		}
	}
	visible := make([]device.EdgeDevice, 0, len(devices))
	for _, edgeDevice := range devices {
		if edgeDevice.TenantID == tenantID && authz.CanAccessSite(principal, tenantID, edgeDevice.SiteID) {
			visible = append(visible, edgeDevice)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil, "observed_at": observedAt}})
}

func (s *Server) recordDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	if s.deviceVerifier == nil || s.deviceStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device heartbeat service unavailable.")
		return
	}
	verifiedDeviceID, err := s.deviceVerifier.VerifyDevice(r)
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if err != nil || verifiedDeviceID != deviceID {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	}
	if _, ok := correlationHeaderOnly(w, r); !ok {
		return
	}
	var input deviceHeartbeatRequest
	if !decodeBodyLimit(w, r, &input, heartbeatBodyLimit) {
		return
	}
	input.HeartbeatID = strings.TrimSpace(input.HeartbeatID)
	input.FirmwareVersion = strings.TrimSpace(input.FirmwareVersion)
	heartbeatID, parseErr := uuid.Parse(input.HeartbeatID)
	if parseErr != nil || heartbeatID.Version() != 4 || input.ReportedAt.IsZero() || input.ReportedAt.After(time.Now().UTC().Add(5*time.Minute)) || input.UptimeSeconds < 0 || input.UptimeSeconds > 3155760000 || input.StoreForwardDepth < 0 || input.StoreForwardDepth > 1000000000 || input.FirmwareVersion == "" || len(input.FirmwareVersion) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Device heartbeat fields are invalid.")
		return
	}
	result, err := s.deviceStatus.RecordHeartbeat(r.Context(), device.HeartbeatCommand{
		DeviceID: deviceID, HeartbeatID: heartbeatID.String(), ReportedAt: input.ReportedAt,
		UptimeSeconds: input.UptimeSeconds, StoreForwardDepth: input.StoreForwardDepth, FirmwareVersion: input.FirmwareVersion,
	})
	switch {
	case errors.Is(err, device.ErrDeviceUnauthorized):
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	case errors.Is(err, device.ErrHeartbeatConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Heartbeat identifier was already used for different telemetry.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device heartbeat service unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) deviceStatusRequestContext(w http.ResponseWriter, r *http.Request) (identityPrincipal, string, bool) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigRead, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identityPrincipal{}, "", false
	}
	if s.deviceStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device status repository unavailable.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}

func decodeBodyLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
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
