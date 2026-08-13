package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

type fakeVerifier struct {
	principal identity.Principal
	err       error
}

func (v fakeVerifier) Verify(_ context.Context, token string) (identity.Principal, error) {
	if token != "valid" || v.err != nil {
		return identity.Principal{}, errors.New("invalid token")
	}
	return v.principal, nil
}

type leakyRepository struct{}

func (leakyRepository) ListSites(_ context.Context, _ string) ([]tenant.Site, error) {
	return []tenant.Site{
		{ID: "site-a", TenantID: "tenant-a", Name: "Visible"},
		{ID: "site-b", TenantID: "tenant-a", Name: "Outside site scope"},
		{ID: "site-x", TenantID: "tenant-b", Name: "Cross tenant"},
	}, nil
}

func (leakyRepository) CreateSite(_ context.Context, command tenant.CreateSiteCommand) (tenant.CreateSiteResult, error) {
	return tenant.CreateSiteResult{Site: tenant.Site{ID: "site-created", TenantID: command.TenantID, Name: command.Name, Timezone: command.Timezone, Status: "provisioning"}}, nil
}

func viewer() identity.Principal {
	return identity.Principal{
		UserID: "user-1", Email: "viewer@example.test", TenantID: "tenant-a",
		SiteIDs: []string{"site-a"}, Roles: []identity.Role{identity.RoleViewer},
		Scopes: []string{"auth:read", "sites:read"}, MFALevel: "password",
	}
}

func request(handler http.Handler, path, token, tenantID string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}
	handler.ServeHTTP(recorder, req)
	return recorder
}

func mutationRequest(handler http.Handler, body, token, tenantID, idempotencyKey string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/sites", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}
	if idempotencyKey != "" {
		req.Header.Set(idempotencyHeader, idempotencyKey)
	}
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestHealthDoesNotRequireAuthentication(t *testing.T) {
	response := request(New(nil, nil), "/healthz", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}

func TestMeRequiresVerifiedBearerAndMatchingTenant(t *testing.T) {
	handler := New(fakeVerifier{principal: viewer()}, leakyRepository{})

	if response := request(handler, "/v1/auth/me", "", "tenant-a"); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer: expected 401, got %d", response.Code)
	}
	if response := request(handler, "/v1/auth/me", "invalid", "tenant-a"); response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer: expected 401, got %d", response.Code)
	}
	if response := request(handler, "/v1/auth/me", "valid", ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing tenant: expected 422, got %d", response.Code)
	}
	if response := request(handler, "/v1/auth/me", "valid", "tenant-b"); response.Code != http.StatusNotFound {
		t.Fatalf("cross tenant: expected 404, got %d", response.Code)
	}

	response := request(handler, "/v1/auth/me", "valid", "tenant-a")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "viewer@example.test") {
		t.Fatalf("valid request failed: %d %s", response.Code, response.Body.String())
	}
}

func TestListSitesDefendsAgainstLeakyRepository(t *testing.T) {
	handler := New(fakeVerifier{principal: viewer()}, leakyRepository{})
	response := request(handler, "/v1/sites", "valid", "tenant-a")
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "site-a") || strings.Contains(body, "site-b") || strings.Contains(body, "site-x") {
		t.Fatalf("site isolation failed: %s", body)
	}
}

func TestListSitesRequiresExplicitScope(t *testing.T) {
	principal := viewer()
	principal.Scopes = []string{"auth:read"}
	handler := New(fakeVerifier{principal: principal}, leakyRepository{})
	response := request(handler, "/v1/sites", "valid", "tenant-a")
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestListSitesFailsClosedWithoutRepository(t *testing.T) {
	handler := New(fakeVerifier{principal: viewer()}, nil)
	response := request(handler, "/v1/sites", "valid", "tenant-a")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreateSiteRequiresTenantManagerAndIdempotency(t *testing.T) {
	if response := mutationRequest(New(fakeVerifier{principal: viewer()}, leakyRepository{}), `{"name":"Pilot","timezone":"Asia/Kolkata"}`, "valid", "tenant-a", "site-create-1"); response.Code != http.StatusForbidden {
		t.Fatalf("viewer create: expected 403, got %d: %s", response.Code, response.Body.String())
	}

	principal := viewer()
	principal.TenantID = "11111111-1111-4111-8111-111111111111"
	principal.Roles = []identity.Role{identity.RoleSuperAdmin}
	principal.Scopes = []string{"tenant:manage"}
	principal.MFALevel = "mfa"
	handler := New(fakeVerifier{principal: principal}, tenant.NewMemoryRepository(nil))
	if response := mutationRequest(handler, `{"name":"Pilot","timezone":"Asia/Kolkata"}`, "valid", principal.TenantID, ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing idempotency key: expected 422, got %d", response.Code)
	}
	response := mutationRequest(handler, `{"name":"Pilot","timezone":"Asia/Kolkata"}`, "valid", principal.TenantID, "site-create-1")
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"status":"provisioning"`) {
		t.Fatalf("create failed: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get(correlationHeader) == "" {
		t.Fatal("expected generated correlation identifier")
	}
	replay := mutationRequest(handler, `{"name":"Pilot","timezone":"Asia/Kolkata"}`, "valid", principal.TenantID, "site-create-1")
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" || replay.Body.String() != response.Body.String() {
		t.Fatalf("idempotent replay failed: %d %s", replay.Code, replay.Body.String())
	}
	conflict := mutationRequest(handler, `{"name":"Different","timezone":"Asia/Kolkata"}`, "valid", principal.TenantID, "site-create-1")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_REPLAY") {
		t.Fatalf("different replay: expected 409, got %d %s", conflict.Code, conflict.Body.String())
	}
}

func TestCreateSiteRejectsUnknownFieldsAndInvalidCorrelationID(t *testing.T) {
	principal := viewer()
	principal.TenantID = "11111111-1111-4111-8111-111111111111"
	principal.Roles = []identity.Role{identity.RoleSuperAdmin}
	principal.Scopes = []string{"tenant:manage"}
	principal.MFALevel = "mfa"
	handler := New(fakeVerifier{principal: principal}, tenant.NewMemoryRepository(nil))

	response := mutationRequest(handler, `{"name":"Pilot","timezone":"Asia/Kolkata","unexpected":true}`, "valid", principal.TenantID, "site-create-2")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field: expected 422, got %d", response.Code)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/sites", strings.NewReader(`{"name":"Pilot","timezone":"Asia/Kolkata"}`))
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set(tenantHeader, principal.TenantID)
	req.Header.Set(idempotencyHeader, "site-create-3")
	req.Header.Set(correlationHeader, "not-a-uuid")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid correlation id: expected 422, got %d", recorder.Code)
	}
}
