package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

const zoneID = "99999999-9999-4999-8999-999999999999"

func zoneHandler(principal identity.Principal, repository zones.Repository) http.Handler {
	return NewWithZones(fakeVerifier{principal: principal}, nil, nil, nil, nil, nil, nil, nil, nil, nil, repository)
}

func polygon() string { return `{"type":"Polygon","coordinates":[[[10,10],[90,10],[90,90],[10,10]]]}` }

func TestZoneRoutesEnforceScopeGeometryAndVersions(t *testing.T) {
	principal := cameraPrincipal(identity.RoleSiteAdmin, "config:read", "config:write")
	repository := zones.NewMemoryRepository(nil)
	handler := zoneHandler(principal, repository)
	body := `{"site_id":"` + cameraSite + `","name":"Loading bay perimeter","kind":"intrusion","geometry":` + polygon() + `}`
	created := cameraRequest(handler, http.MethodPost, "/v1/zones", body, map[string]string{idempotencyHeader: "zone-create"})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"config_version":1`) || !strings.Contains(created.Body.String(), `"enabled":true`) {
		t.Fatalf("create zone failed: %d %s", created.Code, created.Body.String())
	}
	if replay := cameraRequest(handler, http.MethodPost, "/v1/zones", body, map[string]string{idempotencyHeader: "zone-create"}); replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("zone replay failed: %d %s", replay.Code, replay.Body.String())
	}
	if invalid := cameraRequest(handler, http.MethodPost, "/v1/zones", `{"site_id":"`+cameraSite+`","name":"Mask","kind":"mask","geometry":`+polygon()+`}`, map[string]string{idempotencyHeader: "mask"}); invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mask must require its own dual-approval workflow: %d %s", invalid.Code, invalid.Body.String())
	}
	listed := cameraRequest(handler, http.MethodGet, "/v1/zones?site_id="+cameraSite, "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "Loading bay perimeter") {
		t.Fatalf("list zones failed: %d %s", listed.Code, listed.Body.String())
	}
	var id string
	for _, candidate := range []string{zoneID} {
		id = candidate
	}
	// The memory repository creates the UUID. Extracting it is unnecessary here:
	// retrieve the only result through the repository, then exercise the route.
	stored, err := repository.List(t.Context(), cameraTenant, cameraSite)
	if err != nil || len(stored) != 1 {
		t.Fatalf("read stored zone: %v", err)
	}
	id = stored[0].ID
	updated := cameraRequest(handler, http.MethodPatch, "/v1/zones/"+id, `{"config_version":1,"enabled":false}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"config_version":2`) || !strings.Contains(updated.Body.String(), `"enabled":false`) {
		t.Fatalf("update zone failed: %d %s", updated.Code, updated.Body.String())
	}
	if stale := cameraRequest(handler, http.MethodPatch, "/v1/zones/"+id, `{"config_version":1,"enabled":true}`, nil); stale.Code != http.StatusConflict {
		t.Fatalf("stale zone update: %d %s", stale.Code, stale.Body.String())
	}
}

func TestZoneRoutesHideCrossSiteAndRejectInvalidGeometry(t *testing.T) {
	principal := cameraPrincipal(identity.RoleViewer, "config:read")
	repository := zones.NewMemoryRepository([]zones.Zone{{ID: zoneID, TenantID: cameraTenant, SiteID: otherSite, Name: "Hidden", Kind: "intrusion", Geometry: []byte(polygon()), Enabled: true, ConfigVersion: 1}})
	handler := zoneHandler(principal, repository)
	if response := cameraRequest(handler, http.MethodGet, "/v1/zones/"+zoneID, "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("cross-site zone must be hidden: %d", response.Code)
	}
	writer := zoneHandler(cameraPrincipal(identity.RoleSiteAdmin, "config:write"), zones.NewMemoryRepository(nil))
	invalid := `{"site_id":"` + cameraSite + `","name":"Open polygon","kind":"intrusion","geometry":{"type":"Polygon","coordinates":[[[0,0],[1,1],[2,2]]]}}`
	if response := cameraRequest(writer, http.MethodPost, "/v1/zones", invalid, map[string]string{idempotencyHeader: "invalid-zone"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid geometry: %d %s", response.Code, response.Body.String())
	}
}

func TestZoneRoutesValidateAndVersionLoiterDuration(t *testing.T) {
	principal := cameraPrincipal(identity.RoleSiteAdmin, "config:read", "config:write")
	repository := zones.NewMemoryRepository(nil)
	handler := zoneHandler(principal, repository)
	body := `{"site_id":"` + cameraSite + `","name":"Dispatch dwell","kind":"loitering","loiter_seconds":90,"geometry":` + polygon() + `}`
	created := cameraRequest(handler, http.MethodPost, "/v1/zones", body, map[string]string{idempotencyHeader: "loiter-create"})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"loiter_seconds":90`) {
		t.Fatalf("create loiter zone: %d %s", created.Code, created.Body.String())
	}
	stored, err := repository.List(t.Context(), cameraTenant, cameraSite)
	if err != nil || len(stored) != 1 {
		t.Fatalf("read loiter zone: %v", err)
	}
	updated := cameraRequest(handler, http.MethodPatch, "/v1/zones/"+stored[0].ID, `{"config_version":1,"loiter_seconds":120}`, nil)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"loiter_seconds":120`) {
		t.Fatalf("update loiter zone: %d %s", updated.Code, updated.Body.String())
	}
	invalid := cameraRequest(handler, http.MethodPost, "/v1/zones", `{"site_id":"`+cameraSite+`","name":"Perimeter","kind":"intrusion","loiter_seconds":30,"geometry":`+polygon()+`}`, map[string]string{idempotencyHeader: "intrusion-duration"})
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-loiter duration must fail: %d %s", invalid.Code, invalid.Body.String())
	}
}
