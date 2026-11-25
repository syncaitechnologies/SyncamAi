package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/analytics"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

const (
	doorZone     = "d1111111-1111-4111-8111-111111111111"
	tableZone    = "d2222222-2222-4222-8222-222222222222"
	alertZone    = "d3333333-3333-4333-8333-333333333333"
	foreignZone  = "d4444444-4444-4444-8444-444444444444"
	analyticsRun = "e1111111-1111-4111-8111-111111111111"
)

func analyticsFixture(t *testing.T) (*ingestFixture, *analytics.MemoryStore) {
	t.Helper()
	fixture := &ingestFixture{keys: siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)}
	rawKey, _, err := fixture.keys.Create(context.Background(), ingestTenant, ingestSite, "admin-1", "Box")
	if err != nil {
		t.Fatal(err)
	}
	fixture.rawKey = rawKey
	line := json.RawMessage(`{"type":"LineString","coordinates":[[0.5,0],[0.5,1]]}`)
	square := json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`)
	zoneRepository := zones.NewMemoryRepository([]zones.Zone{
		{ID: doorZone, TenantID: ingestTenant, SiteID: ingestSite, CameraID: ingestCamera, Name: "Front door", Kind: "door_line", Geometry: line, Enabled: true, ConfigVersion: 1},
		{ID: tableZone, TenantID: ingestTenant, SiteID: ingestSite, CameraID: ingestCamera, Name: "Table 4", Kind: "table", Geometry: square, Enabled: true, ConfigVersion: 1},
		{ID: alertZone, TenantID: ingestTenant, SiteID: ingestSite, CameraID: ingestCamera, Name: "Yard", Kind: "intrusion", Geometry: square, Enabled: true, ConfigVersion: 1},
		{ID: foreignZone, TenantID: ingestTenant, SiteID: ingestOtherSite, Name: "Other door", Kind: "door_line", Geometry: line, Enabled: true, ConfigVersion: 1},
	})
	store := analytics.NewMemoryStore()
	store.Names[doorZone] = [2]string{"Front door", "door_line"}
	store.Names[tableZone] = [2]string{"Table 4", "table"}
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Entrance", LifecycleStatus: "active"}})
	fixture.handler = NewWithSiteIngest(fakeVerifier{principal: siteAdmin()}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, zoneRepository, nil, nil, nil,
		SiteIngestOptions{Keys: fixture.keys, Analytics: store})
	return fixture, store
}

func hourRow(zoneID string, hour time.Time, extra string) string {
	return fmt.Sprintf(`{"zone_id":%q,"hour_start":%q,"run_id":%q%s}`, zoneID, hour.Format(time.RFC3339), analyticsRun, extra)
}

func TestSiteBoxUploadsAnonymousCountsForItsOwnCounterZones(t *testing.T) {
	fixture, _ := analyticsFixture(t)
	hour := time.Now().UTC().Truncate(time.Hour)
	upload := func(rows ...string) int {
		body := `{"hours":[` + strings.Join(rows, ",") + `]}`
		return fixture.do(http.MethodPost, "/v1/site-ingest/analytics", fixture.rawKey, []byte(body), "application/json", nil).Code
	}
	if code := upload(hourRow(doorZone, hour, `,"entries":12,"exits":9`), hourRow(tableZone, hour, `,"occupied_seconds":1800,"sessions":2,"session_seconds":3000,"peak_people":4`)); code != http.StatusAccepted {
		t.Fatalf("valid upload: %d", code)
	}
	for name, row := range map[string]string{
		"alert zone":       hourRow(alertZone, hour, `,"entries":1`),
		"other site":       hourRow(foreignZone, hour, `,"entries":1`),
		"not on the hour":  hourRow(doorZone, hour.Add(30*time.Minute), ""),
		"too old":          hourRow(doorZone, hour.Add(-72*time.Hour), ""),
		"too many seconds": hourRow(tableZone, hour, `,"occupied_seconds":4000`),
		"negative":         hourRow(doorZone, hour, `,"entries":-1`),
	} {
		if code := upload(row); code != http.StatusUnprocessableEntity {
			t.Fatalf("%s should be refused, got %d", name, code)
		}
	}
	if response := fixture.do(http.MethodPost, "/v1/site-ingest/analytics", fixture.rawKey, []byte(`{"hours":[]}`), "application/json", nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty upload is refused: %d", response.Code)
	}
}

func TestInsightsSummariseARangeInTheSiteTimeZone(t *testing.T) {
	fixture, store := analyticsFixture(t)
	headers := map[string]string{tenantHeader: ingestTenant}
	_ = store.SaveHours(context.Background(), ingestTenant, ingestSite, []analytics.ZoneHour{
		{ZoneID: doorZone, HourStart: time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC), RunID: analyticsRun, Entries: 30}, // 23 Nov 01:30 in Asia/Kolkata
		{ZoneID: tableZone, HourStart: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC), RunID: analyticsRun, Sessions: 3, SessionSeconds: 5400},
	})
	response := fixture.do(http.MethodGet, "/v1/analytics/insights?site_id="+ingestSite+"&from=2026-11-01&to=2026-11-24", "valid", nil, "", headers)
	if response.Code != http.StatusOK {
		t.Fatalf("insights: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data analytics.Summary `json:"data"`
		Meta map[string]any    `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Meta["timezone"] != "Asia/Kolkata" || len(body.Data.Days) != 2 || body.Data.Days[1].Date != "2026-11-23" || body.Data.Days[1].Entries != 30 {
		t.Fatalf("days in the site's time zone: %+v %v", body.Data.Days, body.Meta)
	}
	if len(body.Data.Zones) != 2 {
		t.Fatalf("zones: %+v", body.Data.Zones)
	}
	for _, query := range []string{"&from=2026-11-24&to=2026-11-01", "&from=2026-01-01&to=2026-12-31", ""} {
		if response := fixture.do(http.MethodGet, "/v1/analytics/insights?site_id="+ingestSite+query, "valid", nil, "", headers); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%q should be refused: %d", query, response.Code)
		}
	}
}

func TestDoorAndTableZonesAreAccepted(t *testing.T) {
	handler := zoneHandler(cameraPrincipal("site_admin", "config:read", "config:write"), zones.NewMemoryRepository(nil))
	door := `{"site_id":"` + cameraSite + `","name":"Front door","kind":"door_line","geometry":{"type":"LineString","coordinates":[[0.5,0.1],[0.5,0.9]]}}`
	if response := cameraRequest(handler, http.MethodPost, "/v1/zones", door, map[string]string{idempotencyHeader: "door"}); response.Code != http.StatusCreated {
		t.Fatalf("door counter: %d %s", response.Code, response.Body.String())
	}
	table := `{"site_id":"` + cameraSite + `","name":"Table 4","kind":"table","geometry":` + polygon() + `}`
	if response := cameraRequest(handler, http.MethodPost, "/v1/zones", table, map[string]string{idempotencyHeader: "table"}); response.Code != http.StatusCreated {
		t.Fatalf("table: %d %s", response.Code, response.Body.String())
	}
	wrong := `{"site_id":"` + cameraSite + `","name":"Bad door","kind":"door_line","geometry":` + polygon() + `}`
	if response := cameraRequest(handler, http.MethodPost, "/v1/zones", wrong, map[string]string{idempotencyHeader: "bad"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a door counter must be a line: %d", response.Code)
	}
}
