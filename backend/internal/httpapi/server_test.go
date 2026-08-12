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
