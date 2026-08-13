// Package httpapi exposes the initial control-plane REST boundary.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

const tenantHeader = "X-SentinelVision-Tenant-ID"

const (
	idempotencyHeader  = "Idempotency-Key"
	correlationHeader  = "X-Correlation-Id"
	eventRequestHeader = "X-SentinelVision-Request-ID"
)

type principalContextKey struct{}

// Server owns the first tenant-safe control-plane routes.
type Server struct {
	verifier identity.Verifier
	tenants  tenant.Repository
	events   eventing.Repository
	alerts   alerting.Repository
	realtime realtime.Repository
	tickets  realtime.TicketStore
	mux      *http.ServeMux
}

// New builds a fail-closed HTTP handler around explicit dependencies.
func New(verifier identity.Verifier, tenants tenant.Repository, eventRepositories ...eventing.Repository) http.Handler {
	var events eventing.Repository
	if len(eventRepositories) > 0 {
		events = eventRepositories[0]
	}
	return NewWithAlerts(verifier, tenants, events, nil)
}

// NewWithAlerts wires the complete local walking-skeleton dependencies.
func NewWithAlerts(verifier identity.Verifier, tenants tenant.Repository, events eventing.Repository, alerts alerting.Repository) http.Handler {
	return NewWithRealtime(verifier, tenants, events, alerts, nil, nil)
}

