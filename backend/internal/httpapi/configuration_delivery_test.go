package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/configdelivery"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

func TestZonePushCreatesVisibleImmutableConfigurationRevision(t *testing.T) {
	zoneRepository := zones.NewMemoryRepository([]zones.Zone{{ID: zoneID, TenantID: cameraTenant, SiteID: cameraSite, Name: "Loading", Kind: "intrusion", Geometry: []byte(polygon()), Enabled: true, ConfigVersion: 1}})
	configuration := configdelivery.NewMemoryRepository(nil)
	handler := NewWithConfiguration(fakeVerifier{principal: cameraPrincipal(identity.RoleSiteAdmin, "config:read", "config:write")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, zoneRepository, configuration)
	response := cameraRequest(handler, http.MethodPost, "/v1/zones/"+zoneID+"/push", "", map[string]string{correlationHeader: "55555555-5555-4555-8555-555555555555"})
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"number":1`) || !strings.Contains(response.Body.String(), "Loading") {
		t.Fatalf("publish configuration: %d %s", response.Code, response.Body.String())
	}
	listed := cameraRequest(handler, http.MethodGet, "/v1/config/versions?site_id="+cameraSite, "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"content_hash"`) {
		t.Fatalf("list configuration revisions: %d %s", listed.Code, listed.Body.String())
	}
	if hidden := cameraRequest(handler, http.MethodGet, "/v1/config/versions?site_id="+otherSite, "", nil); hidden.Code != http.StatusNotFound {
		t.Fatalf("cross-site history should be hidden: %d", hidden.Code)
	}
}

func TestEdgeConfigurationPullReportAndStatus(t *testing.T) {
	configuration := configdelivery.NewMemoryRepository([]configdelivery.DeviceBinding{{ID: statusDeviceID, TenantID: cameraTenant, SiteID: cameraSite}})
	_, err := configuration.Publish(context.Background(), configdelivery.PublishCommand{TenantID: cameraTenant, SiteID: cameraSite, Payload: []byte(`{"zones":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	verifier := deviceVerifierFunc(func(*http.Request) (string, error) { return statusDeviceID, nil })
	handler := NewWithConfiguration(fakeVerifier{principal: cameraPrincipal(identity.RoleViewer, "config:read")}, nil, nil, nil, nil, nil, nil, nil, nil, verifier, nil, configuration)
	path := "/v1/edge/devices/" + statusDeviceID + "/config"
	if response := enrollmentRequest(handler, http.MethodGet, path+"?after_revision=0", "", false, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"number":1`) {
		t.Fatalf("pull revision: %d %s", response.Code, response.Body.String())
	}
	if response := enrollmentRequest(handler, http.MethodGet, path+"?after_revision=1", "", false, nil); response.Code != http.StatusNoContent {
		t.Fatalf("pull unchanged: %d %s", response.Code, response.Body.String())
	}
	statusPath := path + "/status"
	response := enrollmentRequest(handler, http.MethodPost, statusPath, `{"revision":1,"state":"failed","error_message":"atomic apply failed"}`, false, map[string]string{correlationHeader: "55555555-5555-4555-8555-555555555555"})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"failed"`) {
		t.Fatalf("report failure: %d %s", response.Code, response.Body.String())
	}
	response = cameraRequest(handler, http.MethodGet, "/v1/edge/devices/"+statusDeviceID+"/config-status", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"error_message":"atomic apply failed"`) {
		t.Fatalf("get status: %d %s", response.Code, response.Body.String())
	}
}

func TestEdgeConfigurationRoutesFailClosedAndValidateInput(t *testing.T) {
	deviceID := statusDeviceID
	configuration := configdelivery.NewMemoryRepository([]configdelivery.DeviceBinding{{ID: deviceID, TenantID: cameraTenant, SiteID: cameraSite}})
	verified := deviceVerifierFunc(func(*http.Request) (string, error) { return deviceID, nil })
	principal := fakeVerifier{principal: cameraPrincipal(identity.RoleViewer, "config:read")}
	missing := NewWithConfiguration(principal, nil, nil, nil, nil, nil, nil, nil, nil, verified, nil, nil)
	path := "/v1/edge/devices/" + deviceID + "/config"
	if response := enrollmentRequest(missing, http.MethodGet, path, "", false, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing configuration: %d", response.Code)
	}
	denied := NewWithConfiguration(principal, nil, nil, nil, nil, nil, nil, nil, nil, deviceVerifierFunc(func(*http.Request) (string, error) { return "", errors.New("denied") }), nil, configuration)
	if response := enrollmentRequest(denied, http.MethodGet, path, "", false, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unverified device: %d", response.Code)
	}
	handler := NewWithConfiguration(principal, nil, nil, nil, nil, nil, nil, nil, nil, verified, nil, configuration)
	if response := enrollmentRequest(handler, http.MethodGet, path+"?after_revision=-1", "", false, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid after revision: %d", response.Code)
	}
	if response := enrollmentRequest(handler, http.MethodPost, path+"/status", `{"revision":0,"state":"applied"}`, false, map[string]string{correlationHeader: "55555555-5555-4555-8555-555555555555"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status report: %d", response.Code)
	}
}

func TestConfigurationHistoryAndStatusEnforceRequestScope(t *testing.T) {
	configuration := configdelivery.NewMemoryRepository([]configdelivery.DeviceBinding{{ID: statusDeviceID, TenantID: cameraTenant, SiteID: cameraSite}})
	handler := NewWithConfiguration(fakeVerifier{principal: cameraPrincipal(identity.RoleViewer, "config:read")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, configuration)
	if response := cameraRequest(handler, http.MethodGet, "/v1/config/versions?site_id=not-a-uuid", "", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid version site: %d", response.Code)
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/edge/devices/not-a-uuid/config-status", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("invalid device: %d", response.Code)
	}
	if response := cameraRequest(handler, http.MethodGet, "/v1/edge/devices/88888888-8888-4888-8888-888888888888/config-status", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", response.Code)
	}
	if response := request(handler, "/v1/config/versions", "valid", ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing tenant header: %d", response.Code)
	}
	writeOnly := NewWithConfiguration(fakeVerifier{principal: cameraPrincipal(identity.RoleSiteAdmin, "config:write")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, configuration)
	if response := cameraRequest(writeOnly, http.MethodGet, "/v1/config/versions", "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("missing read scope: %d", response.Code)
	}
	unavailable := NewWithConfiguration(fakeVerifier{principal: cameraPrincipal(identity.RoleViewer, "config:read")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if response := cameraRequest(unavailable, http.MethodGet, "/v1/config/versions", "", nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable history: %d", response.Code)
	}
}
