package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

const adminWorkspaceID = "99999999-9999-4999-8999-999999999999"

type fakeAdmin struct {
	leakyRepository
	updates []map[string]any
	err     error
}

func (f *fakeAdmin) ListWorkspaces(context.Context, string) ([]tenant.AdminWorkspace, error) {
	if f.err != nil {
		return nil, f.err
	}
	minutes := 10
	return []tenant.AdminWorkspace{{TenantID: adminWorkspaceID, Name: "Client A", Plan: "trial", LiveViewMinutes: &minutes}}, nil
}

func (f *fakeAdmin) UpdateWorkspace(_ context.Context, _, _, _ string, changes map[string]any) error {
	if f.err != nil {
		return f.err
	}
	f.updates = append(f.updates, changes)
	return nil
}

func platformOperator() identity.Principal {
	principal := viewer()
	principal.UserID = "88888888-8888-4888-8888-888888888888"
	principal.Operator, principal.MFALevel = true, "aal2"
	return principal
}

func adminRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAdminScreenIsForOperatorsSignedInWithTheirAuthenticator(t *testing.T) {
	admin := &fakeAdmin{}
	for _, test := range []struct {
		name      string
		principal identity.Principal
		status    int
	}{
		{name: "operator with aal2", principal: platformOperator(), status: http.StatusOK},
		{name: "not an operator", principal: viewer(), status: http.StatusForbidden},
		{name: "operator without authenticator", principal: func() identity.Principal { p := platformOperator(); p.MFALevel = "aal1"; return p }(), status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := adminRequest(New(fakeVerifier{principal: test.principal}, admin), http.MethodGet, "/v1/admin/workspaces", "")
			if response.Code != test.status {
				t.Fatalf("expected %d, got %d %s", test.status, response.Code, response.Body.String())
			}
		})
	}
	response := adminRequest(New(fakeVerifier{principal: platformOperator()}, admin), http.MethodGet, "/v1/admin/workspaces", "")
	if !strings.Contains(response.Body.String(), `"live_view_minutes":10`) {
		t.Fatalf("list body: %s", response.Body.String())
	}
	if response := adminRequest(New(fakeVerifier{principal: platformOperator()}, leakyRepository{}), http.MethodGet, "/v1/admin/workspaces", ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("without an admin repository: %d", response.Code)
	}
}

func TestAdminUpdateValidatesChanges(t *testing.T) {
	admin := &fakeAdmin{}
	handler := New(fakeVerifier{principal: platformOperator()}, admin)
	path := "/v1/admin/workspaces/" + adminWorkspaceID
	response := adminRequest(handler, http.MethodPatch, path, `{"plan":"active","trial_ends_at":null,"camera_limit":null,"live_view_minutes":300}`)
	if response.Code != http.StatusNoContent || len(admin.updates) != 1 {
		t.Fatalf("valid change: %d %s", response.Code, response.Body.String())
	}
	got := admin.updates[0]
	if got["plan"] != "active" || got["trial_ends_at"] != nil || got["camera_limit"] != nil || got["live_view_minutes"] != 300 {
		t.Fatalf("normalized changes: %#v", got)
	}
	response = adminRequest(handler, http.MethodPatch, path, `{"trial_ends_at":"2026-12-05T10:00:00-05:00"}`)
	if response.Code != http.StatusNoContent || admin.updates[1]["trial_ends_at"] != "2026-12-05T15:00:00Z" {
		t.Fatalf("trial end in UTC: %d %#v", response.Code, admin.updates)
	}
	for body, why := range map[string]string{
		`{}`:                                    "nothing to change",
		`{"plan":"gold"}`:                       "unknown plan",
		`{"tier":"enterprise"}`:                 "unknown setting",
		`{"live_view_minutes":-1}`:              "negative minutes",
		`{"camera_limit":2.5}`:                  "fractional limit",
		`{"trial_ends_at":"soon"}`:              "bad date",
		`{"plan":"trial","trial_ends_at":null}`: "trial without an end",
		`{"site_limit":"3"}`:                    "limit as text",
		`{"live_view_minutes":1000001}`:         "too many minutes",
	} {
		if response := adminRequest(handler, http.MethodPatch, path, body); response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s must be refused: %d", why, response.Code)
		}
	}
	if response := adminRequest(handler, http.MethodPatch, "/v1/admin/workspaces/not-a-uuid", `{"plan":"active"}`); response.Code != http.StatusNotFound {
		t.Fatalf("bad workspace id: %d", response.Code)
	}
	admin.err = tenant.ErrNotOperator
	if response := adminRequest(handler, http.MethodPatch, path, `{"plan":"active"}`); response.Code != http.StatusForbidden {
		t.Fatalf("database refusal: %d", response.Code)
	}
	admin.err = tenant.ErrWorkspaceNotFound
	if response := adminRequest(handler, http.MethodPatch, path, `{"plan":"active"}`); response.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: %d", response.Code)
	}
}