// NewWithRealtime wires the complete local control-plane and realtime boundary.
func NewWithRealtime(verifier identity.Verifier, tenants tenant.Repository, events eventing.Repository, alerts alerting.Repository, realtimeRepository realtime.Repository, tickets realtime.TicketStore) http.Handler {
	server := &Server{verifier: verifier, tenants: tenants, events: events, alerts: alerts, realtime: realtimeRepository, tickets: tickets, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.Handle("GET /v1/auth/me", server.authenticate(http.HandlerFunc(server.me)))
	server.mux.Handle("POST /v1/auth/ws-ticket", server.authenticate(http.HandlerFunc(server.issueRealtimeTicket)))
	server.mux.Handle("GET /v1/sites", server.authenticate(http.HandlerFunc(server.listSites)))
	server.mux.Handle("POST /v1/sites", server.authenticate(http.HandlerFunc(server.createSite)))
	server.mux.Handle("POST /v1/events", server.authenticate(http.HandlerFunc(server.ingestEvent)))
	server.mux.Handle("GET /v1/alerts", server.authenticate(http.HandlerFunc(server.listAlerts)))
	server.mux.Handle("POST /v1/alerts/{id}/acknowledge", server.authenticate(http.HandlerFunc(server.acknowledgeAlert)))
	server.mux.HandleFunc("GET /ws/v1/alerts", server.streamAlerts)
	return server.mux
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityAlertsRead, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Alert repository unavailable.")
		return
	}
	queue, err := s.alerts.List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Alert repository unavailable.")
		return
	}
	visible := make([]alerting.Alert, 0, len(queue))
	for _, alert := range queue {
		if alert.TenantID == tenantID && authz.CanAccessSite(principal, tenantID, alert.SiteID) {
			visible = append(visible, alert)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

var eventTypes = map[string]struct{}{
	"camera_health": {}, "intrusion": {}, "restricted_zone": {}, "loitering": {},
	"vehicle_activity": {}, "weapon_review": {}, "fire_review": {}, "smoke_review": {},
	"attendance_review": {}, "ppe_review": {}, "fall_review": {}, "fight_review": {},
	"abandoned_object_review": {},
}

func (s *Server) ingestEvent(w http.ResponseWriter, r *http.Request) {
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
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Event repository unavailable.")
		return
	}
	requestID := strings.TrimSpace(r.Header.Get(eventRequestHeader))
	parsedRequestID, err := uuid.Parse(requestID)
	if err != nil || parsedRequestID.Version() != 4 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "X-SentinelVision-Request-ID must be a UUIDv4.")
		return
	}

	var event eventing.DetectionEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Event body is invalid.")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body must contain one JSON object.")
		return
	}
	if err := validateDetectionEvent(event, tenantID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
		return
	}
	if err := authz.Authorize(principal, authz.Request{
		Capability: authz.CapabilityEventsWrite, TenantID: tenantID, SiteID: event.SiteID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}

	result, err := s.events.Ingest(r.Context(), eventing.IngestCommand{
		ActorID: principal.UserID, RequestID: requestID, Event: event,
	})
	switch {
	case errors.Is(err, eventing.ErrDedupeConflict), errors.Is(err, eventing.ErrEventConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Event identity was already used for different content.")
		return
	case errors.Is(err, eventing.ErrSiteNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Event repository unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": result})
}

func validateDetectionEvent(event eventing.DetectionEvent, tenantID string) error {
	identifiers := []string{event.EventID, event.TenantID, event.SiteID, event.CameraID, event.ZoneID}
	for _, identifier := range identifiers {
		if _, err := uuid.Parse(identifier); err != nil {
			return errors.New("Event identifiers must be UUIDs.")
		}
	}
	if event.TenantID != tenantID {
		return errors.New("Event tenant must match the verified tenant.")
	}
	if len(strings.TrimSpace(event.DedupeKey)) == 0 || len(event.DedupeKey) > 256 || event.OccurredAt.IsZero() {
		return errors.New("Event dedupe key and occurrence time are required.")
	}
	if _, ok := eventTypes[event.EventType]; !ok || len(strings.TrimSpace(event.ModelVersion)) == 0 || len(event.ModelVersion) > 128 {
		return errors.New("Event type or model version is invalid.")
	}
	if event.Confidence < 0 || event.Confidence > 1 || !event.RequiresHumanReview || event.ReviewState != "pending" {
		return errors.New("New probabilistic events must be pending human review.")
	}
	if len(event.EvidenceRefs) > 32 {
		return errors.New("Event evidence references are invalid.")
	}
	for _, ref := range event.EvidenceRefs {
		if len(strings.TrimSpace(ref)) == 0 || len(ref) > 1024 {
			return errors.New("Event evidence references are invalid.")
		}
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.verifier == nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
			return
		}
		authorization := strings.Fields(r.Header.Get("Authorization"))
		if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") || authorization[1] == "" {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
			return
		}
		principal, err := s.verifier.Verify(r.Context(), authorization[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func principalFromContext(ctx context.Context) (identity.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(identity.Principal)
	return principal, ok
}

func requestTenant(r *http.Request, principal identity.Principal) (string, error) {
	tenantID := strings.TrimSpace(r.Header.Get(tenantHeader))
	if tenantID == "" {
		return "", errors.New("tenant header required")
	}
	if tenantID != principal.TenantID {
		return "", authz.ErrDenied
	}
	return tenantID, nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityAuthRead, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"id": principal.UserID, "email": principal.Email, "tenant_id": principal.TenantID,
		"site_ids": principal.SiteIDs, "roles": principal.Roles, "scopes": principal.Scopes,
		"data_class": principal.DataClasses, "mfa_level": principal.MFALevel,
	}})
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilitySitesRead, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	if s.tenants == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Tenant repository unavailable.")
		return
	}
	sites, err := s.tenants.ListSites(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Tenant repository unavailable.")
		return
	}
	visible := make([]tenant.Site, 0, len(sites))
	for _, site := range sites {
		if site.TenantID == tenantID && authz.CanAccessSite(principal, tenantID, site.ID) {
			visible = append(visible, site)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

type createSiteRequest struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Timezone string `json:"timezone"`
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
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
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityTenantManage, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	if s.tenants == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Tenant repository unavailable.")
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

	var input createSiteRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body is invalid.")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body must contain one JSON object.")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Address = strings.TrimSpace(input.Address)
	input.Timezone = strings.TrimSpace(input.Timezone)
	if len(input.Name) == 0 || len(input.Name) > 120 || len(input.Address) > 500 || len(input.Timezone) == 0 || len(input.Timezone) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Site fields are invalid.")
		return
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Timezone must be an IANA timezone.")
		return
	}

	result, err := s.tenants.CreateSite(r.Context(), tenant.CreateSiteCommand{
		TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Name: input.Name, Address: input.Address, Timezone: input.Timezone,
	})
	switch {
	case errors.Is(err, tenant.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Idempotency-Key was already used for a different request.")
		return
	case errors.Is(err, tenant.ErrSiteConflict):
		writeError(w, http.StatusConflict, "RESOURCE_CONFLICT", "A site with this name already exists.")
		return
	case errors.Is(err, tenant.ErrTenantNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Tenant repository unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": result.Site})
}

func correlationID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return uuid.NewString(), nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 4 {
		return "", errors.New("correlation identifier must be UUIDv4")
	}
	return parsed.String(), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "status": status,
	}})
}
