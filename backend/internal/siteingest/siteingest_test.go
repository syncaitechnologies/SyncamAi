package siteingest

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
)

const (
	testTenant = "11111111-1111-4111-8111-111111111111"
	testSite   = "22222222-2222-4222-8222-222222222222"
)

var jpegBytes = []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x01, 0x02}

func TestKeysAreRandomPrefixedAndHashed(t *testing.T) {
	first, err := NewRawKey()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := NewRawKey()
	if first == second || !strings.HasPrefix(first, KeyPrefix) || !validRawKey(first) {
		t.Fatalf("unexpected keys %q %q", first, second)
	}
	if len(HashKey(first)) != 64 || HashKey(first) == HashKey(second) {
		t.Fatal("hash must be a distinct SHA-256 hex digest")
	}
	if validRawKey("sck_short") || validRawKey(strings.Repeat("x", 60)) {
		t.Fatal("malformed keys must be rejected before lookup")
	}
}

func TestMemoryKeyStoreLifecycle(t *testing.T) {
	store := NewMemoryKeyStore(testTenant + ":" + testSite)
	ctx := context.Background()
	if _, _, err := store.Create(ctx, testTenant, "other", "u", "x"); err != ErrSiteNotFound {
		t.Fatalf("unknown site: %v", err)
	}
	raw, key, err := store.Create(ctx, testTenant, testSite, "u", " Box ")
	if err != nil || key.Label != "Box" {
		t.Fatalf("create: %v %+v", err, key)
	}
	scope, err := store.Resolve(ctx, raw)
	if err != nil || scope.TenantID != testTenant || scope.SiteID != testSite {
		t.Fatalf("resolve: %v %+v", err, scope)
	}
	if err := store.RecordHeartbeat(ctx, scope, map[string]CameraStatus{"c": {Online: true}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, "other-tenant", key.ID); err != ErrKeyNotFound {
		t.Fatalf("cross-tenant revoke: %v", err)
	}
	if err := store.Revoke(ctx, testTenant, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(ctx, raw); err != ErrInvalidKey {
		t.Fatalf("revoked key: %v", err)
	}
}

func TestEvidenceReferencesAreTenantBound(t *testing.T) {
	store := NewMemoryEvidenceStore()
	ctx := context.Background()
	if _, err := store.Put(ctx, testTenant, testSite, []byte("png?")); err != ErrInvalidSnapshot {
		t.Fatalf("non-jpeg: %v", err)
	}
	ref, err := store.Put(ctx, testTenant, testSite, jpegBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "99999999-9999-4999-8999-999999999999", ref); err != ErrEvidenceNotFound {
		t.Fatalf("cross-tenant read must fail: %v", err)
	}
	for _, bad := range []string{"other:" + ref, SnapshotRefPrefix + testTenant + "/../x.jpg", SnapshotRefPrefix + testTenant + "/a.png"} {
		if _, err := store.Get(ctx, testTenant, bad); err != ErrEvidenceNotFound {
			t.Fatalf("bad ref %q accepted: %v", bad, err)
		}
	}
	data, err := store.Get(ctx, testTenant, ref)
	if err != nil || string(data) != string(jpegBytes) {
		t.Fatalf("get: %v", err)
	}
}

func TestSupabaseEvidenceStoreUploadsAndDownloads(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "secret" || r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = body
			_, _ = w.Write([]byte(`{"Key":"ok"}`))
		case http.MethodGet:
			data, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	store := &SupabaseEvidenceStore{BaseURL: server.URL, SecretKey: "secret", Bucket: "snapshots", Now: func() time.Time { return time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC) }}
	ctx := context.Background()
	ref, err := store.Put(ctx, testTenant, testSite, jpegBytes)
	if err != nil || !strings.Contains(ref, "/2026/11/12/") {
		t.Fatalf("put: %v %q", err, ref)
	}
	data, err := store.Get(ctx, testTenant, ref)
	if err != nil || string(data) != string(jpegBytes) {
		t.Fatalf("get: %v", err)
	}
	if err := store.PutReference(ctx, testTenant, testSite, "cam", jpegBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetReference(ctx, testTenant, testSite, "missing"); err != ErrEvidenceNotFound {
		t.Fatalf("missing reference: %v", err)
	}
	unauthorized := &SupabaseEvidenceStore{BaseURL: server.URL, SecretKey: "wrong", Bucket: "snapshots"}
	if _, err := unauthorized.Put(ctx, testTenant, testSite, jpegBytes); err == nil {
		t.Fatal("storage rejection must surface")
	}
}

func TestScheduleCaptionsAndTitles(t *testing.T) {
	alert := alerting.Alert{EventType: "weapon_review", Severity: "critical", Confidence: 0.876, OccurredAt: time.Date(2026, 11, 12, 18, 30, 0, 0, time.UTC)}
	caption := Caption(alert, AlertLabels{SiteName: "Warehouse", CameraName: "Gate 1", ZoneName: "Yard", Timezone: "Asia/Kolkata"})
	for _, want := range []string{"Possible weapon", "Yard", "Warehouse", "Gate 1", "13 Nov 2026, 00:00:00", "88%", "CRITICAL"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption missing %q:\n%s", want, caption)
		}
	}
	health := Caption(alerting.Alert{EventType: "camera_health", Severity: "medium"}, AlertLabels{})
	if strings.Contains(health, "Confidence") || !strings.Contains(health, "UTC") {
		t.Fatalf("camera health caption: %s", health)
	}
	if Title("custom_thing") != "🔔 custom thing" {
		t.Fatal("unknown types need a readable title")
	}
}

type telegramCall struct {
	method  string
	chatID  string
	caption string
	photo   []byte
}

func fakeTelegram(t *testing.T) (*httptest.Server, *[]telegramCall, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	calls := []telegramCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		call := telegramCall{method: method}
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(part)
				switch part.FormName() {
				case "chat_id":
					call.chatID = string(data)
				case "caption":
					call.caption = string(data)
				case "photo":
					call.photo = data
				}
			}
		} else if method != "getUpdates" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			call.caption, _ = body["text"].(string)
		}
		calls = append(calls, call)
		if method == "getUpdates" {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":5,"message":{"text":"/start ` + r.URL.Query().Get("code") + `","chat":{"id":7,"first_name":"Ann"}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	return server, &calls, &mu
}

