package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/privacymasks"
)

type createPrivacyMaskRequest struct {
	SiteID   string          `json:"site_id"`
	CameraID string          `json:"camera_id"`
	Name     string          `json:"name"`
	Geometry json.RawMessage `json:"geometry"`
}

func (s *Server) createPrivacyMaskRequest(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.privacyMaskContext(w, r)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	var input createPrivacyMaskRequest
	if !decodeBody(w, r, &input) {
		return
	}
	if !validPrivacyMaskRequest(input) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Privacy mask request fields or geometry are invalid.")
		return
	}
	request, err := s.privacyMasks.Create(r.Context(), privacymasks.CreateCommand{TenantID: tenantID, SiteID: strings.TrimSpace(input.SiteID), CameraID: strings.TrimSpace(input.CameraID), ActorID: principal.UserID, RequestID: requestID, Name: strings.TrimSpace(input.Name), Geometry: input.Geometry})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Privacy mask request is invalid.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": request})
}

func (s *Server) getPrivacyMaskRequest(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.privacyMaskContext(w, r)
	if !ok {
		return
	}
	request, err := s.privacyMasks.Get(r.Context(), tenantID, r.PathValue("id"))
	if errors.Is(err, privacymasks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy mask repository unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": request})
}

func (s *Server) approvePrivacyMaskRequest(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.privacyMaskContext(w, r)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	request, err := s.privacyMasks.Approve(r.Context(), privacymasks.ApproveCommand{TenantID: tenantID, RequestID: r.PathValue("id"), ActorID: principal.UserID, AuditRequestID: requestID})
	if errors.Is(err, privacymasks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if errors.Is(err, privacymasks.ErrRequesterCannotApprove) || errors.Is(err, privacymasks.ErrAlreadyApproved) {
		writeError(w, http.StatusConflict, "APPROVAL_CONFLICT", "Privacy mask approval is not permitted.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy mask repository unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": request})
}

func (s *Server) privacyMaskContext(w http.ResponseWriter, r *http.Request) (identityPrincipal, string, bool) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return identityPrincipal{}, "", false
	}
	tenantID, err := requestTenant(r, principal)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identityPrincipal{}, "", false
	}
	if s.privacyMasks == nil || authz.Authorize(principal, authz.Request{Capability: authz.CapabilityPrivacyMaskApprove, TenantID: tenantID}) != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}

func validPrivacyMaskRequest(input createPrivacyMaskRequest) bool {
	if _, err := uuid.Parse(strings.TrimSpace(input.SiteID)); err != nil {
		return false
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.CameraID)); err != nil || len(strings.TrimSpace(input.Name)) == 0 || len(strings.TrimSpace(input.Name)) > 120 {
		return false
	}
	return validGeometry(input.Geometry, "intrusion")
}
