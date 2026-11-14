package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

const (
	ingestTenant    = "11111111-1111-4111-8111-111111111111"
	ingestSite      = "22222222-2222-4222-8222-222222222222"
	ingestOtherSite = "33333333-3333-4333-8333-333333333333"
	ingestCamera    = "44444444-4444-4444-8444-444444444444"
	ingestZone      = "55555555-5555-4555-8555-555555555555"
	otherTenant     = "66666666-6666-4666-8666-666666666666"
)

var testJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}

type ingestSites struct{}

func (ingestSites) ListSites(_ context.Context, tenantID string) ([]tenant.Site, error) {
	return []tenant.Site{{ID: ingestSite, TenantID: tenantID, Name: "Warehouse", Timezone: "Asia/Kolkata"}}, nil
}

func (ingestSites) CreateSite(context.Context, tenant.CreateSiteCommand) (tenant.CreateSiteResult, error) {
	return tenant.CreateSiteResult{}, nil
}

type evidenceReaderFunc func(context.Context, string, string) ([]string, error)

func (f evidenceReaderFunc) EvidenceRefs(ctx context.Context, tenantID, alertID string) ([]string, error) {
	return f(ctx, tenantID, alertID)
}

type ingestFixture struct {
	handler  http.Handler
	keys     *siteingest.MemoryKeyStore
	evidence *siteingest.MemoryEvidenceStore
	targets  *siteingest.MemoryTargetStore
	rawKey   string
	ingested []eventing.IngestCommand
	refs     []string
}

func siteAdmin() identity.Principal {
	return identity.Principal{
		UserID: "admin-1", TenantID: ingestTenant, SiteIDs: []string{ingestSite},
		Roles:       []identity.Role{identity.RoleSiteAdmin},
		Scopes:      []string{"config:read", "config:write", "alerts:read", "events:write"},
		DataClasses: []string{"metadata"}, MFALevel: "aal2",
	}
}

func newIngestFixture(t *testing.T) *ingestFixture {
	t.Helper()
	fixture := &ingestFixture{
		keys:     siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite),
		evidence: siteingest.NewMemoryEvidenceStore(),
		targets:  siteingest.NewMemoryTargetStore(),
	}
	rawKey, _, err := fixture.keys.Create(context.Background(), ingestTenant, ingestSite, "admin-1", "Box")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	fixture.rawKey = rawKey
	events := eventRepositoryFunc(func(_ context.Context, command eventing.IngestCommand) (eventing.IngestResult, error) {
		fixture.ingested = append(fixture.ingested, command)
		return eventing.IngestResult{EventID: command.Event.EventID, Accepted: true}, nil
	})
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	schedule := json.RawMessage(`{"timezone":"Asia/Kolkata","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)
	zoneRepo := zones.NewMemoryRepository([]zones.Zone{{ID: ingestZone, TenantID: ingestTenant, SiteID: ingestSite, CameraID: ingestCamera, Name: "Yard", Kind: "intrusion", Geometry: json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`), Enabled: true, Schedule: schedule, ConfigVersion: 1}})
	alerts := &alerting.MemoryRepository{Alerts: []alerting.Alert{{ID: "77777777-7777-4777-8777-777777777777", TenantID: ingestTenant, SiteID: ingestSite, CameraID: ingestCamera, ZoneID: ingestZone, EventType: "intrusion", Severity: "high", Status: "unacknowledged"}}}
	options := SiteIngestOptions{
		Keys: fixture.keys, Evidence: fixture.evidence, TelegramTargets: fixture.targets, TelegramBotHandle: "SyncCamBot",
		AlertEvidence: evidenceReaderFunc(func(_ context.Context, _ string, _ string) ([]string, error) { return fixture.refs, nil }),
	}
	fixture.handler = NewWithSiteIngest(fakeVerifier{principal: siteAdmin()}, ingestSites{}, events, alerts, nil, nil, cameras, nil, nil, nil, zoneRepo, nil, nil, nil, options)
	return fixture
}

func (f *ingestFixture) do(method, path, token string, body []byte, contentType string, headers map[string]string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	f.handler.ServeHTTP(recorder, req)
	return recorder
}

func siteEvent(siteID string) []byte {
	payload, _ := json.Marshal(map[string]any{
		"event_id": "88888888-8888-4888-8888-888888888888", "tenant_id": ingestTenant,
		"dedupe_key": "intrusion:1", "occurred_at": time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC),
		"site_id": siteID, "camera_id": ingestCamera, "zone_id": ingestZone, "event_type": "intrusion",
		"model_version": "yolox-s-1", "confidence": 0.9, "evidence_refs": []string{},
		"requires_human_review": true, "review_state": "pending",
	})
	return payload
}

