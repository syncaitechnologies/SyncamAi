package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

func enrollmentHandler(t *testing.T, principal identity.Principal, repository device.EnrollmentRepository) http.Handler {
	t.Helper()
	return NewWithDeviceEnrollment(fakeVerifier{principal: principal}, nil, nil, nil, nil, nil, nil, repository)
}

func enrollmentRepository(t *testing.T) device.EnrollmentRepository {
	t.Helper()
	tokens, err := device.NewClaimTokenManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return device.NewMemoryEnrollmentRepository(tokens)
}

func enrollmentRequest(handler http.Handler, method, path, body string, authenticate bool, headers map[string]string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticate {
		request.Header.Set("Authorization", "Bearer valid")
		request.Header.Set(tenantHeader, cameraTenant)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestIssueDeviceClaimRequiresAdminScopeSiteAndMutationHeaders(t *testing.T) {
	body := `{"site_id":"` + cameraSite + `","serial_number":"EDGE-01","hardware_tier":"m","model":"Jetson Orin"}`
	viewerHandler := enrollmentHandler(t, cameraPrincipal(identity.RoleViewer, "config:read"), enrollmentRepository(t))
	if response := enrollmentRequest(viewerHandler, http.MethodPost, "/v1/device-claims", body, true, nil); response.Code != http.StatusForbidden {
		t.Fatalf("viewer issue: expected 403, got %d", response.Code)
	}
	admin := cameraPrincipal(identity.RoleSiteAdmin, "config:write")
	handler := enrollmentHandler(t, admin, enrollmentRepository(t))
	if response := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", body, true, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing idempotency: expected 422, got %d", response.Code)
	}
	crossSite := strings.Replace(body, cameraSite, otherSite, 1)
	if response := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", crossSite, true, map[string]string{idempotencyHeader: "claim-cross-site"}); response.Code != http.StatusNotFound {
		t.Fatalf("cross-site issue: expected 404, got %d", response.Code)
	}
	invalid := strings.Replace(body, `"hardware_tier":"m"`, `"hardware_tier":"xl"`, 1)
	if response := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", invalid, true, map[string]string{idempotencyHeader: "claim-invalid"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid tier: expected 422, got %d", response.Code)
	}
}

func TestDeviceClaimIssueReplayAndSingleUseActivation(t *testing.T) {
	handler := enrollmentHandler(t, cameraPrincipal(identity.RoleSiteAdmin, "config:write"), enrollmentRepository(t))
	body := `{"site_id":"` + cameraSite + `","serial_number":"EDGE-01","hardware_tier":"m","model":"Jetson Orin"}`
	headers := map[string]string{idempotencyHeader: "claim-issue-1"}
	issued := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", body, true, headers)
	if issued.Code != http.StatusCreated || issued.Header().Get("Cache-Control") != "no-store" || issued.Header().Get(correlationHeader) == "" {
		t.Fatalf("issue failed: %d %s", issued.Code, issued.Body.String())
	}
	var envelope struct {
		Data struct {
			ClaimToken string `json:"claim_token"`
			Claim      struct {
				TenantID string `json:"tenant_id"`
				SiteID   string `json:"site_id"`
				DeviceID string `json:"device_id"`
			} `json:"claim"`
		} `json:"data"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &envelope); err != nil || envelope.Data.ClaimToken == "" || envelope.Data.Claim.TenantID != cameraTenant || envelope.Data.Claim.SiteID != cameraSite {
		t.Fatalf("invalid issue response: %+v %v", envelope, err)
	}
	replayed := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", body, true, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotent-Replayed") != "true" || replayed.Body.String() != issued.Body.String() {
		t.Fatalf("issue replay failed: %d %s", replayed.Code, replayed.Body.String())
	}
	conflictBody := strings.Replace(body, "Jetson Orin", "Different", 1)
	if response := enrollmentRequest(handler, http.MethodPost, "/v1/device-claims", conflictBody, true, headers); response.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict: expected 409, got %d", response.Code)
	}

	activationBody := `{"claim_token":"` + envelope.Data.ClaimToken + `","serial_number":"edge-01"}`
	activationPath := "/v1/edge/devices/" + envelope.Data.Claim.DeviceID + "/pair"
	wrongDevice := enrollmentRequest(handler, http.MethodPost, "/v1/edge/devices/99999999-9999-4999-8999-999999999999/pair", activationBody, false, nil)
	if wrongDevice.Code != http.StatusUnauthorized {
		t.Fatalf("claim must be bound to path device: expected 401, got %d", wrongDevice.Code)
	}
	activated := enrollmentRequest(handler, http.MethodPost, activationPath, activationBody, false, nil)
	if activated.Code != http.StatusOK || !strings.Contains(activated.Body.String(), `"status":"active"`) || !strings.Contains(activated.Body.String(), `"certificate_status":"pending"`) {
		t.Fatalf("activation failed: %d %s", activated.Code, activated.Body.String())
	}
	reused := enrollmentRequest(handler, http.MethodPost, activationPath, activationBody, false, nil)
	if reused.Code != http.StatusUnauthorized || !strings.Contains(reused.Body.String(), "DEVICE_CLAIM_INVALID") {
		t.Fatalf("claim reuse must fail uniformly: %d %s", reused.Code, reused.Body.String())
	}
}

func TestDeviceActivationFailsClosedAndDoesNotRequireOIDC(t *testing.T) {
	principal := cameraPrincipal(identity.RoleSiteAdmin, "config:write")
	missing := enrollmentHandler(t, principal, nil)
	if response := enrollmentRequest(missing, http.MethodPost, "/v1/edge/devices/"+cameraID+"/pair", `{}`, false, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing repository: expected 503, got %d", response.Code)
	}
	handler := enrollmentHandler(t, principal, enrollmentRepository(t))
	for _, body := range []string{`{}`, `{"claim_token":"short","serial_number":"EDGE"}`, `{"claim_token":"` + strings.Repeat("a", 90) + `","serial_number":"EDGE","unknown":true}`} {
		if response := enrollmentRequest(handler, http.MethodPost, "/v1/edge/devices/"+cameraID+"/pair", body, false, nil); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid activation body: expected 422, got %d %s", response.Code, response.Body.String())
		}
	}
}
