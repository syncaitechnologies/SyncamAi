package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

const statusDeviceID = "77777777-7777-4777-8777-777777777777"

type deviceVerifierFunc func(*http.Request) (string, error)

func (f deviceVerifierFunc) VerifyDevice(request *http.Request) (string, error) { return f(request) }

func statusHandler(principal identity.Principal, repository device.StatusRepository, verifier device.DeviceIdentityVerifier) http.Handler {
	return NewWithDeviceStatus(fakeVerifier{principal: principal}, nil, nil, nil, nil, nil, nil, nil, repository, verifier)
}

func TestListEdgeDevicesDerivesOfflineAndEnforcesSiteScope(t *testing.T) {
	stale := time.Now().UTC().Add(-device.DeviceOfflineAfter - time.Second)
	repository := device.NewMemoryStatusRepository([]device.EdgeDevice{
		{ID: statusDeviceID, TenantID: cameraTenant, SiteID: cameraSite, SerialNumber: "EDGE-01", HardwareTier: "m", Status: "active", CertificateStatus: "active", ActivatedAt: &stale},
		{ID: "88888888-8888-4888-8888-888888888888", TenantID: cameraTenant, SiteID: otherSite, Status: "active", CertificateStatus: "active", ActivatedAt: &stale},
		{ID: "99999999-9999-4999-8999-999999999999", TenantID: "22222222-2222-4222-8222-222222222222", SiteID: cameraSite, Status: "active", CertificateStatus: "active", ActivatedAt: &stale},
	})
	handler := statusHandler(cameraPrincipal(identity.RoleViewer, "config:read"), repository, nil)
	response := cameraRequest(handler, http.MethodGet, "/v1/edge/devices", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), statusDeviceID) || !strings.Contains(response.Body.String(), `"status":"offline"`) || strings.Contains(response.Body.String(), "88888888") || strings.Contains(response.Body.String(), "99999999") {
		t.Fatalf("fleet isolation failed: %d %s", response.Code, response.Body.String())
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/edge/devices?site_id="+otherSite, "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("cross-site fleet query: expected 404, got %d", response.Code)
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/edge/devices?site_id=bad", "", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid site filter: expected 422, got %d", response.Code)
	}
	if response := cameraRequest(statusHandler(cameraPrincipal(identity.RoleSiteAdmin, "config:write"), repository, nil), http.MethodGet, "/v1/edge/devices", "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("write-only principal: expected 403, got %d", response.Code)
	}
}

func TestDeviceHeartbeatRequiresVerifiedMatchingDeviceAndReplays(t *testing.T) {
	now := time.Now().UTC()
	activated := now.Add(-time.Hour)
	repository := device.NewMemoryStatusRepository([]device.EdgeDevice{{
		ID: statusDeviceID, TenantID: cameraTenant, SiteID: cameraSite, SerialNumber: "EDGE-01", HardwareTier: "m",
		Status: "offline", CertificateStatus: "active", ActivatedAt: &activated,
	}})
	verifier := deviceVerifierFunc(func(*http.Request) (string, error) { return statusDeviceID, nil })
	handler := statusHandler(cameraPrincipal(identity.RoleViewer, "config:read"), repository, verifier)
	body := `{"heartbeat_id":"66666666-6666-4666-8666-666666666666","reported_at":"` + now.Format(time.RFC3339Nano) + `","uptime_seconds":42,"store_forward_depth":7,"firmware_version":"1.2.3"}`
	path := "/v1/edge/devices/" + statusDeviceID + "/heartbeat"
	response := enrollmentRequest(handler, http.MethodPost, path, body, false, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"active"`) || !strings.Contains(response.Body.String(), `"store_forward_depth":7`) || response.Header().Get(correlationHeader) == "" {
		t.Fatalf("heartbeat failed: %d %s", response.Code, response.Body.String())
	}
	replayed := enrollmentRequest(handler, http.MethodPost, path, body, false, nil)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("heartbeat replay failed: %d %s", replayed.Code, replayed.Body.String())
	}
	conflict := enrollmentRequest(handler, http.MethodPost, path, strings.Replace(body, "1.2.3", "1.2.4", 1), false, nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("heartbeat conflict: expected 409, got %d", conflict.Code)
	}
	wrongPath := enrollmentRequest(handler, http.MethodPost, "/v1/edge/devices/88888888-8888-4888-8888-888888888888/heartbeat", body, false, nil)
	if wrongPath.Code != http.StatusUnauthorized {
		t.Fatalf("certificate/path mismatch: expected 401, got %d", wrongPath.Code)
	}
}

func TestDeviceStatusRoutesFailClosedAndValidateHeartbeat(t *testing.T) {
	principal := cameraPrincipal(identity.RoleViewer, "config:read")
	if response := cameraRequest(statusHandler(principal, nil, nil), http.MethodGet, "/v1/edge/devices", "", nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing fleet repository: expected 503, got %d", response.Code)
	}
	repository := device.NewMemoryStatusRepository(nil)
	missingVerifier := statusHandler(principal, repository, nil)
	if response := enrollmentRequest(missingVerifier, http.MethodPost, "/v1/edge/devices/"+statusDeviceID+"/heartbeat", `{}`, false, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing heartbeat verifier: expected 503, got %d", response.Code)
	}
	deniedVerifier := deviceVerifierFunc(func(*http.Request) (string, error) { return "", errors.New("denied") })
	denied := statusHandler(principal, repository, deniedVerifier)
	if response := enrollmentRequest(denied, http.MethodPost, "/v1/edge/devices/"+statusDeviceID+"/heartbeat", `{}`, false, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unverified heartbeat: expected 401, got %d", response.Code)
	}
	verified := deviceVerifierFunc(func(*http.Request) (string, error) { return statusDeviceID, nil })
	handler := statusHandler(principal, repository, verified)
	future := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	for _, body := range []string{
		`{}`,
		`{"heartbeat_id":"bad","reported_at":"2026-08-13T12:00:00Z","uptime_seconds":1,"store_forward_depth":0,"firmware_version":"1"}`,
		`{"heartbeat_id":"66666666-6666-4666-8666-666666666666","reported_at":"` + future + `","uptime_seconds":1,"store_forward_depth":0,"firmware_version":"1"}`,
		`{"heartbeat_id":"66666666-6666-4666-8666-666666666666","reported_at":"2026-08-13T12:00:00Z","uptime_seconds":-1,"store_forward_depth":0,"firmware_version":"1"}`,
		`{"heartbeat_id":"66666666-6666-4666-8666-666666666666","reported_at":"2026-08-13T12:00:00Z","uptime_seconds":1,"store_forward_depth":0,"firmware_version":"1","unknown":true}`,
	} {
		if response := enrollmentRequest(handler, http.MethodPost, "/v1/edge/devices/"+statusDeviceID+"/heartbeat", body, false, nil); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid heartbeat: expected 422, got %d %s", response.Code, response.Body.String())
		}
	}
}
