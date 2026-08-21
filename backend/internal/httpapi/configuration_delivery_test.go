package httpapi

import (
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
