package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
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

type eventRepositoryFunc func(context.Context, eventing.IngestCommand) (eventing.IngestResult, error)

func (f eventRepositoryFunc) Ingest(ctx context.Context, command eventing.IngestCommand) (eventing.IngestResult, error) {
	return f(ctx, command)
}

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

func eventRequest(handler http.Handler, body, token, tenantID, requestID string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}
	if requestID != "" {
		req.Header.Set(eventRequestHeader, requestID)
	}
	handler.ServeHTTP(recorder, req)
	return recorder
}

const validEventBody = `{
  "event_id":"22222222-2222-4222-8222-222222222222",
  "tenant_id":"11111111-1111-4111-8111-111111111111",
  "dedupe_key":"camera-1:42",
  "occurred_at":"2026-08-13T01:00:00Z",
  "site_id":"33333333-3333-4333-8333-333333333333",
  "camera_id":"44444444-4444-4444-8444-444444444444",
  "zone_id":"55555555-5555-4555-8555-555555555555",
  "event_type":"intrusion",
  "model_version":"detector-1",
  "confidence":0.91,
  "evidence_refs":["evidence://clip-1"],
  "requires_human_review":true,
  "review_state":"pending"
}`

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

func TestListAlertsFiltersTenantAndSiteScope(t *testing.T) {
	principal := viewer()
	principal.Scopes = append(principal.Scopes, "alerts:read")
	repository := &alerting.MemoryRepository{Alerts: []alerting.Alert{
		{ID: "visible", TenantID: "tenant-a", SiteID: "site-a", Severity: "high", Status: "unacknowledged"},
		{ID: "other-site", TenantID: "tenant-a", SiteID: "site-b"},
		{ID: "other-tenant", TenantID: "tenant-b", SiteID: "site-a"},
	}}
	handler := NewWithAlerts(fakeVerifier{principal: principal}, leakyRepository{}, nil, repository)
	response := request(handler, "/v1/alerts", "valid", "tenant-a")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "visible") || strings.Contains(response.Body.String(), "other-site") || strings.Contains(response.Body.String(), "other-tenant") {
		t.Fatalf("alert isolation failed: %d %s", response.Code, response.Body.String())
	}
}

