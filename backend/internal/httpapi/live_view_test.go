package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/livefeed"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

type recordingAuditor struct {
	sessions []livefeed.Session
	err      error
}

func (a *recordingAuditor) LiveViewStarted(_ context.Context, session livefeed.Session) error {
	if a.err != nil {
		return a.err
	}
	a.sessions = append(a.sessions, session)
	return nil
}

func liveOperator() identity.Principal {
	return identity.Principal{
		UserID: "operator-1", Email: "op@example.com", TenantID: ingestTenant, SiteIDs: []string{ingestSite},
		Roles:       []identity.Role{identity.RoleOperator},
		Scopes:      []string{"config:read", "alerts:read", "raw_video:read"},
		DataClasses: []string{"metadata", "raw_video"}, MFALevel: "aal1",
	}
}

type liveFixture struct {
	*ingestFixture
	auditor *recordingAuditor
	hub     *livefeed.Hub
}

func newLiveFixture(t *testing.T, principal identity.Principal) *liveFixture {
	t.Helper()
	base := &ingestFixture{keys: siteingest.NewMemoryKeyStore(ingestTenant + ":" + ingestSite)}
	rawKey, _, err := base.keys.Create(context.Background(), ingestTenant, ingestSite, "admin-1", "Box")
	if err != nil {
		t.Fatal(err)
	}
	base.rawKey = rawKey
	fixture := &liveFixture{ingestFixture: base, auditor: &recordingAuditor{}, hub: livefeed.NewHub()}
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	options := SiteIngestOptions{Keys: base.keys, Live: fixture.hub, LiveAudit: fixture.auditor}
	base.handler = NewWithSiteIngest(fakeVerifier{principal: principal}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, nil, nil, nil, nil, options)
	return fixture
}

var viewerHeaders = map[string]string{tenantHeader: ingestTenant}

