package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

// SiteIngestOptions wires on-premises site boxes, alert snapshots and
// Telegram linking. Any nil dependency makes its routes fail closed with 503.
type SiteIngestOptions struct {
	Keys              siteingest.KeyStore
	Evidence          siteingest.EvidenceStore
	TelegramTargets   siteingest.TargetStore
	AlertEvidence     alerting.EvidenceReader
	TelegramBotHandle string
}

type siteScopeContextKey struct{}

// siteKeyScopes are the only capabilities a site box ever receives.
var siteKeyScopes = []string{string(authz.CapabilityEventsWrite), string(authz.CapabilityConfigRead)}

func (s *Server) registerSiteIngestRoutes() {
	s.mux.Handle("GET /v1/site-ingest/config", s.authenticateSite(http.HandlerFunc(s.siteConfiguration)))
	s.mux.Handle("POST /v1/site-ingest/events", s.authenticateSite(http.HandlerFunc(s.ingestEvent)))
	s.mux.Handle("POST /v1/site-ingest/evidence", s.authenticateSite(http.HandlerFunc(s.uploadSiteEvidence)))
	s.mux.Handle("POST /v1/site-ingest/heartbeat", s.authenticateSite(http.HandlerFunc(s.recordSiteHeartbeat)))
	s.mux.Handle("PUT /v1/site-ingest/cameras/{id}/reference", s.authenticateSite(http.HandlerFunc(s.uploadCameraReference)))

	s.mux.Handle("GET /v1/alerts/{id}/snapshot", s.authenticate(http.HandlerFunc(s.alertSnapshot)))
	s.mux.Handle("GET /v1/cameras/{id}/reference", s.authenticate(http.HandlerFunc(s.cameraReference)))
	s.mux.Handle("GET /v1/site-keys", s.authenticate(http.HandlerFunc(s.listSiteKeys)))
	s.mux.Handle("POST /v1/site-keys", s.authenticate(http.HandlerFunc(s.createSiteKey)))
	s.mux.Handle("POST /v1/site-keys/{id}/revoke", s.authenticate(http.HandlerFunc(s.revokeSiteKey)))
	s.mux.Handle("GET /v1/notifications/telegram", s.authenticate(http.HandlerFunc(s.listTelegramTargets)))
	s.mux.Handle("POST /v1/notifications/telegram/link-codes", s.authenticate(http.HandlerFunc(s.createTelegramLinkCode)))
	s.mux.Handle("DELETE /v1/notifications/telegram/{id}", s.authenticate(http.HandlerFunc(s.deleteTelegramTarget)))
}

// authenticateSite verifies a site key and runs the handler as a principal
// limited to that key's single site, with only event-write and config-read.
func (s *Server) authenticateSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.siteIngest.Keys == nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
			return
		}
		authorization := strings.Fields(r.Header.Get("Authorization"))
		if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
			return
		}
		scope, err := s.siteIngest.Keys.Resolve(r.Context(), authorization[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
			return
		}
		principal := identity.Principal{
			UserID: "site-key:" + scope.KeyID, TenantID: scope.TenantID, SiteIDs: []string{scope.SiteID},
			Roles: []identity.Role{identity.RoleSiteAdmin}, Scopes: siteKeyScopes,
			DataClasses: []string{"metadata"}, MFALevel: "site_key",
		}
		// The tenant comes only from the verified key, never from the client.
		r.Header.Set(tenantHeader, scope.TenantID)
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		ctx = context.WithValue(ctx, siteScopeContextKey{}, scope)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func siteScopeFromContext(ctx context.Context) (siteingest.Scope, bool) {
	scope, ok := ctx.Value(siteScopeContextKey{}).(siteingest.Scope)
	return scope, ok
}

type siteCamera struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) siteConfiguration(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.zones == nil || s.cameras == nil || s.tenants == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration unavailable.")
		return
	}
	site, err := s.findSite(r.Context(), scope.TenantID, scope.SiteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration unavailable.")
		return
	}
	cameras, err := s.cameras.List(r.Context(), scope.TenantID, scope.SiteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration unavailable.")
		return
	}
	listedZones, err := s.zones.List(r.Context(), scope.TenantID, scope.SiteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Configuration unavailable.")
		return
	}
	activeCameras := make([]siteCamera, 0, len(cameras))
	for _, camera := range cameras {
		if camera.SiteID == scope.SiteID && camera.LifecycleStatus != "retired" {
			activeCameras = append(activeCameras, siteCamera{ID: camera.ID, Name: camera.Name})
		}
	}
	visibleZones := make([]zones.Zone, 0, len(listedZones))
	for _, zone := range listedZones {
		if zone.SiteID == scope.SiteID {
			visibleZones = append(visibleZones, zone)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"tenant_id": scope.TenantID, "site_id": scope.SiteID,
		"site":    map[string]string{"name": site.Name, "timezone": site.Timezone},
		"cameras": activeCameras, "zones": visibleZones,
		"server_time": time.Now().UTC(),
	}})
}

