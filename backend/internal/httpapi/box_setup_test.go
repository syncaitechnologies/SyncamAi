package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/boxsetup"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

const otherSiteCamera = "77777777-7777-4777-8777-777777777778"

func newSetupFixture(t *testing.T, principal identity.Principal) (*ingestFixture, *siteingest.MemoryKeyStore) {
	t.Helper()
	keys := siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)
	cameras := device.NewMemoryRepository([]device.Camera{
		{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"},
		{ID: otherSiteCamera, TenantID: ingestTenant, SiteID: ingestOtherSite, Name: "Elsewhere", LifecycleStatus: "active"},
	})
	options := SiteIngestOptions{Keys: keys, Pairing: keys, Setup: boxsetup.NewHub()}
	handler := NewWithSiteIngest(fakeVerifier{principal: principal}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, nil, nil, nil, nil, options)
	return &ingestFixture{handler: handler, keys: keys}, keys
}

func pairTestBox(t *testing.T, fixture *ingestFixture) (string, string) {
	t.Helper()
	_, code := issuePairingCode(t, fixture, ingestSite)
	publicKey := boxKeyPEM(t)
	response := redeem(fixture, code, publicKey)
	var body struct {
		Data struct {
			SiteKey string `json:"site_key"`
		} `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Data.SiteKey, publicKey
}

func decodeData(t *testing.T, raw []byte, target any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode: %v %s", err, raw)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("decode data: %v %s", err, envelope.Data)
	}
}

func TestFindCamerasRoundTripThroughTheBoxPoll(t *testing.T) {
	fixture, _ := newSetupFixture(t, pairingAdmin())
	base := "/v1/sites/" + ingestSite + "/setup"
	headers := map[string]string{tenantHeader: ingestTenant}

	var box setupBoxView
	decodeData(t, fixture.do(http.MethodGet, base+"/box", "valid", nil, "", headers).Body.Bytes(), &box)
	if box.Paired {
		t.Fatal("no box is paired yet")
	}
	siteKey, publicKey := pairTestBox(t, fixture)
	fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", siteKey, []byte(`{"cameras":{}}`), "application/json", nil)
	decodeData(t, fixture.do(http.MethodGet, base+"/box", "valid", nil, "", headers).Body.Bytes(), &box)
	if !box.Paired || !box.Online || box.PublicKey != strings.TrimSpace(publicKey) {
		t.Fatalf("paired box: %+v", box)
	}

	created := fixture.do(http.MethodPost, base+"/tasks", "valid", []byte(`{"type":"probe","payload":{"device":{"ip":"192.168.1.108"},"credentials":{"ct":"x"}}}`), "application/json", headers)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var task boxsetup.Task
	decodeData(t, created.Body.Bytes(), &task)

	var poll struct {
		Tasks []boxsetup.Delivery `json:"tasks"`
	}
	decodeData(t, fixture.do(http.MethodGet, "/v1/site-ingest/live", siteKey, nil, "", nil).Body.Bytes(), &poll)
	if len(poll.Tasks) != 1 || poll.Tasks[0].ID != task.ID || !strings.Contains(string(poll.Tasks[0].Payload), `"ct":"x"`) {
		t.Fatalf("the box receives the task with its payload: %+v", poll.Tasks)
	}

	result := []byte(`{"status":"done","result":{"device_id":"192.168.1.108:554","channels":[{"channel":1}]}}`)
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/setup/tasks/"+task.ID+"/result", siteKey, result, "application/json", nil); response.Code != http.StatusNoContent {
		t.Fatalf("box result: %d %s", response.Code, response.Body.String())
	}
	decodeData(t, fixture.do(http.MethodGet, base+"/tasks/"+task.ID, "valid", nil, "", headers).Body.Bytes(), &task)
	if task.Status != "done" || !strings.Contains(string(task.Result), "192.168.1.108:554") {
		t.Fatalf("dashboard reads the result: %+v", task)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/setup/tasks/"+task.ID+"/result", siteKey, []byte(`{"status":"maybe"}`), "application/json", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad status: %d", response.Code)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/setup/tasks/nope/result", siteKey, []byte(`{"status":"done"}`), "application/json", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown task: %d", response.Code)
	}
}

func TestRestartReachesAnOnlineBoxOnly(t *testing.T) {
	fixture, _ := newSetupFixture(t, pairingAdmin())
	base := "/v1/sites/" + ingestSite + "/setup/tasks"
	headers := map[string]string{tenantHeader: ingestTenant}
	restart := []byte(`{"type":"restart"}`)

	if response := fixture.do(http.MethodPost, base, "valid", restart, "application/json", headers); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "BOX_OFFLINE") {
		t.Fatalf("no paired box: %d %s", response.Code, response.Body.String())
	}
	siteKey, _ := pairTestBox(t, fixture)
	if response := fixture.do(http.MethodPost, base, "valid", restart, "application/json", headers); response.Code != http.StatusConflict {
		t.Fatalf("a paired box that never checked in is offline: %d", response.Code)
	}
	fixture.do(http.MethodPost, "/v1/site-ingest/heartbeat", siteKey, []byte(`{"cameras":{}}`), "application/json", nil)
	if response := fixture.do(http.MethodPost, base, "valid", []byte(`{"type":"restart","payload":{"now":true}}`), "application/json", headers); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("restart takes no payload: %d", response.Code)
	}
	created := fixture.do(http.MethodPost, base, "valid", restart, "application/json", headers)
	if created.Code != http.StatusAccepted {
		t.Fatalf("online box: %d %s", created.Code, created.Body.String())
	}
	var task, again boxsetup.Task
	decodeData(t, created.Body.Bytes(), &task)
	decodeData(t, fixture.do(http.MethodPost, base, "valid", restart, "application/json", headers).Body.Bytes(), &again)
	if again.ID != task.ID {
		t.Fatal("a second click reuses the pending restart")
	}
	var poll struct {
		Tasks []boxsetup.Delivery `json:"tasks"`
	}
	decodeData(t, fixture.do(http.MethodGet, "/v1/site-ingest/live", siteKey, nil, "", nil).Body.Bytes(), &poll)
	if len(poll.Tasks) != 1 || poll.Tasks[0].Type != "restart" || len(poll.Tasks[0].Payload) != 0 {
		t.Fatalf("the box receives one restart: %+v", poll.Tasks)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/setup/tasks/"+task.ID+"/result", siteKey, []byte(`{"status":"done","result":{"restarting":true}}`), "application/json", nil); response.Code != http.StatusNoContent {
		t.Fatalf("box confirms: %d", response.Code)
	}
}

func TestSetupTaskValidation(t *testing.T) {
	fixture, _ := newSetupFixture(t, pairingAdmin())
	base := "/v1/sites/" + ingestSite + "/setup/tasks"
	headers := map[string]string{tenantHeader: ingestTenant}
	for name, body := range map[string]string{
		"unknown type":            `{"type":"format"}`,
		"adopt without cameras":   `{"type":"adopt","payload":{"device_id":"d"}}`,
		"adopt bad channel":       `{"type":"adopt","payload":{"device_id":"d","cameras":[{"camera_id":"` + ingestCamera + `","channel":0}]}}`,
		"adopt bad camera id":     `{"type":"adopt","payload":{"device_id":"d","cameras":[{"camera_id":"x","channel":1}]}}`,
		"adopt other site camera": `{"type":"adopt","payload":{"device_id":"d","cameras":[{"camera_id":"` + otherSiteCamera + `","channel":1}]}}`,
	} {
		response := fixture.do(http.MethodPost, base, "valid", []byte(body), "application/json", headers)
		if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusNotFound {
			t.Fatalf("%s must be refused: %d", name, response.Code)
		}
	}
	good := `{"type":"adopt","payload":{"device_id":"192.168.1.108:554","cameras":[{"camera_id":"` + ingestCamera + `","channel":3}]}}`
	if response := fixture.do(http.MethodPost, base, "valid", []byte(good), "application/json", headers); response.Code != http.StatusAccepted {
		t.Fatalf("valid adopt: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.do(http.MethodGet, base+"/missing", "valid", nil, "", headers); response.Code != http.StatusNotFound {
		t.Fatalf("missing task: %d", response.Code)
	}
}

func TestSetupNeedsConfigWriteForTheSite(t *testing.T) {
	reader := siteAdmin()
	reader.Scopes = []string{"config:read", "alerts:read"}
	fixture, _ := newSetupFixture(t, reader)
	headers := map[string]string{tenantHeader: ingestTenant}
	if response := fixture.do(http.MethodPost, "/v1/sites/"+ingestSite+"/setup/tasks", "valid", []byte(`{"type":"scan"}`), "application/json", headers); response.Code != http.StatusForbidden {
		t.Fatalf("without config:write: %d", response.Code)
	}
	fixture, _ = newSetupFixture(t, pairingAdmin())
	if response := fixture.do(http.MethodPost, "/v1/sites/"+ingestOtherSite+"/setup/tasks", "valid", []byte(`{"type":"scan"}`), "application/json", headers); response.Code != http.StatusNotFound {
		t.Fatalf("another site: %d", response.Code)
	}
	if response := fixture.do(http.MethodGet, "/v1/sites/not-a-uuid/setup/box", "valid", nil, "", headers); response.Code != http.StatusNotFound {
		t.Fatalf("bad site id: %d", response.Code)
	}
}

func TestSetupUnavailableWithoutHub(t *testing.T) {
	keys := siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)
	handler := NewWithSiteIngest(fakeVerifier{principal: pairingAdmin()}, ingestSites{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SiteIngestOptions{Keys: keys})
	fixture := &ingestFixture{handler: handler}
	if response := fixture.do(http.MethodPost, "/v1/sites/"+ingestSite+"/setup/tasks", "valid", []byte(`{"type":"scan"}`), "application/json", map[string]string{tenantHeader: ingestTenant}); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("no hub: %d", response.Code)
	}
	raw, _, _ := keys.Create(context.Background(), ingestTenant, ingestSite, "a", "Box")
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/setup/tasks/x/result", raw, []byte(`{"status":"done"}`), "application/json", nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("no hub for results: %d", response.Code)
	}
}
