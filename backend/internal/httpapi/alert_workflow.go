package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
)

func (s *Server) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	tenantID, err := requestTenant(r, principal)
	if errors.Is(err, authz.ErrDenied) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Tenant header is required.")
		return
	}
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Alert repository unavailable.")
		return
	}
	alertID := strings.TrimSpace(r.PathValue("id"))
	if _, err := uuid.Parse(alertID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	alert, err := s.alerts.Get(r.Context(), tenantID, alertID)
	if errors.Is(err, alerting.ErrAlertNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Alert repository unavailable.")
		return
	}
	if err := authz.Authorize(principal, authz.Request{
		Capability: authz.CapabilityAlertsWrite, TenantID: tenantID, SiteID: alert.SiteID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "A valid Idempotency-Key is required.")
		return
	}
	requestID, err := correlationID(r.Header.Get(correlationHeader))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "X-Correlation-Id must be a UUIDv4.")
		return
	}
	w.Header().Set(correlationHeader, requestID)
	result, err := s.alerts.Acknowledge(r.Context(), alerting.AcknowledgeCommand{
		TenantID: tenantID, SiteID: alert.SiteID, AlertID: alert.ID,
		ActorID: principal.UserID, RequestID: requestID, IdempotencyKey: idempotencyKey,
	})
	switch {
	case errors.Is(err, alerting.ErrAlertNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	case errors.Is(err, alerting.ErrAlertStateConflict):
		writeError(w, http.StatusConflict, "STATE_CONFLICT", "Alert is no longer unacknowledged.")
		return
	case errors.Is(err, alerting.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Idempotency-Key was already used for a different request.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Alert repository unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result.Alert})
}
