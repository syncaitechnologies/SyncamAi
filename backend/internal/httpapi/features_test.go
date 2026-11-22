package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/features"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/livefeed"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

const featureRequestID = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

func workspaceOwner() identity.Principal {
	return identity.Principal{
		UserID: "owner-1", TenantID: ingestTenant, SiteIDs: []string{ingestSite},
		Roles:       []identity.Role{identity.RoleSuperAdmin},
		Scopes:      []string{"config:read", "config:write", "tenant:manage", "alerts:read"},
		DataClasses: []string{"metadata"}, MFALevel: "aal2",
	}
}

type featureFixture struct {
	*ingestFixture
	store *features.MemoryStore
}

func newFeatureFixture(t *testing.T, user identity.Principal) *featureFixture {
	t.Helper()
	fixture := &featureFixture{ingestFixture: &ingestFixture{
		keys:     siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite),
		evidence: siteingest.NewMemoryEvidenceStore(),
		targets:  siteingest.NewMemoryTargetStore(),
	}, store: features.NewMemoryStore()}
	rawKey, _, err := fixture.keys.Create(context.Background(), ingestTenant, ingestSite, "admin-1", "Box")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	fixture.rawKey = rawKey
	fixture.store.Snapshots[ingestTenant] = features.Snapshot{Plan: "trial"}
	fixture.store.Cameras[ingestTenant] = map[string]bool{ingestCamera: true}
	fixture.store.Alerts[ingestTenant] = map[string]int{"intrusion": 4, "restricted_zone": 1, "loitering": 2}
	fixture.store.Sightings[ingestTenant] = 9
	events := eventRepositoryFunc(func(_ context.Context, command eventing.IngestCommand) (eventing.IngestResult, error) {
		fixture.ingested = append(fixture.ingested, command)
		return eventing.IngestResult{EventID: command.Event.EventID, Accepted: true}, nil
	})
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	options := SiteIngestOptions{Keys: fixture.keys, Evidence: fixture.evidence, Features: features.NewService(fixture.store)}
	fixture.handler = NewWithSiteIngest(fakeVerifier{principal: user}, ingestSites{}, events, &alerting.MemoryRepository{}, nil, nil, cameras, nil, nil, nil, zones.NewMemoryRepository(nil), nil, nil, nil, options)
	return fixture
}

func (f *featureFixture) user(method, path, body string) *httptest.ResponseRecorder {
	return f.do(method, path, "valid", []byte(body), "application/json", map[string]string{
		tenantHeader: ingestTenant, correlationHeader: featureRequestID,
	})
}

