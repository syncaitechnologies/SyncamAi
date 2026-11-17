package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

// registrationVerifier answers both normal and registration verification.
type registrationVerifier struct {
	principal identity.Principal
	user      identity.Identity
}

func (v registrationVerifier) Verify(_ context.Context, token string) (identity.Principal, error) {
	if token != "member" {
		return identity.Principal{}, errors.New("no workspace claims")
	}
	return v.principal, nil
}

func (v registrationVerifier) VerifyIdentity(_ context.Context, token string) (identity.Identity, error) {
	if token != "member" && token != "new" {
		return identity.Identity{}, errors.New("invalid token")
	}
	return v.user, nil
}

func registrationRequest(handler http.Handler, body, token, idempotencyKey, ip string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set(idempotencyHeader, idempotencyKey)
	}
	req.RemoteAddr = ip + ":5000"
	handler.ServeHTTP(recorder, req)
	return recorder
}

const newUserID = "77777777-7777-4777-8777-777777777777"
const workspaceBody = `{"company":"Acme Security","site_name":"Head office","timezone":"America/Toronto"}`

func TestRegisterWorkspaceCreatesOneWorkspacePerUser(t *testing.T) {
	repository := tenant.NewMemoryRepository(nil)
	handler := New(registrationVerifier{user: identity.Identity{UserID: newUserID, EmailVerified: true}}, repository)

	first := registrationRequest(handler, workspaceBody, "new", "key-1", "203.0.113.1")
	if first.Code != http.StatusCreated {
		t.Fatalf("first registration: %d %s", first.Code, first.Body.String())
	}
	var created struct {
		Data tenant.RegisterWorkspaceResult `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil || !created.Data.Created || created.Data.TenantID == "" || created.Data.SiteID == "" {
		t.Fatalf("first body: %s %v", first.Body.String(), err)
	}
	again := registrationRequest(handler, `{"company":"Other","site_name":"Other","timezone":"UTC"}`, "new", "key-2", "203.0.113.1")
	if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), created.Data.TenantID) || !strings.Contains(again.Body.String(), `"created":false`) {
		t.Fatalf("repeat registration: %d %s", again.Code, again.Body.String())
	}
}

func TestRegisterWorkspaceRejectsBadRequests(t *testing.T) {
	verified := registrationVerifier{user: identity.Identity{UserID: newUserID, EmailVerified: true}}
	for _, test := range []struct {
		name     string
		verifier identity.Verifier
		body     string
		token    string
		key      string
		status   int
		code     string
	}{
		{name: "no token", verifier: verified, body: workspaceBody, key: "k", status: http.StatusUnauthorized, code: "AUTH_REQUIRED"},
		{name: "bad token", verifier: verified, body: workspaceBody, token: "forged", key: "k", status: http.StatusUnauthorized, code: "AUTH_REQUIRED"},
		{name: "unverified email", verifier: registrationVerifier{user: identity.Identity{UserID: newUserID}}, body: workspaceBody, token: "new", key: "k", status: http.StatusForbidden, code: "EMAIL_NOT_VERIFIED"},
		{name: "no idempotency key", verifier: verified, body: workspaceBody, token: "new", status: http.StatusUnprocessableEntity, code: "VALIDATION_FAILED"},
		{name: "short company", verifier: verified, body: `{"company":"A","site_name":"Head office","timezone":"UTC"}`, token: "new", key: "k", status: http.StatusUnprocessableEntity, code: "VALIDATION_FAILED"},
		{name: "bad timezone", verifier: verified, body: `{"company":"Acme","site_name":"Head office","timezone":"Mars/Base"}`, token: "new", key: "k", status: http.StatusUnprocessableEntity, code: "VALIDATION_FAILED"},
		{name: "unknown field", verifier: verified, body: `{"company":"Acme","site_name":"Head office","timezone":"UTC","plan":"active"}`, token: "new", key: "k", status: http.StatusUnprocessableEntity, code: "VALIDATION_FAILED"},
		{name: "verifier without registration", verifier: fakeVerifier{principal: viewer()}, body: workspaceBody, token: "valid", key: "k", status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := New(test.verifier, tenant.NewMemoryRepository(nil))
			response := registrationRequest(handler, test.body, test.token, test.key, "203.0.113.2")
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("expected %d %s, got %d %s", test.status, test.code, response.Code, response.Body.String())
			}
		})
	}
}

func TestRegisterWorkspaceIsRateLimitedPerCaller(t *testing.T) {
	handler := New(registrationVerifier{user: identity.Identity{UserID: newUserID, EmailVerified: true}}, tenant.NewMemoryRepository(nil))
	for attempt := 0; attempt < 5; attempt++ {
		if response := registrationRequest(handler, workspaceBody, "new", "k", "198.51.100.7"); response.Code >= 400 {
			t.Fatalf("attempt %d: %d %s", attempt, response.Code, response.Body.String())
		}
	}
	if response := registrationRequest(handler, workspaceBody, "new", "k", "198.51.100.7"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt: %d", response.Code)
	}
	if response := registrationRequest(handler, workspaceBody, "new", "k", "198.51.100.8"); response.Code >= 400 {
		t.Fatalf("another caller is not limited: %d", response.Code)
	}
}

func TestEndedTrialKeepsOnlyWhoAmI(t *testing.T) {
	ended := time.Now().Add(-time.Hour)
	principal := viewer()
	principal.Plan, principal.TrialEndsAt = identity.PlanTrial, &ended
	handler := New(fakeVerifier{principal: principal}, leakyRepository{})
	if response := request(handler, "/v1/sites", "valid", "tenant-a"); response.Code != http.StatusPaymentRequired || !strings.Contains(response.Body.String(), "TRIAL_ENDED") {
		t.Fatalf("sites after trial: %d %s", response.Code, response.Body.String())
	}
	if response := request(handler, "/v1/auth/me", "valid", "tenant-a"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"plan":"trial"`) {
		t.Fatalf("me after trial: %d %s", response.Code, response.Body.String())
	}
	future := time.Now().Add(time.Hour)
	principal.TrialEndsAt = &future
	handler = New(fakeVerifier{principal: principal}, leakyRepository{})
	if response := request(handler, "/v1/sites", "valid", "tenant-a"); response.Code != http.StatusOK {
		t.Fatalf("sites during trial: %d %s", response.Code, response.Body.String())
	}
}

func TestSiteCreationOverThePlanLimitIsRefused(t *testing.T) {
	principal := viewer()
	principal.Roles, principal.Scopes, principal.MFALevel = []identity.Role{identity.RoleSuperAdmin}, []string{"tenant:manage"}, "aal2"
	repository := tenant.NewMemoryRepository([]tenant.Site{{ID: "site-a", TenantID: "tenant-a", Name: "First", Timezone: "UTC", Status: "active"}})
	repository.SiteLimit = 1
	handler := New(fakeVerifier{principal: principal}, repository)
	response := mutationRequest(handler, `{"name":"Second","timezone":"UTC"}`, "valid", "tenant-a", "second")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "PLAN_LIMIT") {
		t.Fatalf("second site: %d %s", response.Code, response.Body.String())
	}
}
