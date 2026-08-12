// Package httpapi exposes the initial control-plane REST boundary.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

const tenantHeader = "X-SentinelVision-Tenant-ID"

type principalContextKey struct{}

// Server owns the first tenant-safe control-plane routes.
type Server struct {
	verifier identity.Verifier
	tenants  tenant.Repository
	mux      *http.ServeMux
}

// New builds a fail-closed HTTP handler around explicit dependencies.
func New(verifier identity.Verifier, tenants tenant.Repository) http.Handler {
	server := &Server{verifier: verifier, tenants: tenants, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.Handle("GET /v1/auth/me", server.authenticate(http.HandlerFunc(server.me)))
	server.mux.Handle("GET /v1/sites", server.authenticate(http.HandlerFunc(server.listSites)))
	return server.mux
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