func (s *Server) findSite(ctx context.Context, tenantID, siteID string) (tenant.Site, error) {
	sites, err := s.tenants.ListSites(ctx, tenantID)
	if err != nil {
		return tenant.Site{}, err
	}
	for _, site := range sites {
		if site.ID == siteID {
			return site, nil
		}
	}
	return tenant.Site{}, siteingest.ErrSiteNotFound
}

func readJPEG(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "image/jpeg") {
		writeError(w, http.StatusUnsupportedMediaType, "VALIDATION_FAILED", "Snapshot must be image/jpeg.")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, siteingest.MaxSnapshotBytes))
	if err != nil || !siteingest.IsJPEG(data) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Snapshot must be a JPEG of at most 512 KB.")
		return nil, false
	}
	return data, true
}

func (s *Server) uploadSiteEvidence(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.siteIngest.Evidence == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	data, ok := readJPEG(w, r)
	if !ok {
		return
	}
	ref, err := s.siteIngest.Evidence.Put(r.Context(), scope.TenantID, scope.SiteID, data)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"ref": ref}})
}

func (s *Server) uploadCameraReference(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.siteIngest.Evidence == nil || s.cameras == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	cameraID := r.PathValue("id")
	if _, err := uuid.Parse(cameraID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	camera, err := s.cameras.Get(r.Context(), scope.TenantID, cameraID)
	if err != nil || camera.SiteID != scope.SiteID {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	data, ok := readJPEG(w, r)
	if !ok {
		return
	}
	if err := s.siteIngest.Evidence.PutReference(r.Context(), scope.TenantID, scope.SiteID, cameraID, data); err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type siteHeartbeatRequest struct {
	Cameras map[string]siteingest.CameraStatus `json:"cameras"`
}

func (s *Server) recordSiteHeartbeat(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	var input siteHeartbeatRequest
	if !decodeBodyLimit(w, r, &input, 64<<10) {
		return
	}
	if len(input.Cameras) > 256 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Too many cameras in heartbeat.")
		return
	}
	for cameraID, status := range input.Cameras {
		if _, err := uuid.Parse(cameraID); err != nil || len(status.Error) > 256 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Camera status is invalid.")
			return
		}
	}
	if input.Cameras == nil {
		input.Cameras = map[string]siteingest.CameraStatus{}
	}
	if err := s.siteIngest.Keys.RecordHeartbeat(r.Context(), scope, input.Cameras); err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Heartbeat unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"server_time": time.Now().UTC()}})
}

// userTenant resolves the authenticated user's tenant and checks a capability.
func (s *Server) userTenant(w http.ResponseWriter, r *http.Request, capability authz.Capability) (identity.Principal, string, bool) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return identity.Principal{}, "", false
	}
	tenantID, err := requestTenant(r, principal)
	if errors.Is(err, authz.ErrDenied) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identity.Principal{}, "", false
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Tenant header is required.")
		return identity.Principal{}, "", false
	}
	if err := authz.Authorize(principal, authz.Request{Capability: capability, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identity.Principal{}, "", false
	}
	return principal, tenantID, true
}

func writeJPEG(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) alertSnapshot(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityAlertsRead)
	if !ok {
		return
	}
	if s.alerts == nil || s.siteIngest.AlertEvidence == nil || s.siteIngest.Evidence == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	alert, err := s.alerts.Get(r.Context(), tenantID, r.PathValue("id"))
	if err != nil || !authz.CanAccessSite(principal, tenantID, alert.SiteID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	refs, err := s.siteIngest.AlertEvidence.EvidenceRefs(r.Context(), tenantID, alert.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref, siteingest.SnapshotRefPrefix) {
			data, err := s.siteIngest.Evidence.Get(r.Context(), tenantID, ref)
			if errors.Is(err, siteingest.ErrEvidenceNotFound) {
				break
			}
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
				return
			}
			writeJPEG(w, data)
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "No snapshot for this alert.")
}

