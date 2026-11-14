package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/attendance"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

func attendanceFixture(t *testing.T) (*ingestFixture, *attendance.MemoryRepository) {
	t.Helper()
	fixture := newIngestFixture(t)
	repository := attendance.NewMemoryRepository(ingestTenant + ":" + ingestSite)
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Entrance", LifecycleStatus: "active"}})
	fixture.handler = NewWithSiteIngest(fakeVerifier{principal: siteAdmin()}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, zones.NewMemoryRepository(nil), nil, nil, nil, SiteIngestOptions{
		Keys: fixture.keys, Evidence: siteingest.NewMemoryEvidenceStore(), Attendance: repository,
	})
	return fixture, repository
}

func TestStaffEnrollmentRequiresConsentAndFeedsTheSiteBox(t *testing.T) {
	fixture, _ := attendanceFixture(t)
	headers := map[string]string{tenantHeader: ingestTenant}
	noConsent := `{"site_id":"` + ingestSite + `","display_name":"Asha"}`
	if response := fixture.do(http.MethodPost, "/v1/staff", "valid", []byte(noConsent), "application/json", headers); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "CONSENT_REQUIRED") {
		t.Fatalf("enrollment without consent: %d %s", response.Code, response.Body.String())
	}
	body := `{"site_id":"` + ingestSite + `","display_name":"Asha","employee_code":"E1","consent_confirmed":true}`
	response := fixture.do(http.MethodPost, "/v1/staff", "valid", []byte(body), "application/json", headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("create staff: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Data attendance.Staff `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &created)
	if created.Data.ConsentRecordedBy != "admin-1" {
		t.Fatalf("consent actor not recorded: %+v", created.Data)
	}
	config := fixture.do(http.MethodGet, "/v1/site-ingest/config", fixture.rawKey, nil, "", nil)
	if !strings.Contains(config.Body.String(), created.Data.ID) {
		t.Fatalf("site box config must list enrolled staff: %s", config.Body.String())
	}

	sighting := func(id string) []byte {
		payload, _ := json.Marshal(map[string]any{"id": id, "staff_id": created.Data.ID, "camera_id": ingestCamera, "occurred_at": time.Now().UTC().Add(-time.Minute), "confidence": 0.81})
		return payload
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/attendance", fixture.rawKey, sighting("77777777-7777-4777-8777-777777777777"), "application/json", nil); response.Code != http.StatusAccepted {
		t.Fatalf("sighting: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.do(http.MethodGet, "/v1/attendance?site_id="+ingestSite, "valid", nil, "", headers); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Asha") {
		t.Fatalf("attendance list: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.do(http.MethodGet, "/v1/staff?site_id="+ingestSite, "valid", nil, "", headers); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "E1") {
		t.Fatalf("staff list: %d", response.Code)
	}

	deactivate := fixture.do(http.MethodPatch, "/v1/staff/"+created.Data.ID, "valid", []byte(`{"active":false}`), "application/json", headers)
	if deactivate.Code != http.StatusOK {
		t.Fatalf("deactivate: %d", deactivate.Code)
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/attendance", fixture.rawKey, sighting("88888888-8888-4888-8888-888888888888"), "application/json", nil); response.Code != http.StatusNotFound {
		t.Fatalf("inactive staff sighting: %d", response.Code)
	}
}

func TestAttendanceRejectsInvalidRequests(t *testing.T) {
	fixture, _ := attendanceFixture(t)
	headers := map[string]string{tenantHeader: ingestTenant}
	cases := []struct {
		name, method, path, token string
		body                      string
		headers                   map[string]string
		want                      int
	}{
		{"sighting bad ids", http.MethodPost, "/v1/site-ingest/attendance", fixture.rawKey, `{"id":"x","staff_id":"y","camera_id":"z","occurred_at":"2026-11-13T00:00:00Z","confidence":0.5}`, nil, http.StatusUnprocessableEntity},
		{"sighting from the future", http.MethodPost, "/v1/site-ingest/attendance", fixture.rawKey, `{"id":"` + ingestZone + `","staff_id":"` + ingestZone + `","camera_id":"` + ingestCamera + `","occurred_at":"2099-01-01T00:00:00Z","confidence":0.5}`, nil, http.StatusUnprocessableEntity},
		{"staff missing site", http.MethodGet, "/v1/staff", "valid", "", headers, http.StatusUnprocessableEntity},
		{"staff foreign site", http.MethodPost, "/v1/staff", "valid", `{"site_id":"` + ingestOtherSite + `","display_name":"A","consent_confirmed":true}`, headers, http.StatusNotFound},
		{"staff bad name", http.MethodPost, "/v1/staff", "valid", `{"site_id":"` + ingestSite + `","display_name":"","consent_confirmed":true}`, headers, http.StatusUnprocessableEntity},
		{"update unknown staff", http.MethodPatch, "/v1/staff/" + ingestZone, "valid", `{"active":true}`, headers, http.StatusNotFound},
		{"update without field", http.MethodPatch, "/v1/staff/" + ingestZone, "valid", `{}`, headers, http.StatusUnprocessableEntity},
		{"attendance bad range", http.MethodGet, "/v1/attendance?site_id=" + ingestSite + "&from=2026-11-13T00:00:00Z&to=2026-11-12T00:00:00Z", "valid", "", headers, http.StatusUnprocessableEntity},
		{"attendance bad from", http.MethodGet, "/v1/attendance?site_id=" + ingestSite + "&from=yesterday", "valid", "", headers, http.StatusUnprocessableEntity},
		{"attendance bad to", http.MethodGet, "/v1/attendance?site_id=" + ingestSite + "&to=tomorrow", "valid", "", headers, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		var body []byte
		if tc.body != "" {
			body = []byte(tc.body)
		}
		if response := fixture.do(tc.method, tc.path, tc.token, body, "application/json", tc.headers); response.Code != tc.want {
			t.Errorf("%s: got %d want %d: %s", tc.name, response.Code, tc.want, response.Body.String())
		}
	}
	unavailable := newIngestFixture(t)
	if response := unavailable.do(http.MethodGet, "/v1/attendance?site_id="+ingestSite, "valid", nil, "", headers); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("attendance without repository must fail closed: %d", response.Code)
	}
}