func TestSiteKeyIngestsEventsOnlyForItsOwnSite(t *testing.T) {
	fixture := newIngestFixture(t)
	headers := map[string]string{eventRequestHeader: "99999999-9999-4999-8999-999999999999", tenantHeader: otherTenant}

	response := fixture.do(http.MethodPost, "/v1/site-ingest/events", fixture.rawKey, siteEvent(ingestSite), "application/json", headers)
	if response.Code != http.StatusAccepted {
		t.Fatalf("own site event: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.ingested) != 1 || fixture.ingested[0].ActorID == "" || !strings.HasPrefix(fixture.ingested[0].ActorID, "site-key:") {
		t.Fatalf("event not recorded with site actor: %+v", fixture.ingested)
	}

	response = fixture.do(http.MethodPost, "/v1/site-ingest/events", fixture.rawKey, siteEvent(ingestOtherSite), "application/json", headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("other site event must be forbidden: %d", response.Code)
	}
	response = fixture.do(http.MethodPost, "/v1/site-ingest/events", "sck_wrongwrongwrongwrongwrongwrongwrongwrongwrong", siteEvent(ingestSite), "application/json", headers)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key must be rejected: %d", response.Code)
	}
	// A user token is not a site key.
	response = fixture.do(http.MethodPost, "/v1/site-ingest/events", "valid", siteEvent(ingestSite), "application/json", headers)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("user token on site route must be rejected: %d", response.Code)
	}
}

func TestSiteConfigurationReturnsCamerasAndScheduledZones(t *testing.T) {
	fixture := newIngestFixture(t)
	response := fixture.do(http.MethodGet, "/v1/site-ingest/config", fixture.rawKey, nil, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("config: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			TenantID string `json:"tenant_id"`
			Site     struct {
				Timezone string `json:"timezone"`
			} `json:"site"`
			Cameras []siteCamera `json:"cameras"`
			Zones   []zones.Zone `json:"zones"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if body.Data.TenantID != ingestTenant || body.Data.Site.Timezone != "Asia/Kolkata" || len(body.Data.Cameras) != 1 || len(body.Data.Zones) != 1 || len(body.Data.Zones[0].Schedule) == 0 {
		t.Fatalf("unexpected config: %s", response.Body.String())
	}
}

func TestEvidenceUploadAndAlertSnapshotRoundTrip(t *testing.T) {
	fixture := newIngestFixture(t)
	response := fixture.do(http.MethodPost, "/v1/site-ingest/evidence", fixture.rawKey, []byte("not a jpeg"), "image/jpeg", nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-jpeg must be rejected: %d", response.Code)
	}
	response = fixture.do(http.MethodPost, "/v1/site-ingest/evidence", fixture.rawKey, testJPEG, "image/jpeg", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", response.Code, response.Body.String())
	}
	var uploaded struct {
		Data struct {
			Ref string `json:"ref"`
		} `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &uploaded)
	if !strings.HasPrefix(uploaded.Data.Ref, siteingest.SnapshotRefPrefix+ingestTenant+"/"+ingestSite+"/") {
		t.Fatalf("unexpected ref %q", uploaded.Data.Ref)
	}
	fixture.refs = []string{uploaded.Data.Ref}
	headers := map[string]string{tenantHeader: ingestTenant}
	response = fixture.do(http.MethodGet, "/v1/alerts/77777777-7777-4777-8777-777777777777/snapshot", "valid", nil, "", headers)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(response.Body.Bytes(), testJPEG) {
		t.Fatalf("snapshot: %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	fixture.refs = nil
	response = fixture.do(http.MethodGet, "/v1/alerts/77777777-7777-4777-8777-777777777777/snapshot", "valid", nil, "", headers)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing snapshot must be 404: %d", response.Code)
	}
}

func TestCameraReferenceAndHeartbeat(t *testing.T) {
	fixture := newIngestFixture(t)
	response := fixture.do(http.MethodPut, "/v1/site-ingest/cameras/"+ingestCamera+"/reference", fixture.rawKey, testJPEG, "image/jpeg", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("reference upload: %d %s", response.Code, response.Body.String())
	}
	response = fixture.do(http.MethodGet, "/v1/cameras/"+ingestCamera+"/reference", "valid", nil, "", map[string]string{tenantHeader: ingestTenant})
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), testJPEG) {
		t.Fatalf("reference read: %d", response.Code)
	}
	heartbeat := []byte(`{"cameras":{"` + ingestCamera + `":{"online":true}}}`)
	response = fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", fixture.rawKey, heartbeat, "application/json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", response.Code, response.Body.String())
	}
	keys, _ := fixture.keys.List(context.Background(), ingestTenant, ingestSite)
	if len(keys) != 1 || keys[0].LastSeenAt == nil || !keys[0].CameraStatus[ingestCamera].Online {
		t.Fatalf("heartbeat not recorded: %+v", keys)
	}
	response = fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", fixture.rawKey, []byte(`{"cameras":{"not-a-uuid":{"online":true}}}`), "application/json", nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad heartbeat must fail validation: %d", response.Code)
	}
}

