package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

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

func TestLiveViewUnavailableWithoutHub(t *testing.T) {
	handler := NewWithSiteIngest(fakeVerifier{principal: liveOperator()}, ingestSites{}, nil, nil, nil, nil, device.NewMemoryRepository(nil), nil, nil, nil, nil, nil, nil, nil, SiteIngestOptions{Keys: siteingest.NewMemoryKeyStore()})
	fixture := &ingestFixture{handler: handler}
	response := fixture.do(http.MethodPost, "/v1/cameras/"+ingestCamera+"/live", "valid", nil, "", viewerHeaders)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("live view without a hub must be unavailable: %d", response.Code)
	}
}
