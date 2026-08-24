package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivacyMaskReleaseSynchronizerPullsAppliesAndReportsThroughDedicatedTransport(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, private)
	reports := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path != "/v1/edge/devices/"+manifest.DeviceID+"/privacy-mask-release" {
				t.Errorf("unexpected pull path %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": manifest})
		case http.MethodPost:
			reports++
			var status PrivacyMaskReleaseStatus
			_ = json.NewDecoder(r.Body).Decode(&status)
			if r.URL.Path != "/v1/edge/devices/"+manifest.DeviceID+"/privacy-mask-release/status" {
				t.Errorf("unexpected report path %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"state": "accepted"}})
		}
	}))
	defer server.Close()
	client, err := NewPrivacyMaskReleaseClient(server.URL, manifest.DeviceID, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{}
	gate, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": public}, applier, client)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewPrivacyMaskReleaseSynchronizer(client, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(applier.manifests) != 1 || reports != 1 || synchronizer.gate.LastAccepted() == nil {
		t.Fatalf("release was not applied and reported safely: manifests=%d reports=%d", len(applier.manifests), reports)
	}
}
