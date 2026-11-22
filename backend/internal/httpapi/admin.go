package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

// Owner admin screen (ADR-018): platform operators, signed in with their
// authenticator (aal2), list every workspace and change its plan, trial end,
// limits and live-view minutes. The database checks the operator list again
// and writes each change to the workspace's audit log.
func (s *Server) registerAdminRoutes() {
	s.mux.Handle("GET /v1/admin/workspaces", s.authenticate(http.HandlerFunc(s.adminListWorkspaces)))
	s.mux.Handle("PATCH /v1/admin/workspaces/{id}", s.authenticate(http.HandlerFunc(s.adminUpdateWorkspace)))
}

const (
	maxAdminLimit       = 100_000
	maxAdminLiveMinutes = 1_000_000
)

func (s *Server) operator(w http.ResponseWriter, r *http.Request) (identity.Principal, tenant.AdminRepository, bool) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return identity.Principal{}, nil, false
	}
	if !principal.Operator || !strings.EqualFold(principal.MFALevel, "aal2") {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Owner access with authenticator sign-in is required.")
		return identity.Principal{}, nil, false
	}
	admin, ok := s.tenants.(tenant.AdminRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Workspace administration is unavailable.")
		return identity.Principal{}, nil, false
	}
	return principal, admin, true
}

func writeAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tenant.ErrNotOperator):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Owner access is required.")
	case errors.Is(err, tenant.ErrAdminInvalid):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "That change is not valid.")
	case errors.Is(err, tenant.ErrWorkspaceNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Workspace administration is unavailable.")
	}
}

func (s *Server) adminListWorkspaces(w http.ResponseWriter, r *http.Request) {
	principal, admin, ok := s.operator(w, r)
	if !ok {
		return
	}
	workspaces, err := admin.ListWorkspaces(r.Context(), principal.UserID)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	if lister, ok := admin.(tenant.AdminFeatureRepository); ok {
		overrides, err := lister.ListPlanFeatures(r.Context(), principal.UserID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		for index := range workspaces {
			workspaces[index].Features = planFeatures(overrides[workspaces[index].TenantID])
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": workspaces, "meta": map[string]any{"count": len(workspaces)}})
}

// adminChanges validates a PATCH body: only known settings, each with a valid
// value. JSON null means unlimited for the limits.
func adminChanges(raw map[string]json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, errors.New("Nothing to change.")
	}
	changes := map[string]any{}
	for key, value := range raw {
		isNull := strings.TrimSpace(string(value)) == "null"
		switch key {
		case "plan":
			var plan string
			if err := json.Unmarshal(value, &plan); err != nil || (plan != identity.PlanTrial && plan != identity.PlanActive && plan != identity.PlanSuspended) {
				return nil, errors.New("Plan must be trial, active or suspended.")
			}
			changes[key] = plan
		case "trial_ends_at":
			if isNull {
				changes[key] = nil
				continue
			}
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, errors.New("Trial end must be a date and time.")
			}
			at, err := time.Parse(time.RFC3339, text)
			if err != nil {
				return nil, errors.New("Trial end must be a date and time.")
			}
			changes[key] = at.UTC().Format(time.RFC3339)
		case "site_limit", "camera_limit", "live_view_minutes":
			if isNull {
				changes[key] = nil
				continue
			}
			var number int
			maximum := maxAdminLimit
			if key == "live_view_minutes" {
				maximum = maxAdminLiveMinutes
			}
			if err := json.Unmarshal(value, &number); err != nil || number < 0 || number > maximum {
				return nil, errors.New("Limits must be whole numbers of zero or more, or empty for unlimited.")
			}
			changes[key] = number
		default:
			return nil, errors.New("Unknown setting: " + key + ".")
		}
	}
	if plan, ok := changes["plan"]; ok && plan == identity.PlanTrial {
		if end, set := changes["trial_ends_at"]; set && end == nil {
			return nil, errors.New("A trial needs an end date.")
		}
	}
	return changes, nil
}

func (s *Server) adminUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, admin, ok := s.operator(w, r)
	if !ok {
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
	var raw map[string]json.RawMessage
	if !decodeBodyLimit(w, r, &raw, 4<<10) {
		return
	}
	changes, err := adminChanges(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
		return
	}
	if err := admin.UpdateWorkspace(r.Context(), principal.UserID, requestID, tenantID, changes); err != nil {
		writeAdminError(w, err)
		return
	}
	w.Header().Set(correlationHeader, requestID)
	w.WriteHeader(http.StatusNoContent)
}
