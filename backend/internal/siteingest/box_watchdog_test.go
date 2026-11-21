package siteingest

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

type fakePresence struct {
	changes []BoxTransition
	err     error
	asked   time.Duration
}

func (f *fakePresence) Transitions(_ context.Context, offlineAfter time.Duration) ([]BoxTransition, error) {
	f.asked = offlineAfter
	changes := f.changes
	f.changes = nil // each change is reported once, like the database function
	return changes, f.err
}

type sentMessage struct {
	chat int64
	text string
}

type fakeSender struct {
	sent []sentMessage
	fail error
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, text string) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, sentMessage{chatID, text})
	return nil
}

func (f *fakeSender) chats() []int64 {
	chats := []int64{}
	for _, message := range f.sent {
		chats = append(chats, message.chat)
	}
	sort.Slice(chats, func(i, j int) bool { return chats[i] < chats[j] })
	return chats
}

var offlineAt = time.Date(2026, 11, 21, 3, 5, 0, 0, time.UTC) // 20 Nov 22:05 in Toronto

func offlineChange(kind string) BoxTransition {
	return BoxTransition{
		Kind: kind, KeyID: testKeyID, TenantID: testTenant, SiteID: testSite,
		Workspace: "Acme Security", SiteName: "Warehouse", Timezone: "America/Toronto", BoxLabel: "Front PC",
		LastSeenAt: offlineAt, OfflineSince: offlineAt,
	}
}

func TestBoxWatchdogTellsTheSiteChatsAndTheOwnerOnce(t *testing.T) {
	targets := NewMemoryTargetStore(
		TelegramTarget{ID: "t1", TenantID: testTenant, SiteID: testSite, ChatID: 111},
		TelegramTarget{ID: "t2", TenantID: testTenant, SiteID: testSite, ChatID: 222, Muted: true},
		TelegramTarget{ID: "t3", TenantID: testTenant, SiteID: testSite, ChatID: 999},
		TelegramTarget{ID: "t4", TenantID: testTenant, SiteID: "other-site", ChatID: 333},
	)
	store := &fakePresence{changes: []BoxTransition{offlineChange("offline")}}
	sender := &fakeSender{}
	watchdog := &BoxWatchdog{Store: store, Sender: sender, Targets: targets, OwnerChatID: 999}
	watchdog.Check(context.Background())
	if got := sender.chats(); len(got) != 2 || got[0] != 111 || got[1] != 999 {
		t.Fatalf("site chats (not muted, not other sites) and the owner, once each: %v", got)
	}
	if !strings.Contains(sender.sent[0].text, "Site box offline") || !strings.Contains(sender.sent[0].text, "Warehouse (Acme Security)") {
		t.Fatalf("offline notice: %q", sender.sent[0].text)
	}
	if store.asked != DefaultBoxOfflineAfter {
		t.Fatalf("default threshold: %v", store.asked)
	}
	watchdog.Check(context.Background())
	if len(sender.sent) != 2 {
		t.Fatalf("an offline box is announced once: %d messages", len(sender.sent))
	}
}

func TestBoxWatchdogWorksWithoutAnOwnerChatAndSurvivesFailures(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, format) }
	sender := &fakeSender{}
	targets := NewMemoryTargetStore(TelegramTarget{ID: "t1", TenantID: testTenant, SiteID: testSite, ChatID: 111})
	watchdog := &BoxWatchdog{Store: &fakePresence{changes: []BoxTransition{offlineChange("online")}}, Sender: sender, Targets: targets, Logf: logf}
	watchdog.Check(context.Background())
	if len(sender.sent) != 1 || sender.sent[0].chat != 111 || !strings.Contains(sender.sent[0].text, "back online") {
		t.Fatalf("without an owner chat only the site is told: %+v", sender.sent)
	}
	failing := &BoxWatchdog{Store: &fakePresence{err: errors.New("database down")}, Sender: sender, Logf: logf}
	failing.Check(context.Background())
	sender.fail = errors.New("telegram down")
	unsent := &BoxWatchdog{Store: &fakePresence{changes: []BoxTransition{offlineChange("offline")}}, Sender: sender, OwnerChatID: 999, Logf: logf}
	unsent.Check(context.Background())
	if len(logged) < 3 {
		t.Fatalf("failures are logged, not fatal: %v", logged)
	}
	var missing *BoxWatchdog
	missing.Check(context.Background())
}

func TestBoxNoticeUsesTheSiteTimezone(t *testing.T) {
	offline := BoxNotice(offlineChange("offline"), offlineAt.Add(10*time.Minute))
	if !strings.Contains(offline, "20 Nov 22:05 EST") || !strings.Contains(offline, "Front PC has not reported") {
		t.Fatalf("offline text: %q", offline)
	}
	back := BoxNotice(offlineChange("online"), offlineAt.Add(95*time.Minute))
	if !strings.Contains(back, "after 1 h 35 min") || !strings.Contains(back, "24 hours") {
		t.Fatalf("back online text: %q", back)
	}
	unknown := offlineChange("offline")
	unknown.Timezone, unknown.BoxLabel, unknown.Workspace = "Mars/Base", "", ""
	if text := BoxNotice(unknown, offlineAt); !strings.Contains(text, "03:05 UTC") || !strings.Contains(text, "Site box has not reported") {
		t.Fatalf("fallbacks: %q", text)
	}
	for duration, want := range map[time.Duration]string{
		time.Minute: "about a minute", 25 * time.Minute: "25 minutes", 3 * time.Hour: "3 h", 72 * time.Hour: "3 days",
	} {
		if got := friendlyDuration(duration); got != want {
			t.Errorf("friendlyDuration(%v) = %q, want %q", duration, got, want)
		}
	}
}

func TestPostgresBoxPresenceReadsChanges(t *testing.T) {
	mock := newMock(t)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("site_box_transitions").WithArgs(float64(600)).WillReturnRows(pgxmock.NewRows(
		[]string{"kind", "key_id", "tenant_id", "site_id", "workspace", "site_name", "timezone", "box_label", "last_seen_at", "offline_since"}).
		AddRow("offline", testKeyID, testTenant, testSite, "Acme Security", "Warehouse", "America/Toronto", "Front PC", offlineAt, offlineAt))
	mock.ExpectCommit()
	mock.ExpectRollback()
	changes, err := NewPostgresBoxPresence(mock).Transitions(context.Background(), 10*time.Minute)
	if err != nil || len(changes) != 1 || changes[0].Kind != "offline" || changes[0].SiteName != "Warehouse" || !changes[0].OfflineSince.Equal(offlineAt) {
		t.Fatalf("changes: %+v %v", changes, err)
	}
	if _, err := (*PostgresBoxPresence)(nil).Transitions(context.Background(), time.Minute); err == nil {
		t.Fatal("a missing store fails")
	}
}
