package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

const (
	cameraTenant = "11111111-1111-4111-8111-111111111111"
	cameraSite   = "33333333-3333-4333-8333-333333333333"
	otherSite    = "44444444-4444-4444-8444-444444444444"
	cameraID     = "55555555-5555-4555-8555-555555555555"
)

func cameraPrincipal(role identity.Role, scopes ...string) identity.Principal {
	return identity.Principal{
		UserID: "user-camera", TenantID: cameraTenant, SiteIDs: []string{cameraSite},
		Roles: []identity.Role{role}, Scopes: scopes, MFALevel: "password",
	}
}

func cameraHandler(principal identity.Principal, repository device.Repository) http.Handler {
	return NewWithCameras(fakeVerifier{principal: principal}, nil, nil, nil, nil, nil, repository)
}

func cameraRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set(tenantHeader, cameraTenant)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestListCamerasFiltersRepositoryLeaksAndSiteScope(t *testing.T) {
	repository := device.NewMemoryRepository([]device.Camera{
		{ID: cameraID, TenantID: cameraTenant, SiteID: cameraSite, Name: "Gate", LifecycleStatus: "active"},
		{ID: "66666666-6666-4666-8666-666666666666", TenantID: cameraTenant, SiteID: otherSite, Name: "Hidden", LifecycleStatus: "active"},
		{ID: "77777777-7777-4777-8777-777777777777", TenantID: "88888888-8888-4888-8888-888888888888", SiteID: cameraSite, Name: "Foreign", LifecycleStatus: "active"},
	})
	handler := cameraHandler(cameraPrincipal(identity.RoleViewer, "config:read"), repository)
	response := cameraRequest(handler, http.MethodGet, "/v1/cameras", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Gate") || strings.Contains(response.Body.String(), "Hidden") || strings.Contains(response.Body.String(), "Foreign") {
		t.Fatalf("camera isolation failed: %d %s", response.Code, response.Body.String())
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/cameras?site_id="+otherSite, "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("cross-site filter: expected 404, got %d", response.Code)
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/cameras?site_id=bad", "", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid site: expected 422, got %d", response.Code)
	}
}

func TestGetCameraHidesInvalidAndUnauthorizedResources(t *testing.T) {
	repository := device.NewMemoryRepository([]device.Camera{
		{ID: cameraID, TenantID: cameraTenant, SiteID: cameraSite, Name: "Gate", LifecycleStatus: "active"},
		{ID: "66666666-6666-4666-8666-666666666666", TenantID: cameraTenant, SiteID: otherSite, Name: "Hidden", LifecycleStatus: "active"},
	})
	handler := cameraHandler(cameraPrincipal(identity.RoleViewer, "config:read"), repository)
	if response := cameraRequest(handler, http.MethodGet, "/v1/cameras/"+cameraID, "", nil); response.Code != http.StatusOK {
		t.Fatalf("get camera: expected 200, got %d %s", response.Code, response.Body.String())
	}
	for _, id := range []string{"bad", "66666666-6666-4666-8666-666666666666", "99999999-9999-4999-8999-999999999999"} {
		if response := cameraRequest(handler, http.MethodGet, "/v1/cameras/"+id, "", nil); response.Code != http.StatusNotFound {
			t.Fatalf("hidden camera %s: expected 404, got %d", id, response.Code)
		}
	}
}