func TestSiteKeyLifecycleAndTelegramLinkCodes(t *testing.T) {
	fixture := newIngestFixture(t)
	headers := map[string]string{tenantHeader: ingestTenant}
	response := fixture.do(http.MethodPost, "/v1/site-keys", "valid", []byte(`{"site_id":"`+ingestSite+`","label":"Gate box"}`), "application/json", headers)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create key: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Data   siteingest.SiteKey `json:"data"`
		Secret struct {
			SiteKey string `json:"site_key"`
		} `json:"secret"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Secret.SiteKey, siteingest.KeyPrefix) {
		t.Fatalf("raw key not returned once: %s", response.Body.String())
	}
	response = fixture.do(http.MethodPost, "/v1/site-keys", "valid", []byte(`{"site_id":"`+ingestOtherSite+`"}`), "application/json", headers)
	if response.Code != http.StatusNotFound {
		t.Fatalf("key for unassigned site must be hidden: %d", response.Code)
	}
	response = fixture.do(http.MethodGet, "/v1/site-keys?site_id="+ingestSite, "valid", nil, "", headers)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), created.Secret.SiteKey) {
		t.Fatalf("list keys must not leak secrets: %d", response.Code)
	}
	response = fixture.do(http.MethodPost, "/v1/site-keys/"+created.Data.ID+"/revoke", "valid", nil, "", headers)
	if response.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", response.Code)
	}
	if _, err := fixture.keys.Resolve(context.Background(), created.Secret.SiteKey); err == nil {
		t.Fatal("revoked key still resolves")
	}

	response = fixture.do(http.MethodPost, "/v1/notifications/telegram/link-codes", "valid", []byte(`{"site_id":"`+ingestSite+`"}`), "application/json", headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("link code: %d %s", response.Code, response.Body.String())
	}
	var link struct {
		Data struct {
			Code string `json:"code"`
			Bot  string `json:"bot"`
		} `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &link)
	if len(link.Data.Code) != 8 || link.Data.Bot != "SyncCamBot" {
		t.Fatalf("unexpected link: %s", response.Body.String())
	}
	if _, _, ok, _ := fixture.targets.Redeem(context.Background(), strings.ToLower(link.Data.Code), 42, "Owner"); !ok {
		t.Fatal("link code did not redeem")
	}
	if _, _, ok, _ := fixture.targets.Redeem(context.Background(), link.Data.Code, 43, "Again"); ok {
		t.Fatal("link code redeemed twice")
	}
	response = fixture.do(http.MethodGet, "/v1/notifications/telegram?site_id="+ingestSite, "valid", nil, "", headers)
	var targets struct {
		Data []siteingest.TelegramTarget `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &targets)
	if response.Code != http.StatusOK || len(targets.Data) != 1 || targets.Data[0].ChatID != 42 {
		t.Fatalf("targets: %d %s", response.Code, response.Body.String())
	}
	response = fixture.do(http.MethodDelete, "/v1/notifications/telegram/"+targets.Data[0].ID, "valid", nil, "", headers)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete target: %d", response.Code)
	}
}

func TestSiteIngestRejectsInvalidRequests(t *testing.T) {
	fixture := newIngestFixture(t)
	tenantOnly := map[string]string{tenantHeader: ingestTenant}
	cases := []struct {
		name, method, path, token, contentType string
		body                                   []byte
		headers                                map[string]string
		want                                   int
	}{
		{"missing bearer", http.MethodGet, "/v1/site-ingest/config", "", "", nil, nil, http.StatusUnauthorized},
		{"wrong media type", http.MethodPost, "/v1/site-ingest/evidence", fixture.rawKey, "image/png", testJPEG, nil, http.StatusUnsupportedMediaType},
		{"reference bad camera id", http.MethodPut, "/v1/site-ingest/cameras/nope/reference", fixture.rawKey, "image/jpeg", testJPEG, nil, http.StatusNotFound},
		{"reference unknown camera", http.MethodPut, "/v1/site-ingest/cameras/" + ingestZone + "/reference", fixture.rawKey, "image/jpeg", testJPEG, nil, http.StatusNotFound},
		{"heartbeat bad json", http.MethodPost, "/v1/site-ingest/heartbeat", fixture.rawKey, "application/json", []byte(`{"cameras":`), nil, http.StatusUnprocessableEntity},
		{"reference missing", http.MethodGet, "/v1/cameras/" + ingestCamera + "/reference", "valid", "", nil, tenantOnly, http.StatusNotFound},
		{"snapshot no tenant", http.MethodGet, "/v1/alerts/77777777-7777-4777-8777-777777777777/snapshot", "valid", "", nil, nil, http.StatusUnprocessableEntity},
		{"snapshot wrong tenant", http.MethodGet, "/v1/alerts/77777777-7777-4777-8777-777777777777/snapshot", "valid", "", nil, map[string]string{tenantHeader: otherTenant}, http.StatusNotFound},
		{"snapshot unknown alert", http.MethodGet, "/v1/alerts/" + ingestZone + "/snapshot", "valid", "", nil, tenantOnly, http.StatusNotFound},
		{"keys bad site filter", http.MethodGet, "/v1/site-keys?site_id=nope", "valid", "", nil, tenantOnly, http.StatusUnprocessableEntity},
		{"keys foreign site filter", http.MethodGet, "/v1/site-keys?site_id=" + ingestOtherSite, "valid", "", nil, tenantOnly, http.StatusNotFound},
		{"key bad body", http.MethodPost, "/v1/site-keys", "valid", "application/json", []byte(`{"site_id":"nope"}`), tenantOnly, http.StatusUnprocessableEntity},
		{"key unknown field", http.MethodPost, "/v1/site-keys", "valid", "application/json", []byte(`{"x":1}`), tenantOnly, http.StatusUnprocessableEntity},
		{"revoke unknown key", http.MethodPost, "/v1/site-keys/" + ingestZone + "/revoke", "valid", "", nil, tenantOnly, http.StatusNotFound},
		{"link bad site", http.MethodPost, "/v1/notifications/telegram/link-codes", "valid", "application/json", []byte(`{"site_id":"nope"}`), tenantOnly, http.StatusUnprocessableEntity},
		{"link foreign site", http.MethodPost, "/v1/notifications/telegram/link-codes", "valid", "application/json", []byte(`{"site_id":"` + ingestOtherSite + `"}`), tenantOnly, http.StatusNotFound},
		{"delete unknown target", http.MethodDelete, "/v1/notifications/telegram/" + ingestZone, "valid", "", nil, tenantOnly, http.StatusNotFound},
		{"targets bad site filter", http.MethodGet, "/v1/notifications/telegram?site_id=nope", "valid", "", nil, tenantOnly, http.StatusUnprocessableEntity},
		{"user route without token", http.MethodGet, "/v1/site-keys", "", "", nil, tenantOnly, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		if response := fixture.do(tc.method, tc.path, tc.token, tc.body, tc.contentType, tc.headers); response.Code != tc.want {
			t.Errorf("%s: got %d want %d: %s", tc.name, response.Code, tc.want, response.Body.String())
		}
	}
}

func TestZoneScheduleValidationAtTheAPI(t *testing.T) {
	fixture := newIngestFixture(t)
	headers := map[string]string{tenantHeader: ingestTenant, idempotencyHeader: "zone-1"}
	body := `{"site_id":"` + ingestSite + `","camera_id":"` + ingestCamera + `","name":"Yard","kind":"intrusion","geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]},"schedule":{"timezone":"Nowhere/City","windows":[]}}`
	if response := fixture.do(http.MethodPost, "/v1/zones", "valid", []byte(body), "application/json", headers); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad schedule accepted: %d", response.Code)
	}
	body = strings.Replace(body, `{"timezone":"Nowhere/City","windows":[]}`, `{"timezone":"UTC","windows":[{"days":["sat","sun"],"start":"00:00","end":"23:59"}]}`, 1)
	response := fixture.do(http.MethodPost, "/v1/zones", "valid", []byte(body), "application/json", headers)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"schedule":{"timezone":"UTC"`) {
		t.Fatalf("valid schedule: %d %s", response.Code, response.Body.String())
	}
	update := `{"config_version":1,"schedule":{"timezone":"UTC","windows":[{"days":["xyz"],"start":"00:00","end":"01:00"}]}}`
	if response := fixture.do(http.MethodPatch, "/v1/zones/"+ingestZone, "valid", []byte(update), "application/json", map[string]string{tenantHeader: ingestTenant}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad schedule update accepted: %d", response.Code)
	}
}

func TestSiteIngestRoutesFailClosedWithoutDependencies(t *testing.T) {
	handler := NewWithSiteIngest(fakeVerifier{principal: siteAdmin()}, ingestSites{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SiteIngestOptions{})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/site-ingest/config", nil)
	req.Header.Set("Authorization", "Bearer sck_anything")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing key store must fail closed: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/notifications/telegram", nil)
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set(tenantHeader, ingestTenant)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing telegram store must fail closed: %d", recorder.Code)
	}
}