func (s *Server) cameraReference(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	if s.cameras == nil || s.siteIngest.Evidence == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	camera, ok := s.authorizedCamera(w, r, principal, tenantID)
	if !ok {
		return
	}
	data, err := s.siteIngest.Evidence.GetReference(r.Context(), tenantID, camera.SiteID, camera.ID)
	if errors.Is(err, siteingest.ErrEvidenceNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "No reference image yet. Start the site box first.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Evidence storage unavailable.")
		return
	}
	writeJPEG(w, data)
}

// siteFromQuery validates an optional or required site_id query parameter.
func siteFromQuery(w http.ResponseWriter, r *http.Request, principal identity.Principal, tenantID string, required bool) (string, bool) {
	siteID := strings.TrimSpace(r.URL.Query().Get("site_id"))
	if siteID == "" && !required {
		return "", true
	}
	if _, err := uuid.Parse(siteID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "site_id must be a UUID.")
		return "", false
	}
	if !authz.CanAccessSite(principal, tenantID, siteID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return "", false
	}
	return siteID, true
}

func (s *Server) listSiteKeys(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	if s.siteIngest.Keys == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	siteID, ok := siteFromQuery(w, r, principal, tenantID, false)
	if !ok {
		return
	}
	keys, err := s.siteIngest.Keys.List(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	visible := make([]siteingest.SiteKey, 0, len(keys))
	for _, key := range keys {
		if authz.CanAccessSite(principal, tenantID, key.SiteID) {
			visible = append(visible, key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

type createSiteKeyRequest struct {
	SiteID string `json:"site_id"`
	Label  string `json:"label"`
}

func (s *Server) createSiteKey(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.siteIngest.Keys == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	var input createSiteKeyRequest
	if !decodeBody(w, r, &input) {
		return
	}
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" {
		input.Label = "Site box"
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.SiteID)); err != nil || len(input.Label) > 120 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Site key fields are invalid.")
		return
	}
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: input.SiteID}); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	rawKey, key, err := s.siteIngest.Keys.Create(r.Context(), tenantID, strings.TrimSpace(input.SiteID), principal.UserID, input.Label)
	if errors.Is(err, siteingest.ErrSiteNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"data": key, "secret": map[string]string{
		"site_key": rawKey, "notice": "Copy this key into the site box now. It is shown only once.",
	}})
}

func (s *Server) revokeSiteKey(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.siteIngest.Keys == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	keyID := r.PathValue("id")
	keys, err := s.siteIngest.Keys.List(r.Context(), tenantID, "")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
		return
	}
	for _, key := range keys {
		if key.ID != keyID {
			continue
		}
		if authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: key.SiteID}) != nil {
			break
		}
		if err := s.siteIngest.Keys.Revoke(r.Context(), tenantID, keyID); err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site ingest unavailable.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
}

func (s *Server) listTelegramTargets(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	if s.siteIngest.TelegramTargets == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts are not configured.")
		return
	}
	siteID, ok := siteFromQuery(w, r, principal, tenantID, false)
	if !ok {
		return
	}
	targets, err := s.siteIngest.TelegramTargets.Targets(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts unavailable.")
		return
	}
	visible := make([]siteingest.TelegramTarget, 0, len(targets))
	for _, target := range targets {
		if authz.CanAccessSite(principal, tenantID, target.SiteID) {
			visible = append(visible, target)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "bot": s.siteIngest.TelegramBotHandle}})
}

type telegramLinkRequest struct {
	SiteID string `json:"site_id"`
}

func (s *Server) createTelegramLinkCode(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.siteIngest.TelegramTargets == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts are not configured.")
		return
	}
	var input telegramLinkRequest
	if !decodeBody(w, r, &input) {
		return
	}
	siteID := strings.TrimSpace(input.SiteID)
	if _, err := uuid.Parse(siteID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "site_id must be a UUID.")
		return
	}
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: siteID}); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	code, expiresAt, err := s.siteIngest.TelegramTargets.CreateLinkCode(r.Context(), tenantID, siteID, principal.UserID)
	if errors.Is(err, siteingest.ErrSiteNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"code": code, "expires_at": expiresAt, "bot": s.siteIngest.TelegramBotHandle,
	}})
}

func (s *Server) deleteTelegramTarget(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.siteIngest.TelegramTargets == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts are not configured.")
		return
	}
	targetID := r.PathValue("id")
	targets, err := s.siteIngest.TelegramTargets.Targets(r.Context(), tenantID, "")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts unavailable.")
		return
	}
	for _, target := range targets {
		if target.ID != targetID {
			continue
		}
		if authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: target.SiteID}) != nil {
			break
		}
		if err := s.siteIngest.TelegramTargets.Delete(r.Context(), tenantID, targetID); err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Telegram alerts unavailable.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
}
