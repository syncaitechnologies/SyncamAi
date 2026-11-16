package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

// Pairing: an administrator gets a short code for a site; the person
// installing the box types it once and the box receives its own site key.
func (s *Server) registerPairingRoutes() {
	s.mux.Handle("POST /v1/site-keys/pairing-codes", s.authenticate(http.HandlerFunc(s.createPairingCode)))
	s.mux.HandleFunc("POST /v1/site-ingest/pair", s.redeemPairingCode)
}

type pairingCodeRequest struct {
	SiteID string `json:"site_id"`
}

func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	if s.siteIngest.Pairing == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site box pairing is unavailable.")
		return
	}
	var input pairingCodeRequest
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
	code, expiresAt, err := s.siteIngest.Pairing.CreatePairingCode(r.Context(), tenantID, siteID, principal.UserID)
	if errors.Is(err, siteingest.ErrSiteNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site box pairing is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"code": siteingest.FormatPairingCode(code), "site_id": siteID, "expires_at": expiresAt,
	}})
}

type redeemPairingRequest struct {
	Code      string `json:"code"`
	PublicKey string `json:"public_key"`
	Label     string `json:"label"`
}

func (s *Server) redeemPairingCode(w http.ResponseWriter, r *http.Request) {
	if s.siteIngest.Pairing == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site box pairing is unavailable.")
		return
	}
	if !s.pairingLimiter.allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many pairing attempts. Try again in a few minutes.")
		return
	}
	var input redeemPairingRequest
	if !decodeBodyLimit(w, r, &input, 8<<10) {
		return
	}
	if len(input.Label) > 120 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Label is too long.")
		return
	}
	rawKey, scope, err := s.siteIngest.Pairing.RedeemPairingCode(r.Context(), input.Code, input.PublicKey, input.Label)
	switch {
	case errors.Is(err, siteingest.ErrPublicKeyInvalid):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "public_key must be a PEM P-256 public key.")
		return
	case errors.Is(err, siteingest.ErrPairingInvalid):
		writeError(w, http.StatusNotFound, "PAIRING_INVALID", "This pairing code is wrong, already used or expired. Create a new one in the dashboard.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Site box pairing is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"site_key": rawKey, "site_id": scope.SiteID, "key_id": scope.KeyID,
		"notice": "Keep this key on the site box only; it is not shown again.",
	}})
}

// clientIP is the address of the caller as seen by the platform's proxy:
// the last X-Forwarded-For hop (added by Render), else the socket address.
func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// attemptLimiter allows perKey attempts per window for each caller and total
// attempts per window overall, so codes cannot be guessed at scale.
type attemptLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	perKey   int
	total    int
	attempts map[string][]time.Time
	all      []time.Time
}

func newAttemptLimiter(window time.Duration, perKey, total int) *attemptLimiter {
	return &attemptLimiter{window: window, perKey: perKey, total: total, attempts: map[string][]time.Time{}}
}

func (l *attemptLimiter) allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	recent := func(times []time.Time) []time.Time {
		kept := times[:0]
		for _, at := range times {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		return kept
	}
	l.all = recent(l.all)
	mine := recent(l.attempts[key])
	if len(l.attempts) > 10000 {
		// Drop idle callers so the map cannot grow without bound.
		for other, times := range l.attempts {
			if len(recent(times)) == 0 {
				delete(l.attempts, other)
			}
		}
	}
	if len(mine) >= l.perKey || len(l.all) >= l.total {
		l.attempts[key] = mine
		return false
	}
	l.attempts[key] = append(mine, now)
	l.all = append(l.all, now)
	return true
}