func TestCreateCameraRequiresWriteScopeHeadersAndAuthorizedSite(t *testing.T) {
	body := `{"site_id":"` + cameraSite + `","serial_number":"SN-01","name":"Front gate","group_name":"Perimeter","tags":["gate"]}`
	readOnly := cameraHandler(cameraPrincipal(identity.RoleViewer, "config:read"), device.NewMemoryRepository(nil))
	if response := cameraRequest(readOnly, http.MethodPost, "/v1/cameras", body, nil); response.Code != http.StatusForbidden {
		t.Fatalf("viewer create: expected 403, got %d", response.Code)
	}
	principal := cameraPrincipal(identity.RoleSiteAdmin, "config:write")
	handler := cameraHandler(principal, device.NewMemoryRepository(nil))
	if response := cameraRequest(handler, http.MethodPost, "/v1/cameras", body, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing idempotency: expected 422, got %d", response.Code)
	}
	headers := map[string]string{idempotencyHeader: "camera-create"}
	created := cameraRequest(handler, http.MethodPost, "/v1/cameras", body, headers)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"lifecycle_status":"pending"`) || created.Header().Get(correlationHeader) == "" {
		t.Fatalf("create camera failed: %d %s", created.Code, created.Body.String())
	}
	replayed := cameraRequest(handler, http.MethodPost, "/v1/cameras", body, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotent-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("create replay failed: %d %s", replayed.Code, replayed.Body.String())
	}
	conflict := cameraRequest(handler, http.MethodPost, "/v1/cameras", strings.Replace(body, "Front gate", "Back gate", 1), headers)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_REPLAY") {
		t.Fatalf("create conflict failed: %d %s", conflict.Code, conflict.Body.String())
	}
	crossSite := strings.Replace(body, cameraSite, otherSite, 1)
	if response := cameraRequest(handler, http.MethodPost, "/v1/cameras", crossSite, map[string]string{idempotencyHeader: "camera-create-other"}); response.Code != http.StatusNotFound {
		t.Fatalf("cross-site create: expected 404, got %d", response.Code)
	}
}

func TestUpdateAndRetireCameraEnforceVersionLifecycleAndAuditHeaders(t *testing.T) {
	repository := device.NewMemoryRepository([]device.Camera{{
		ID: cameraID, TenantID: cameraTenant, SiteID: cameraSite, SerialNumber: "SN-01", Name: "Gate",
		LifecycleStatus: "pending", ConfigVersion: 1,
	}})
	handler := cameraHandler(cameraPrincipal(identity.RoleSiteAdmin, "config:write"), repository)
	updated := cameraRequest(handler, http.MethodPatch, "/v1/cameras/"+cameraID, `{"config_version":1,"name":"Gate north","lifecycle_status":"active"}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"config_version":2`) || !strings.Contains(updated.Body.String(), `"lifecycle_status":"active"`) {
		t.Fatalf("update camera failed: %d %s", updated.Code, updated.Body.String())
	}
	stale := cameraRequest(handler, http.MethodPatch, "/v1/cameras/"+cameraID, `{"config_version":1,"name":"Stale"}`, nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update: expected 409, got %d %s", stale.Code, stale.Body.String())
	}
	invalid := cameraRequest(handler, http.MethodPatch, "/v1/cameras/"+cameraID, `{"config_version":2,"lifecycle_status":"pending"}`, nil)
	if invalid.Code != http.StatusConflict {
		t.Fatalf("invalid lifecycle: expected 409, got %d %s", invalid.Code, invalid.Body.String())
	}
	retired := cameraRequest(handler, http.MethodDelete, "/v1/cameras/"+cameraID, "", nil)
	if retired.Code != http.StatusOK || !strings.Contains(retired.Body.String(), `"lifecycle_status":"retired"`) {
		t.Fatalf("retire camera failed: %d %s", retired.Code, retired.Body.String())
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/cameras/"+cameraID, "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("write-only principal should not read: expected 403, got %d", response.Code)
	}
}

func TestCameraRoutesFailClosedAndValidateBodies(t *testing.T) {
	principal := cameraPrincipal(identity.RoleSiteAdmin, "config:read", "config:write")
	missing := cameraHandler(principal, nil)
	if response := cameraRequest(missing, http.MethodGet, "/v1/cameras", "", nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing repository: expected 503, got %d", response.Code)
	}
	handler := cameraHandler(principal, device.NewMemoryRepository(nil))
	headers := map[string]string{idempotencyHeader: "invalid"}
	for _, body := range []string{
		`{"site_id":"bad","serial_number":"SN","name":"Gate"}`,
		`{"site_id":"` + cameraSite + `","serial_number":"","name":"Gate"}`,
		`{"site_id":"` + cameraSite + `","serial_number":"SN","name":"Gate","unexpected":true}`,
		`{"site_id":"` + cameraSite + `","serial_number":"SN","name":"Gate"}{}`,
	} {
		if response := cameraRequest(handler, http.MethodPost, "/v1/cameras", body, headers); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid body should be 422: %d %s", response.Code, response.Body.String())
		}
	}
}