func decodeFeatures(t *testing.T, body []byte) map[string]featureView {
	t.Helper()
	var payload struct {
		Data []featureView `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode features: %v %s", err, body)
	}
	result := map[string]featureView{}
	for _, view := range payload.Data {
		result[string(view.Key)] = view
	}
	return result
}

func TestFeaturesPageShowsPlanSwitchesAndWhatEachFeatureCaught(t *testing.T) {
	fixture := newFeatureFixture(t, workspaceOwner())
	response := fixture.user(http.MethodGet, "/v1/features", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	views := decodeFeatures(t, response.Body.Bytes())
	if !views["zone_alerts"].On || views["zone_alerts"].Activity30 == nil || *views["zone_alerts"].Activity30 != 5 {
		t.Fatalf("zone alerts: %+v", views["zone_alerts"])
	}
	if *views["attendance"].Activity30 != 9 || *views["loitering"].Activity30 != 2 {
		t.Fatalf("activity: %+v %+v", views["attendance"], views["loitering"])
	}
	if views["weapon"].Included || views["weapon"].Status != features.StatusPreview {
		t.Fatalf("weapon is a preview outside the plan: %+v", views["weapon"])
	}
	if views["fire_smoke"].Status != features.StatusComingSoon || views["fire_smoke"].Activity30 != nil {
		t.Fatalf("coming soon: %+v", views["fire_smoke"])
	}
	if views["zone_alerts"].BoxReady != nil {
		t.Fatal("box readiness is unknown until a box reports")
	}
	if len(views) != len(features.Catalog()) {
		t.Fatalf("every catalog feature is listed: %d", len(views))
	}
}

func TestOnlyWorkspaceSuperAdminsCanSwitchFeatures(t *testing.T) {
	fixture := newFeatureFixture(t, siteAdmin())
	if response := fixture.user(http.MethodPatch, "/v1/features/zone_alerts", `{"enabled":false}`); response.Code != http.StatusForbidden {
		t.Fatalf("a site admin cannot switch features: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.store.Changes) != 0 {
		t.Fatal("a refused change must not be saved")
	}
}

func TestSwitchingFeaturesValidatesAndApplies(t *testing.T) {
	fixture := newFeatureFixture(t, workspaceOwner())
	for _, test := range []struct {
		path, body, code string
		status           int
	}{
		{path: "/v1/features/weapon", body: `{"enabled":true}`, status: http.StatusForbidden, code: "FEATURE_NOT_INCLUDED"},
		{path: "/v1/features/zone_alerts", body: `{"cameras":["45444444-4444-4444-8444-444444444444"]}`, status: http.StatusUnprocessableEntity},
		{path: "/v1/features/zone_alerts", body: `{"cameras":["not-a-uuid"]}`, status: http.StatusUnprocessableEntity},
		{path: "/v1/features/zone_alerts", body: `{"colour":"red"}`, status: http.StatusUnprocessableEntity},
		{path: "/v1/features/camera_health", body: `{"enabled":false}`, status: http.StatusUnprocessableEntity},
		{path: "/v1/features/teleport", body: `{"enabled":true}`, status: http.StatusNotFound},
	} {
		response := fixture.user(http.MethodPatch, test.path, test.body)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) {
			t.Fatalf("%s %s: expected %d %s, got %d %s", test.path, test.body, test.status, test.code, response.Code, response.Body.String())
		}
	}
	response := fixture.user(http.MethodPatch, "/v1/features/zone_alerts", `{"cameras":["`+ingestCamera+`"]}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cameras":["`+ingestCamera+`"]`) {
		t.Fatalf("choose cameras: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.store.Changes) != 1 || fixture.store.Changes[0].RequestID != featureRequestID || fixture.store.Changes[0].ActorID != "owner-1" {
		t.Fatalf("each change is audited with who made it: %+v", fixture.store.Changes)
	}
	config := fixture.do(http.MethodGet, "/v1/site-ingest/config", fixture.rawKey, nil, "", nil)
	if config.Code != http.StatusOK || !strings.Contains(config.Body.String(), `"zone_alerts":{"cameras":["`+ingestCamera+`"],"on":true}`) ||
		!strings.Contains(config.Body.String(), `"weapon":{"cameras":null,"on":false}`) {
		t.Fatalf("the box gets the effective switches: %s", config.Body.String())
	}
}

func TestSwitchedOffFeaturesNeverBecomeAlerts(t *testing.T) {
	fixture := newFeatureFixture(t, workspaceOwner())
	if response := fixture.user(http.MethodPatch, "/v1/features/zone_alerts", `{"enabled":false}`); response.Code != http.StatusOK {
		t.Fatalf("switch off: %d %s", response.Code, response.Body.String())
	}
	response := fixture.do(http.MethodPost, "/v1/site-ingest/events", fixture.rawKey, siteEvent(ingestSite), "application/json",
		map[string]string{eventRequestHeader: "99999999-9999-4999-8999-999999999990"})
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"ignored":"feature_off"`) {
		t.Fatalf("a switched-off event is accepted and dropped: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.ingested) != 0 {
		t.Fatal("no alert may be created for a switched-off feature")
	}
	if response := fixture.user(http.MethodPatch, "/v1/features/zone_alerts", `{"enabled":true}`); response.Code != http.StatusOK {
		t.Fatalf("switch on: %d", response.Code)
	}
	response = fixture.do(http.MethodPost, "/v1/site-ingest/events", fixture.rawKey, siteEvent(ingestSite), "application/json",
		map[string]string{eventRequestHeader: "99999999-9999-4999-8999-999999999991"})
	if response.Code != http.StatusAccepted || len(fixture.ingested) != 1 {
		t.Fatalf("switched back on, events flow again: %d %s", response.Code, response.Body.String())
	}
}

func TestHeartbeatCapabilitiesShowWhetherTheBoxIsReady(t *testing.T) {
	fixture := newFeatureFixture(t, workspaceOwner())
	response := fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", fixture.rawKey,
		[]byte(`{"cameras":{},"capabilities":["zone_alerts","loitering","camera_health","from_the_future"]}`), "application/json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", response.Code, response.Body.String())
	}
	keys, _ := fixture.keys.List(context.Background(), ingestTenant, "")
	stored, ok := fixture.keys.Capabilities(keys[0].ID)
	if !ok || strings.Join(stored, ",") != "zone_alerts,loitering,camera_health" {
		t.Fatalf("known capabilities are stored, unknown ones ignored: %v %v", stored, ok)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", fixture.rawKey, []byte(`{"cameras":{}}`), "application/json", nil); response.Code != http.StatusOK {
		t.Fatalf("older boxes send no capabilities: %d", response.Code)
	}
	fixture.store.Caps[ingestTenant] = []features.Key{features.ZoneAlerts}
	views := decodeFeatures(t, fixture.user(http.MethodGet, "/v1/features", "").Body.Bytes())
	if views["zone_alerts"].BoxReady == nil || !*views["zone_alerts"].BoxReady || *views["weapon"].BoxReady {
		t.Fatalf("readiness follows what the box reported: %+v %+v", views["zone_alerts"].BoxReady, views["weapon"].BoxReady)
	}
}

type fakeFeatureAdmin struct {
	fakeAdmin
	set map[string]bool
}

func (f *fakeFeatureAdmin) ListPlanFeatures(context.Context, string) (map[string]map[string]bool, error) {
	return map[string]map[string]bool{adminWorkspaceID: {"weapon": true}}, nil
}

func (f *fakeFeatureAdmin) SetPlanFeatures(_ context.Context, _, _, _ string, changes map[string]bool) error {
	f.set = changes
	return nil
}

var _ tenant.AdminFeatureRepository = (*fakeFeatureAdmin)(nil)

func TestOwnerSetsWhichFeaturesEachPlanIncludes(t *testing.T) {
	admin := &fakeFeatureAdmin{}
	handler := New(fakeVerifier{principal: platformOperator()}, admin)
	response := adminRequest(handler, http.MethodGet, "/v1/admin/workspaces", "")
	if !strings.Contains(response.Body.String(), `"features":{"attendance":true,"loitering":true,"weapon":true,"zone_alerts":true}`) {
		t.Fatalf("owner list shows included features: %s", response.Body.String())
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/v1/admin/workspaces/"+adminWorkspaceID+"/features", strings.NewReader(`{"weapon":false}`))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set(correlationHeader, featureRequestID)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || admin.set["weapon"] {
		t.Fatalf("set features: %d %s %v", recorder.Code, recorder.Body.String(), admin.set)
	}
	for _, body := range []string{`{"live_view":true}`, `{"weapon":"yes"}`, `{}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/v1/admin/workspaces/"+adminWorkspaceID+"/features", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer valid")
		request.Header.Set(correlationHeader, featureRequestID)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s should be refused, got %d", body, recorder.Code)
		}
	}
	notOwner := adminRequest(New(fakeVerifier{principal: viewer()}, admin), http.MethodPut, "/v1/admin/workspaces/"+adminWorkspaceID+"/features", `{"weapon":true}`)
	if notOwner.Code != http.StatusForbidden {
		t.Fatalf("only operators: %d", notOwner.Code)
	}
}

func TestLiveViewRefusedWhenTheWorkspaceSwitchedItOff(t *testing.T) {
	base := &ingestFixture{keys: siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)}
	auditor := &recordingAuditor{}
	store := features.NewMemoryStore()
	store.Snapshots[ingestTenant] = features.Snapshot{Plan: "active", Settings: map[features.Key]features.Setting{
		features.LiveView: {Feature: features.LiveView, Enabled: false},
	}}
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	options := SiteIngestOptions{Keys: base.keys, Live: livefeed.NewHub(), LiveAudit: auditor, Features: features.NewService(store)}
	base.handler = NewWithSiteIngest(fakeVerifier{principal: liveOperator()}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, nil, nil, nil, nil, options)
	response := base.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusForbidden || errorCode(t, response) != "LIVE_VIEW_OFF" {
		t.Fatalf("expected LIVE_VIEW_OFF, got %d %s", response.Code, response.Body.String())
	}
	if len(auditor.sessions) != 0 {
		t.Fatal("a refused session must not be audited or started")
	}
}
