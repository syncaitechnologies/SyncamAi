package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

// Self-service workspace registration (ADR-016): a verified signed-in user with
// no membership creates one workspace and becomes its Super Admin.
func (s *Server) registerRegistrationRoutes() {
	s.mux.HandleFunc("POST /v1/tenants", s.registerWorkspace)
}

type registerWorkspaceRequest struct {
	Company  string `json:"company"`
	SiteName string `json:"site_name"`
	Timezone string `json:"timezone"`
}

func writeTrialEnded(w http.ResponseWriter) {
	writeError(w, http.StatusPaymentRequired, "TRIAL_ENDED", "Your free trial has ended. Contact SyncCam to keep using the service.")
}

func (s *Server) registerWorkspace(w http.ResponseWriter, r *http.Request) {
	verifier, ok := s.verifier.(identity.IdentityVerifier)
	registrar, registrarOK := s.tenants.(tenant.Registrar)
	if !ok || !registrarOK {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Workspace registration is unavailable.")
		return
	}
	authorization := strings.Fields(r.Header.Get("Authorization"))
	if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") || authorization[1] == "" {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	user, err := verifier.VerifyIdentity(r.Context(), authorization[1])
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	// One workspace per user is enforced by the database; this bounds abuse.
	if !s.registrationLimiter.allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many attempts. Try again later.")
		return
	}
	// A token that already carries workspace claims belongs to an existing
	// member; the routine returns their workspace. An unverified email may not
	// create one.
	if !user.HasWorkspace && !user.EmailVerified {
		writeError(w, http.StatusForbidden, "EMAIL_NOT_VERIFIED", "Verify your email address before creating a workspace.")
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

	var input registerWorkspaceRequest
	if !decodeBodyLimit(w, r, &input, 8<<10) {
		return
	}
	input.Company = strings.TrimSpace(input.Company)
	input.SiteName = strings.TrimSpace(input.SiteName)
	input.Timezone = strings.TrimSpace(input.Timezone)
	if !validName(input.Company) || !validName(input.SiteName) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Company and site names must be 2 to 120 characters.")
		return
	}
	if input.Timezone == "" || len(input.Timezone) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Timezone must be an IANA timezone.")
		return
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Timezone must be an IANA timezone.")
		return
	}

	result, err := registrar.RegisterWorkspace(r.Context(), tenant.RegisterWorkspaceCommand{
		UserID: user.UserID, RequestID: requestID,
		Company: input.Company, SiteName: input.SiteName, Timezone: input.Timezone,
	})
	switch {
	case errors.Is(err, tenant.ErrRegistrationInvalid):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Workspace details are invalid.")
		return
	case errors.Is(err, tenant.ErrMembershipInactive):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Workspace registration is unavailable.")
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"data": result})
}

func validName(value string) bool {
	length := utf8.RuneCountInString(value)
	return length >= 2 && length <= 120
}