func TestTelegramNotifierSendsPhotoRespectsSeverityAndCooldown(t *testing.T) {
	server, calls, mu := fakeTelegram(t)
	defer server.Close()
	evidence := NewMemoryEvidenceStore()
	ref, _ := evidence.Put(context.Background(), testTenant, testSite, jpegBytes)
	targets := NewMemoryTargetStore(
		TelegramTarget{ID: "a", TenantID: testTenant, SiteID: testSite, ChatID: 100, MinSeverity: "medium"},
		TelegramTarget{ID: "b", TenantID: testTenant, SiteID: testSite, ChatID: 200, MinSeverity: "critical"},
		TelegramTarget{ID: "c", TenantID: testTenant, SiteID: testSite, ChatID: 300, MinSeverity: "low", Muted: true},
	)
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	notifier := &TelegramNotifier{
		Client: &TelegramClient{Token: "token", BaseURL: server.URL}, Targets: targets, Evidence: evidence,
		Describe: func(context.Context, alerting.Alert) AlertLabels { return AlertLabels{ZoneName: "Yard"} },
		Now:      func() time.Time { return now },
	}
	alert := alerting.Alert{TenantID: testTenant, SiteID: testSite, ZoneID: "z", EventType: "intrusion", Severity: "high", Confidence: 0.9}
	notifier.AlertCreated(context.Background(), alert, []string{"other-ref", ref})
	mu.Lock()
	if len(*calls) != 1 || (*calls)[0].method != "sendPhoto" || (*calls)[0].chatID != "100" || string((*calls)[0].photo) != string(jpegBytes) || !strings.Contains((*calls)[0].caption, "Yard") {
		t.Fatalf("unexpected first delivery: %+v", *calls)
	}
	mu.Unlock()

	notifier.AlertCreated(context.Background(), alert, []string{ref})
	now = now.Add(DefaultCooldown + time.Second)
	notifier.AlertCreated(context.Background(), alert, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) != 2 || (*calls)[1].method != "sendMessage" {
		t.Fatalf("cooldown or text fallback failed: %+v", *calls)
	}
}

func TestTelegramLinkerRedeemsStartCodes(t *testing.T) {
	targets := NewMemoryTargetStore()
	code, _, err := targets.CreateLinkCode(context.Background(), testTenant, testSite, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	replies := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			if r.URL.Query().Get("offset") != "0" {
				time.Sleep(20 * time.Millisecond)
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":1,"message":{"text":"/start","chat":{"id":5}}},
				{"update_id":2,"message":{"text":"/start BADCODE1","chat":{"id":6}}},
				{"update_id":3,"message":{"text":"/start ` + code + `","chat":{"id":7,"title":"Security team"}}},
				{"update_id":4,"message":{"text":"hello","chat":{"id":8}}}]}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		replies = append(replies, body["text"].(string))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	RunTelegramLinker(ctx, &TelegramClient{Token: "t", BaseURL: server.URL}, targets, func(context.Context, string, string) string { return "Warehouse" }, t.Logf)
	listed, _ := targets.Targets(context.Background(), testTenant, testSite)
	if len(listed) != 1 || listed[0].ChatID != 7 || listed[0].ChatTitle != "Security team" {
		t.Fatalf("link not stored: %+v", listed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(replies) != 3 || !strings.Contains(replies[0], "Welcome") || !strings.Contains(replies[1], "invalid or expired") || !strings.Contains(replies[2], "Warehouse") {
		t.Fatalf("unexpected replies: %q", replies)
	}
}

func TestTelegramClientNeverLeaksTokenInErrors(t *testing.T) {
	client := &TelegramClient{Token: "123:SECRET", BaseURL: "http://127.0.0.1:1"}
	err := client.SendMessage(context.Background(), 1, "x")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("token leaked or no error: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer server.Close()
	client.BaseURL = server.URL
	if err := client.SendPhoto(context.Background(), 1, "x", jpegBytes); err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("telegram rejection must surface: %v", err)
	}
}
