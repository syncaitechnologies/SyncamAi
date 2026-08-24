package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/privacymasks"
)

type releaseTransportStub struct {
	manifest *privacymasks.DeviceReleaseManifest
	status   privacymasks.DeviceReleaseStatus
	err      error
	command  privacymasks.ReportReleaseCommand
}

func (s *releaseTransportStub) Pull(context.Context, string, int64) (privacymasks.PullReleaseResult, error) {
	return privacymasks.PullReleaseResult{Manifest: s.manifest}, s.err
}
func (s *releaseTransportStub) Report(_ context.Context, command privacymasks.ReportReleaseCommand) (privacymasks.DeviceReleaseStatus, error) {
	s.command = command
	return s.status, s.err
}

func TestPrivacyMaskReleaseRoutesRequireMatchingMTLSAndExposeOnlyDedicatedTransport(t *testing.T) {
	stub := &releaseTransportStub{manifest: &privacymasks.DeviceReleaseManifest{ReleaseID: "77777777-7777-4777-8777-777777777777", DeviceID: statusDeviceID, Version: 1, Candidate: []byte(`{"metadata":true}`)}, status: privacymasks.DeviceReleaseStatus{DeviceID: statusDeviceID, State: "accepted", Version: 1}}
	verified := deviceVerifierFunc(func(*http.Request) (string, error) { return statusDeviceID, nil })
	handler := NewWithPrivacyMaskReleaseTransport(fakeVerifier{principal: cameraPrincipal(identity.RoleViewer, "config:read")}, nil, nil, nil, nil, nil, nil, nil, nil, verified, nil, nil, nil, stub)
	path := "/v1/edge/devices/" + statusDeviceID + "/privacy-mask-release"
	if response := enrollmentRequest(handler, http.MethodGet, path+"?after_version=0", "", false, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"release_id"`) {
		t.Fatalf("pull release: %d %s", response.Code, response.Body.String())
	}
	response := enrollmentRequest(handler, http.MethodPost, path+"/status", `{"release_id":"77777777-7777-4777-8777-777777777777","version":1,"state":"accepted"}`, false, map[string]string{correlationHeader: "55555555-5555-4555-8555-555555555555"})
	if response.Code != http.StatusOK || stub.command.DeviceID != statusDeviceID {
		t.Fatalf("report release: %d %s", response.Code, response.Body.String())
	}
	if response := enrollmentRequest(handler, http.MethodGet, path+"?after_version=-1", "", false, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid cursor: %d", response.Code)
	}
	denied := NewWithPrivacyMaskReleaseTransport(fakeVerifier{}, nil, nil, nil, nil, nil, nil, nil, nil, deviceVerifierFunc(func(*http.Request) (string, error) { return "", errors.New("denied") }), nil, nil, nil, stub)
	if response := enrollmentRequest(denied, http.MethodGet, path, "", false, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unverified device: %d", response.Code)
	}
	missing := NewWithPrivacyMaskReleaseTransport(fakeVerifier{}, nil, nil, nil, nil, nil, nil, nil, nil, verified, nil, nil, nil, nil)
	if response := enrollmentRequest(missing, http.MethodGet, path, "", false, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing transport: %d", response.Code)
	}
	stub.manifest = nil
	if response := enrollmentRequest(handler, http.MethodGet, path, "", false, nil); response.Code != http.StatusNoContent {
		t.Fatalf("empty release: %d", response.Code)
	}
	if response := enrollmentRequest(handler, http.MethodPost, path+"/status", `{"release_id":"bad","version":0,"state":"failed"}`, false, map[string]string{correlationHeader: "55555555-5555-4555-8555-555555555555"}); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid report: %d", response.Code)
	}
}