func wantedCameras(t *testing.T, fixture *liveFixture) []string {
	t.Helper()
	response := fixture.do(http.MethodGet, "/v1/site-ingest/live", fixture.rawKey, nil, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("wanted list: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Cameras []string `json:"cameras"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data.Cameras
}

func TestLiveViewRelaysFramesOnlyWhileWatchedAndAudits(t *testing.T) {
	fixture := newLiveFixture(t, liveOperator())
	framePath := "/v1/site-ingest/cameras/" + ingestCamera + "/live"

	if got := wantedCameras(t, fixture); len(got) != 0 {
		t.Fatalf("nothing is wanted before anyone watches: %v", got)
	}
	response := fixture.do(http.MethodPut, framePath, fixture.rawKey, testJPEG, "image/jpeg", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("unwanted frame must be refused: %d", response.Code)
	}
	response = fixture.do(http.MethodGet, "/v1/cameras/"+ingestCamera+"/live/frame", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusConflict {
		t.Fatalf("frames need a session first: %d", response.Code)
	}

	response = fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusOK {
		t.Fatalf("start live view: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.auditor.sessions) != 1 || fixture.auditor.sessions[0].ActorEmail != "op@example.com" || fixture.auditor.sessions[0].CameraID != ingestCamera || fixture.auditor.sessions[0].SiteID != ingestSite {
		t.Fatalf("session not audited: %+v", fixture.auditor.sessions)
	}
	fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if len(fixture.auditor.sessions) != 1 {
		t.Fatal("keep-alives of the same session are not audited again")
	}
	if got := wantedCameras(t, fixture); len(got) != 1 || got[0] != ingestCamera {
		t.Fatalf("watched camera must be wanted: %v", got)
	}

	response = fixture.do(http.MethodGet, "/v1/cameras/"+ingestCamera+"/live/frame", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusNotFound {
		t.Fatalf("no frame yet is 404: %d", response.Code)
	}
	response = fixture.do(http.MethodPut, framePath, fixture.rawKey, []byte("not a jpeg"), "image/jpeg", nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-JPEG frame must be rejected: %d", response.Code)
	}
	response = fixture.do(http.MethodPut, framePath, fixture.rawKey, testJPEG, "image/jpeg", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("watched frame upload: %d %s", response.Code, response.Body.String())
	}
	response = fixture.do(http.MethodGet, "/v1/cameras/"+ingestCamera+"/live/frame", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), testJPEG) || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Frame-Captured-At") == "" {
		t.Fatalf("frame read: %d %v", response.Code, response.Header())
	}

	response = fixture.do(http.MethodDelete, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusNoContent {
		t.Fatalf("stop: %d", response.Code)
	}
	if got := wantedCameras(t, fixture); len(got) != 0 {
		t.Fatalf("stopped camera is no longer wanted: %v", got)
	}
	response = fixture.do(http.MethodPut, framePath, fixture.rawKey, testJPEG, "image/jpeg", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("frames after stop must be refused: %d", response.Code)
	}
}

func TestLiveViewNeedsRawVideoPermission(t *testing.T) {
	// siteAdmin() has no raw_video:read scope and no raw_video data class.
	fixture := newLiveFixture(t, siteAdmin())
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusForbidden {
		t.Fatalf("live view without raw_video must be forbidden: %d", response.Code)
	}
	noDataClass := liveOperator()
	noDataClass.DataClasses = []string{"metadata"}
	fixture = newLiveFixture(t, noDataClass)
	response = fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusForbidden {
		t.Fatalf("the raw_video data class is required: %d", response.Code)
	}
	otherSite := liveOperator()
	otherSite.SiteIDs = []string{ingestOtherSite}
	fixture = newLiveFixture(t, otherSite)
	response = fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusNotFound {
		t.Fatalf("a camera at another site must stay hidden: %d", response.Code)
	}
	if len(fixture.auditor.sessions) != 0 || len(fixture.hub.Wanted(ingestTenant, ingestSite)) != 0 {
		t.Fatal("refused requests must not start sessions")
	}
}

func TestLiveViewFailsClosedWhenAuditFails(t *testing.T) {
	fixture := newLiveFixture(t, liveOperator())
	fixture.auditor.err = errors.New("audit down")
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unaudited live view must not start: %d", response.Code)
	}
	if len(fixture.hub.Wanted(ingestTenant, ingestSite)) != 0 {
		t.Fatal("no frames may be requested without an audit record")
	}
}

type fakeLiveUsage struct {
	limit *int
	used  int64
	fail  error
}

func (f *fakeLiveUsage) Load(context.Context, string, time.Time) (*int, int64, error) {
	return f.limit, f.used, f.fail
}

func (f *fakeLiveUsage) Add(_ context.Context, _ string, _ time.Time, seconds int64) error {
	if f.fail != nil {
		return f.fail
	}
	f.used += seconds
	return nil
}

type liveClock struct{ now time.Time }

func (c *liveClock) Now() time.Time { return c.now }

type meteredLiveFixture struct {
	*liveFixture
	clock *liveClock
	store *fakeLiveUsage
	meter *livefeed.Meter
}

// newMeteredLiveFixture wires a hub and meter sharing one fake clock.
func newMeteredLiveFixture(t *testing.T, limitMinutes *int) *meteredLiveFixture {
	t.Helper()
	fixture := newLiveFixture(t, liveOperator())
	clock := &liveClock{now: time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)}
	store := &fakeLiveUsage{limit: limitMinutes}
	meter := livefeed.NewMeterWithClock(store, clock.Now)
	fixture.hub = livefeed.NewHubWithClock(clock.Now)
	cameras := device.NewMemoryRepository([]device.Camera{{ID: ingestCamera, TenantID: ingestTenant, SiteID: ingestSite, Name: "Gate 1", LifecycleStatus: "active"}})
	options := SiteIngestOptions{Keys: fixture.keys, Live: fixture.hub, LiveAudit: fixture.auditor, LiveMeter: meter}
	fixture.handler = NewWithSiteIngest(fakeVerifier{principal: liveOperator()}, ingestSites{}, nil, nil, nil, nil, cameras, nil, nil, nil, nil, nil, nil, nil, options)
	return &meteredLiveFixture{liveFixture: fixture, clock: clock, store: store, meter: meter}
}

func intPointer(value int) *int { return &value }

// watchFor polls the frame endpoint every step while the box keeps sending
// frames, and returns the first response that is not a served frame.
func (f *meteredLiveFixture) watchFor(t *testing.T, step time.Duration, polls int) *httptest.ResponseRecorder {
	t.Helper()
	framePath := "/v1/site-ingest/cameras/" + ingestCamera + "/live"
	for index := 0; index < polls; index++ {
		f.clock.now = f.clock.now.Add(step)
		f.do(http.MethodPut, framePath, f.rawKey, testJPEG, "image/jpeg", nil)
		response := f.do(http.MethodGet, "/v1/cameras/"+ingestCamera+"/live/frame", "valid", nil, "", viewerHeaders)
		if response.Code != http.StatusOK {
			return response
		}
	}
	return nil
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Error.Code
}

func TestLiveViewStopsAtTheMonthlyAllowance(t *testing.T) {
	fixture := newMeteredLiveFixture(t, intPointer(1))
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"remaining_seconds":60`) || !strings.Contains(response.Body.String(), `"max_continuous_seconds":300`) {
		t.Fatalf("start with a one-minute allowance: %d %s", response.Code, response.Body.String())
	}
	stopped := fixture.watchFor(t, 5*time.Second, 20)
	if stopped == nil || stopped.Code != http.StatusForbidden || errorCode(t, stopped) != "LIVE_VIEW_LIMIT" {
		t.Fatalf("viewing must stop when the minutes are used up: %v", stopped)
	}
	if got := wantedCameras(t, fixture.liveFixture); len(got) != 0 {
		t.Fatalf("the box stops sending once the allowance is used: %v", got)
	}
	response = fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusForbidden || errorCode(t, response) != "LIVE_VIEW_LIMIT" || len(fixture.auditor.sessions) != 1 {
		t.Fatalf("no new session once the allowance is used: %d %d", response.Code, len(fixture.auditor.sessions))
	}
	if err := fixture.meter.Flush(context.Background()); err != nil || fixture.store.used < 60 {
		t.Fatalf("viewing time is saved: %d %v", fixture.store.used, err)
	}
}

func TestLiveViewPausesAfterFiveMinutes(t *testing.T) {
	fixture := newMeteredLiveFixture(t, nil)
	if response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"remaining_seconds":null`) {
		t.Fatalf("unlimited start: %d %s", response.Code, response.Body.String())
	}
	paused := fixture.watchFor(t, 5*time.Second, 100)
	if paused == nil || paused.Code != http.StatusConflict || errorCode(t, paused) != "LIVE_VIEW_PAUSED" {
		t.Fatalf("an unlimited workspace still pauses after five minutes: %v", paused)
	}
	if got := wantedCameras(t, fixture.liveFixture); len(got) != 0 {
		t.Fatalf("a paused camera is no longer wanted: %v", got)
	}
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusOK || len(fixture.auditor.sessions) != 2 {
		t.Fatalf("\"still watching\" starts a new audited session: %d %d", response.Code, len(fixture.auditor.sessions))
	}
}

func TestLiveViewOffWhenTheAllowanceIsZero(t *testing.T) {
	fixture := newMeteredLiveFixture(t, intPointer(0))
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusForbidden || errorCode(t, response) != "LIVE_VIEW_LIMIT" || !strings.Contains(response.Body.String(), "not included") {
		t.Fatalf("live view off: %d %s", response.Code, response.Body.String())
	}
	if len(fixture.auditor.sessions) != 0 || len(fixture.hub.Wanted(ingestTenant, ingestSite)) != 0 {
		t.Fatal("a refused start must not audit or request frames")
	}
}

func TestLiveViewFailsClosedWhenTheAllowanceIsUnreadable(t *testing.T) {
	fixture := newMeteredLiveFixture(t, intPointer(10))
	fixture.store.fail = errors.New("database down")
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusServiceUnavailable || len(fixture.auditor.sessions) != 0 {
		t.Fatalf("an unreadable allowance must not start live view: %d", response.Code)
	}
}

func TestLiveViewUsageReport(t *testing.T) {
	fixture := newMeteredLiveFixture(t, intPointer(10))
	fixture.store.used = 90
	response := fixture.do(http.MethodGet, "/v1/live-view/usage", "valid", nil, "", viewerHeaders)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"limit_minutes":10`) || !strings.Contains(body, `"used_seconds":90`) || !strings.Contains(body, `"month":"2026-11-01"`) || !strings.Contains(body, `"resets_at":"2026-12-01T00:00:00Z"`) {
		t.Fatalf("usage report: %d %s", response.Code, body)
	}
	unlimited := newMeteredLiveFixture(t, nil)
	if response := unlimited.do(http.MethodGet, "/v1/live-view/usage", "valid", nil, "", viewerHeaders); !strings.Contains(response.Body.String(), `"limit_minutes":null`) {
		t.Fatalf("unlimited usage report: %s", response.Body.String())
	}
	if response := fixture.do(http.MethodGet, "/v1/live-view/usage", "valid", nil, "", nil); response.Code == http.StatusOK {
		t.Fatal("the usage report needs the tenant header")
	}
}

func TestLiveViewUnavailableWithoutHub(t *testing.T) {
	handler := NewWithSiteIngest(fakeVerifier{principal: liveOperator()}, ingestSites{}, nil, nil, nil, nil, device.NewMemoryRepository(nil), nil, nil, nil, nil, nil, nil, nil, SiteIngestOptions{Keys: siteingest.NewMemoryKeyStore()})
	fixture := &ingestFixture{handler: handler}
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("live view without a hub must be unavailable: %d", response.Code)
	}
}
