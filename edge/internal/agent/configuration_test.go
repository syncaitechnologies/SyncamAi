package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const configDeviceID = "33333333-3333-4333-8333-333333333333"

type recordingApplier struct {
	applied []int64
	err     error
}

func (a *recordingApplier) ApplyAtomic(_ context.Context, revision ConfigurationRevision) error {
	if a.err != nil {
		return a.err
	}
	a.applied = append(a.applied, revision.Number)
	return nil
}

func TestConfigurationSynchronizerAppliesAndReportsRevision(t *testing.T) {
	revision := testConfigurationRevision(t, 2)
	reports := make([]map[string]any, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/edge/devices/" + configDeviceID + "/config":
			if r.URL.Query().Get("after_revision") != "1" {
				t.Fatalf("unexpected pull revision: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": revision})
		case "/v1/edge/devices/" + configDeviceID + "/config/status":
			var report map[string]any
			_ = json.NewDecoder(r.Body).Decode(&report)
			reports = append(reports, report)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": report})
		default:
			t.Fatalf("unexpected route: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewConfigurationClient(server.URL, configDeviceID, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingApplier{}
	synchronizer, err := NewConfigurationSynchronizer(client, applier, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if synchronizer.AppliedRevision() != 2 || len(applier.applied) != 1 || len(reports) != 1 || reports[0]["state"] != "applied" {
		t.Fatalf("sync result applied=%d calls=%v reports=%v", synchronizer.AppliedRevision(), applier.applied, reports)
	}
}

func TestConfigurationSynchronizerRollsBackOnApplyFailure(t *testing.T) {
	revision := testConfigurationRevision(t, 2)
	var failed map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": revision})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&failed)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": failed})
	}))
	defer server.Close()
	client, err := NewConfigurationClient(server.URL, configDeviceID, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewConfigurationSynchronizer(client, &recordingApplier{err: errors.New("compile failed")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); err == nil {
		t.Fatal("apply failure must reach caller")
	}
	if synchronizer.AppliedRevision() != 1 || failed["state"] != "failed" {
		t.Fatalf("last known good revision must remain active: revision=%d report=%v", synchronizer.AppliedRevision(), failed)
	}
}

func TestConfigurationSynchronizerUsesHeartbeatHintOnlyForNewerRevision(t *testing.T) {
	client, err := NewConfigurationClient("https://control.example", configDeviceID, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewConfigurationSynchronizer(client, &recordingApplier{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.SyncIfDesired(context.Background(), 3); err != nil {
		t.Fatalf("current heartbeat hint should not pull: %v", err)
	}
	if err := synchronizer.SyncIfDesired(context.Background(), -1); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("negative heartbeat hint: %v", err)
	}
}

func TestAtomicFileApplierRejectsBadRevisionWithoutReplacingCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.json")
	valid := testConfigurationRevision(t, 1)
	if err := (AtomicFileApplier{Path: path}).ApplyAtomic(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.Number = 2
	bad.ContentHash = "bad"
	if err := (AtomicFileApplier{Path: path}).ApplyAtomic(context.Background(), bad); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("bad config must fail before replacement: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("active file changed after rejected config: %v", err)
	}
}

func testConfigurationRevision(t *testing.T, number int64) ConfigurationRevision {
	t.Helper()
	payload := json.RawMessage(`{"zones":[]}`)
	sum := sha256.Sum256(payload)
	return ConfigurationRevision{ID: "11111111-1111-4111-8111-111111111111", TenantID: "22222222-2222-4222-8222-222222222222", SiteID: "33333333-3333-4333-8333-333333333333", Number: number, Payload: payload, ContentHash: hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC()}
}