func TestListAlertsRequiresScopeAndRepository(t *testing.T) {
	principal := viewer()
	handler := NewWithAlerts(fakeVerifier{principal: principal}, leakyRepository{}, nil, &alerting.MemoryRepository{})
	if response := request(handler, "/v1/alerts", "valid", "tenant-a"); response.Code != http.StatusForbidden {
		t.Fatalf("missing scope: expected 403, got %d", response.Code)
	}
	principal.Scopes = append(principal.Scopes, "alerts:read")
	handler = NewWithAlerts(fakeVerifier{principal: principal}, leakyRepository{}, nil, nil)
	if response := request(handler, "/v1/alerts", "valid", "tenant-a"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing repository: expected 503, got %d", response.Code)
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

func TestIngestEventRequiresAuthorizedSiteAndDeduplicates(t *testing.T) {
	principal := viewer()
	principal.TenantID = "11111111-1111-4111-8111-111111111111"
	principal.SiteIDs = []string{"33333333-3333-4333-8333-333333333333"}
	principal.Roles = []identity.Role{identity.RoleSiteAdmin}
	principal.Scopes = []string{"events:write"}
	repository := eventing.NewMemoryRepository()
	handler := New(fakeVerifier{principal: principal}, leakyRepository{}, repository)
	requestID := "66666666-6666-4666-8666-666666666666"

	response := eventRequest(handler, validEventBody, "valid", principal.TenantID, requestID)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"accepted":true`) {
		t.Fatalf("event ingest failed: %d %s", response.Code, response.Body.String())
	}
	replay := eventRequest(handler, validEventBody, "valid", principal.TenantID, requestID)
	if replay.Code != http.StatusAccepted || replay.Header().Get("Idempotent-Replayed") != "true" || replay.Body.String() != response.Body.String() {
		t.Fatalf("event replay failed: %d %s", replay.Code, replay.Body.String())
	}
	conflictBody := strings.Replace(validEventBody, `"confidence":0.91`, `"confidence":0.50`, 1)
	conflict := eventRequest(handler, conflictBody, "valid", principal.TenantID, requestID)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_REPLAY") {
		t.Fatalf("event conflict failed: %d %s", conflict.Code, conflict.Body.String())
	}
}

func TestIngestEventFailsClosedAndRequiresPendingHumanReview(t *testing.T) {
	principal := viewer()
	principal.TenantID = "11111111-1111-4111-8111-111111111111"
	principal.SiteIDs = []string{"33333333-3333-4333-8333-333333333333"}
	principal.Roles = []identity.Role{identity.RoleOperator}
	principal.Scopes = []string{"events:write"}
	requestID := "66666666-6666-4666-8666-666666666666"

	forbidden := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), validEventBody, "valid", principal.TenantID, requestID)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("operator event ingest: expected 403, got %d", forbidden.Code)
	}
	principal.Roles = []identity.Role{identity.RoleSiteAdmin}
	missing := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}), validEventBody, "valid", principal.TenantID, requestID)
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing event repository: expected 503, got %d", missing.Code)
	}
	invalidReview := strings.Replace(validEventBody, `"requires_human_review":true`, `"requires_human_review":false`, 1)
	invalid := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), invalidReview, "valid", principal.TenantID, requestID)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-review event: expected 422, got %d %s", invalid.Code, invalid.Body.String())
	}
	badRequestID := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), validEventBody, "valid", principal.TenantID, "not-a-uuid")
	if badRequestID.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid request id: expected 422, got %d", badRequestID.Code)
	}
}

func TestValidateDetectionEventRejectsEveryBoundary(t *testing.T) {
	valid := eventing.DetectionEvent{
		EventID: "22222222-2222-4222-8222-222222222222", TenantID: "11111111-1111-4111-8111-111111111111",
		DedupeKey: "camera-1:42", OccurredAt: time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC),
		SiteID: "33333333-3333-4333-8333-333333333333", CameraID: "44444444-4444-4444-8444-444444444444",
		ZoneID: "55555555-5555-4555-8555-555555555555", EventType: "intrusion", ModelVersion: "detector-1",
		Confidence: 0.91, EvidenceRefs: []string{"evidence://clip-1"}, RequiresHumanReview: true, ReviewState: "pending",
	}
	if err := validateDetectionEvent(valid, valid.TenantID); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*eventing.DetectionEvent)
	}{
		{"invalid identifier", func(event *eventing.DetectionEvent) { event.CameraID = "bad" }},
		{"tenant mismatch", func(event *eventing.DetectionEvent) { event.TenantID = "77777777-7777-4777-8777-777777777777" }},
		{"missing dedupe", func(event *eventing.DetectionEvent) { event.DedupeKey = "" }},
		{"missing time", func(event *eventing.DetectionEvent) { event.OccurredAt = time.Time{} }},
		{"invalid type", func(event *eventing.DetectionEvent) { event.EventType = "theft_detected" }},
		{"missing model", func(event *eventing.DetectionEvent) { event.ModelVersion = "" }},
		{"invalid confidence", func(event *eventing.DetectionEvent) { event.Confidence = 1.1 }},
		{"resolved state", func(event *eventing.DetectionEvent) { event.ReviewState = "confirmed" }},
		{"too many evidence refs", func(event *eventing.DetectionEvent) { event.EvidenceRefs = make([]string, 33) }},
		{"blank evidence ref", func(event *eventing.DetectionEvent) { event.EvidenceRefs = []string{" "} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			test.mutate(&event)
			if err := validateDetectionEvent(event, valid.TenantID); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestIngestEventMapsScopeAndRepositoryFailures(t *testing.T) {
	principal := viewer()
	principal.TenantID = "11111111-1111-4111-8111-111111111111"
	principal.SiteIDs = []string{"33333333-3333-4333-8333-333333333333"}
	principal.Roles = []identity.Role{identity.RoleSiteAdmin}
	principal.Scopes = []string{"events:write"}
	requestID := "66666666-6666-4666-8666-666666666666"

	if response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), validEventBody, "valid", "", requestID); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing tenant: expected 422, got %d", response.Code)
	}
	if response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), validEventBody, "valid", "77777777-7777-4777-8777-777777777777", requestID); response.Code != http.StatusNotFound {
		t.Fatalf("cross tenant: expected 404, got %d", response.Code)
	}
	crossSite := strings.Replace(validEventBody, "33333333-3333-4333-8333-333333333333", "77777777-7777-4777-8777-777777777777", 1)
	if response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), crossSite, "valid", principal.TenantID, requestID); response.Code != http.StatusForbidden {
		t.Fatalf("cross site: expected 403, got %d", response.Code)
	}
	if response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), `{`, "valid", principal.TenantID, requestID); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid JSON: expected 422, got %d", response.Code)
	}
	if response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, eventing.NewMemoryRepository()), validEventBody+validEventBody, "valid", principal.TenantID, requestID); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("multiple objects: expected 422, got %d", response.Code)
	}

	for _, test := range []struct {
		name string
		err  error
		code int
	}{{"site missing", eventing.ErrSiteNotFound, http.StatusNotFound}, {"event conflict", eventing.ErrEventConflict, http.StatusConflict}, {"database down", errors.New("down"), http.StatusServiceUnavailable}} {
		t.Run(test.name, func(t *testing.T) {
			repository := eventRepositoryFunc(func(context.Context, eventing.IngestCommand) (eventing.IngestResult, error) {
				return eventing.IngestResult{}, test.err
			})
			response := eventRequest(New(fakeVerifier{principal: principal}, leakyRepository{}, repository), validEventBody, "valid", principal.TenantID, requestID)
			if response.Code != test.code {
				t.Fatalf("expected %d, got %d: %s", test.code, response.Code, response.Body.String())
			}
		})
	}
}
