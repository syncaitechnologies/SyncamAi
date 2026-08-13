package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
)

type issueDeviceClaimRequest struct {
	SiteID       string `json:"site_id"`
	SerialNumber string `json:"serial_number"`
	HardwareTier string `json:"hardware_tier"`
	Model        string `json:"model"`
}

type activateDeviceRequest struct {
	ClaimToken   string `json:"claim_token"`
	SerialNumber string `json:"serial_number"`
}

func (s *Server) issueDeviceClaim(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.enrollmentRequestContext(w, r)
	if !ok {
		return
	}
	idempotencyKey, requestID, ok := mutationHeaders(w, r)
	if !ok {
		return
	}
	var input issueDeviceClaimRequest
	if !decodeBody(w, r, &input) {
		return
	}
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.SerialNumber = strings.TrimSpace(input.SerialNumber)
	input.HardwareTier = strings.ToLower(strings.TrimSpace(input.HardwareTier))
	input.Model = strings.TrimSpace(input.Model)
	if _, err := uuid.Parse(input.SiteID); err != nil || len(input.SerialNumber) == 0 || len(input.SerialNumber) > 128 || !map[string]bool{"s": true, "m": true, "l": true}[input.HardwareTier] || len(input.Model) > 120 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Device claim fields are invalid.")
		return
	}
	if !authz.CanAccessSite(principal, tenantID, input.SiteID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	result, err := s.enrollment.IssueClaim(r.Context(), device.IssueClaimCommand{
		TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, IdempotencyKey: idempotencyKey,
		SiteID: input.SiteID, SerialNumber: input.SerialNumber, HardwareTier: input.HardwareTier, Model: input.Model,
	})
	switch {
	case errors.Is(err, device.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Idempotency-Key was already used for a different request.")
		return
	case errors.Is(err, device.ErrDeviceSerialConflict):
		writeError(w, http.StatusConflict, "RESOURCE_CONFLICT", "Edge device serial number already exists for this tenant.")
		return
	case errors.Is(err, device.ErrSiteNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device enrollment repository unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": result})
}

func (s *Server) activateDevice(w http.ResponseWriter, r *http.Request) {
	if s.enrollment == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device enrollment repository unavailable.")
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	var input activateDeviceRequest
	if !decodeBody(w, r, &input) {
		return
	}
	input.ClaimToken = strings.TrimSpace(input.ClaimToken)
	input.SerialNumber = strings.TrimSpace(input.SerialNumber)
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if _, err := uuid.Parse(deviceID); err != nil || len(input.ClaimToken) < 80 || len(input.ClaimToken) > 256 || len(input.SerialNumber) == 0 || len(input.SerialNumber) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Device activation fields are invalid.")
		return
	}
	activated, err := s.enrollment.Activate(r.Context(), device.ActivateDeviceCommand{DeviceID: deviceID, ClaimToken: input.ClaimToken, SerialNumber: input.SerialNumber, RequestID: requestID})
	if errors.Is(err, device.ErrClaimInvalid) || errors.Is(err, device.ErrClaimExpired) || errors.Is(err, device.ErrClaimConsumed) || errors.Is(err, device.ErrClaimSerialMismatch) {
		writeError(w, http.StatusUnauthorized, "DEVICE_CLAIM_INVALID", "Device claim is invalid or unavailable.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device enrollment repository unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": activated})
}

func (s *Server) enrollmentRequestContext(w http.ResponseWriter, r *http.Request) (identityPrincipal, string, bool) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identityPrincipal{}, "", false
	}
	if s.enrollment == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Device enrollment repository unavailable.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}
