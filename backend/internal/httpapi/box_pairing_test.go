package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

func pairingAdmin() identity.Principal {
	principal := siteAdmin()
	principal.Scopes = append(principal.Scopes, "config:write")
	return principal
}

func newPairingFixture(t *testing.T, principal identity.Principal) (*ingestFixture, *siteingest.MemoryKeyStore) {
	t.Helper()
	keys := siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	options := SiteIngestOptions{Keys: keys, Pairing: keys}
	handler := NewWithSiteIngest(fakeVerifier{principal: principal}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, nil, nil, nil, nil, options)
	return &ingestFixture{handler: handler, keys: keys}, keys
}

func boxKeyPEM(t *testing.T) string {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&private.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func issuePairingCode(t *testing.T, fixture *ingestFixture, siteID string) (int, string) {
	t.Helper()
	response := fixture.do(http.MethodPost, "/v1/site-keys/pairing-codes", "valid", []byte(`{"site_id":"`+siteID+`"}`), "application/json", map[string]string{tenantHeader: ingestTenant})
	var body struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return response.Code, body.Data.Code
}

func redeem(fixture *ingestFixture, code, publicKey string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(map[string]string{"code": code, "public_key": publicKey, "label": "Front office PC"})
	return fixture.do(http.MethodPost, "/v1/site-ingest/pair", "", payload, "application/json", nil)
}

func TestPairingCodeGivesTheBoxAWorkingSiteKeyOnce(t *testing.T) {
	fixture, keys := newPairingFixture(t, pairingAdmin())
	status, code := issuePairingCode(t, fixture, ingestSite)
	if status != http.StatusCreated || len(code) != 9 || code[4] != '-' {
		t.Fatalf("issue: %d %q", status, code)
	}
	publicKey := boxKeyPEM(t)
	response := redeem(fixture, strings.ToLower(code), publicKey)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("redeem: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			SiteKey string `json:"site_key"`
			SiteID  string `json:"site_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if body.Data.SiteID != ingestSite {
		t.Fatalf("site: %+v", body.Data)
	}
	if scope, err := keys.Resolve(context.Background(), body.Data.SiteKey); err != nil || scope.SiteID != ingestSite {
		t.Fatalf("the returned key must authenticate the box for its site: %v", err)
	}
	listed, _ := keys.List(context.Background(), ingestTenant, ingestSite)
	if len(listed) != 1 || listed[0].BoxPublicKey != strings.TrimSpace(publicKey) {
		t.Fatalf("box public key not kept: %+v", listed)
	}
	again := redeem(fixture, code, "")
	if again.Code != http.StatusNotFound || !strings.Contains(again.Body.String(), "PAIRING_INVALID") {
		t.Fatalf("a code works once: %d %s", again.Code, again.Body.String())
	}
}

func TestPairingRejectsBadInputAndExpiredCodes(t *testing.T) {
	fixture, keys := newPairingFixture(t, pairingAdmin())
	_, code := issuePairingCode(t, fixture, ingestSite)
	if response := redeem(fixture, code, "not a key"); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad public key: %d", response.Code)
	}
	keys.ExpirePairingCode(code)
	if response := redeem(fixture, code, ""); response.Code != http.StatusNotFound {
		t.Fatalf("expired code: %d", response.Code)
	}
	long := strings.Repeat("x", 121)
	payload, _ := json.Marshal(map[string]string{"code": "AAAA-BBBB", "label": long})
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/pair", "", payload, "application/json", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("label too long: %d", response.Code)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/pair", "", []byte(`{"unknown":1}`), "application/json", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown fields: %d", response.Code)
	}
}

func TestOnlyConfigWritersForTheirSiteCanIssuePairingCodes(t *testing.T) {
	reader := siteAdmin()
	reader.Scopes = []string{"config:read", "alerts:read"}
	fixture, _ := newPairingFixture(t, reader)
	if status, _ := issuePairingCode(t, fixture, ingestSite); status != http.StatusForbidden {
		t.Fatalf("without config:write: %d", status)
	}
	fixture, _ = newPairingFixture(t, pairingAdmin())
	if status, _ := issuePairingCode(t, fixture, ingestOtherSite); status != http.StatusNotFound {
		t.Fatalf("another site: %d", status)
	}
	if status, _ := issuePairingCode(t, fixture, "not-a-uuid"); status != http.StatusUnprocessableEntity {
		t.Fatalf("bad site id: %d", status)
	}
}

func TestPairingIsRateLimitedPerCaller(t *testing.T) {
	fixture, _ := newPairingFixture(t, pairingAdmin())
	for attempt := 0; attempt < 10; attempt++ {
		if response := redeem(fixture, "WRNG-CODE", ""); response.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: %d", attempt, response.Code)
		}
	}
	limited := redeem(fixture, "WRNG-CODE", "")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("the 11th guess must be limited: %d", limited.Code)
	}
}

func TestAttemptLimiterWindowAndGlobalCap(t *testing.T) {
	limiter := newAttemptLimiter(time.Minute, 2, 3)
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	if !limiter.allow("a", start) || !limiter.allow("a", start) || limiter.allow("a", start) {
		t.Fatal("two per caller per window")
	}
	if !limiter.allow("b", start) || limiter.allow("c", start) {
		t.Fatal("three overall per window")
	}
	if !limiter.allow("a", start.Add(61*time.Second)) {
		t.Fatal("the window slides")
	}
	if (*attemptLimiter)(nil).allow("x", start) != true {
		t.Fatal("no limiter allows everything")
	}
}

func TestClientIPUsesTheProxyHop(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.RemoteAddr = "10.0.0.5:4444"
	if clientIP(request) != "10.0.0.5" {
		t.Fatalf("socket address: %s", clientIP(request))
	}
	request.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if clientIP(request) != "203.0.113.9" {
		t.Fatalf("the proxy-added last hop is used, not a client-supplied first value: %s", clientIP(request))
	}
}

func TestPairingUnavailableWithoutStore(t *testing.T) {
	handler := NewWithSiteIngest(fakeVerifier{principal: pairingAdmin()}, ingestSites{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SiteIngestOptions{Keys: siteingest.NewMemoryKeyStore()})
	fixture := &ingestFixture{handler: handler}
	if response := redeem(fixture, "AAAA-BBBB", ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("redeem without store: %d", response.Code)
	}
	if status, _ := issuePairingCode(t, fixture, ingestSite); status != http.StatusServiceUnavailable {
		t.Fatalf("issue without store: %d", status)
	}
}
