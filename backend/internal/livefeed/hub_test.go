package livefeed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	siteA   = "22222222-2222-4222-8222-222222222222"
	siteB   = "33333333-3333-4333-8333-333333333333"
	camera1 = "44444444-4444-4444-8444-444444444444"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time            { return c.now }
func (c *clock) Advance(step time.Duration) { c.now = c.now.Add(step) }

func newTestHub() (*Hub, *clock) {
	c := &clock{now: time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC)}
	return NewHubWithClock(c.Now), c
}

func TestFramesFlowOnlyWhileSomeoneWatches(t *testing.T) {
	hub, c := newTestHub()
	if hub.Put(tenantA, siteA, camera1, []byte("jpeg")) {
		t.Fatal("a frame nobody asked for must be refused")
	}
	if len(hub.Wanted(tenantA, siteA)) != 0 {
		t.Fatal("no camera is wanted before anyone watches")
	}
	started, ok := hub.Watch(tenantA, siteA, camera1, "user-1")
	if !started || !ok {
		t.Fatalf("first watch starts a session: started=%v ok=%v", started, ok)
	}
	if started, _ := hub.Watch(tenantA, siteA, camera1, "user-1"); started {
		t.Fatal("refreshing an active session is not a new session")
	}
	if got := hub.Wanted(tenantA, siteA); len(got) != 1 || got[0] != camera1 {
		t.Fatalf("wanted = %v", got)
	}
	if len(hub.Wanted(tenantA, siteB)) != 0 {
		t.Fatal("another site never sees this site's wanted cameras")
	}
	if !hub.Put(tenantA, siteA, camera1, []byte("frame-1")) {
		t.Fatal("watched camera accepts frames")
	}
	if data, _, ok := hub.Latest(tenantA, siteA, camera1); !ok || !bytes.Equal(data, []byte("frame-1")) {
		t.Fatalf("latest = %q %v", data, ok)
	}
	if _, _, ok := hub.Latest(tenantA, siteB, camera1); ok {
		t.Fatal("frames are keyed by site")
	}

	c.Advance(ViewerTTL + time.Second)
	if hub.Watching(tenantA, siteA, camera1, "user-1") {
		t.Fatal("a viewer that stopped polling expires")
	}
	if hub.Put(tenantA, siteA, camera1, []byte("frame-2")) {
		t.Fatal("frames stop once the session expired")
	}
	if _, _, ok := hub.Latest(tenantA, siteA, camera1); ok {
		t.Fatal("the last frame is dropped when nobody watches")
	}
	if started, _ := hub.Watch(tenantA, siteA, camera1, "user-1"); !started {
		t.Fatal("watching again after expiry is a new, audited session")
	}
}

func TestStopAndStaleFrames(t *testing.T) {
	hub, c := newTestHub()
	hub.Watch(tenantA, siteA, camera1, "user-1")
	hub.Watch(tenantA, siteA, camera1, "user-2")
	hub.Put(tenantA, siteA, camera1, []byte("frame"))
	hub.Stop(tenantA, siteA, camera1, "user-1")
	if len(hub.Wanted(tenantA, siteA)) != 1 {
		t.Fatal("the camera stays wanted while another viewer remains")
	}
	c.Advance(FrameTTL - time.Second)
	hub.Watching(tenantA, siteA, camera1, "user-2")
	c.Advance(2 * time.Second)
	if _, _, ok := hub.Latest(tenantA, siteA, camera1); ok {
		t.Fatal("a frame older than FrameTTL is not served as live")
	}
	hub.Stop(tenantA, siteA, camera1, "user-2")
	if len(hub.Wanted(tenantA, siteA)) != 0 {
		t.Fatal("no viewers, nothing wanted")
	}
}

func TestViewerLimit(t *testing.T) {
	hub, _ := newTestHub()
	for index := 0; index < MaxViewersPerCamera; index++ {
		if _, ok := hub.Watch(tenantA, siteA, camera1, fmt.Sprintf("user-%d", index)); !ok {
			t.Fatalf("viewer %d refused", index)
		}
	}
	if _, ok := hub.Watch(tenantA, siteA, camera1, "one-too-many"); ok {
		t.Fatal("viewers beyond the limit are refused")
	}
}

func TestPutCopiesTheFrame(t *testing.T) {
	hub, _ := newTestHub()
	hub.Watch(tenantA, siteA, camera1, "user-1")
	frame := []byte("abc")
	hub.Put(tenantA, siteA, camera1, frame)
	frame[0] = 'x'
	if data, _, _ := hub.Latest(tenantA, siteA, camera1); string(data) != "abc" {
		t.Fatalf("stored frame changed with the caller's buffer: %q", data)
	}
}

func TestPostgresAuditorAppendsLiveViewStarted(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	started := time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantA).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":2026-11-15").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantA, "2026-11-15").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantA, "2026-11-15", started, "user-1", "camera.live_view.started", "camera", camera1,
		"55555555-5555-4555-8555-555555555555", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	err = NewPostgresAuditor(mock).LiveViewStarted(context.Background(), Session{
		TenantID: tenantA, SiteID: siteA, CameraID: camera1, ActorID: "user-1", ActorEmail: "op@example.com",
		RequestID: "55555555-5555-4555-8555-555555555555", StartedAt: started,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAuditorFailsClosed(t *testing.T) {
	if err := (*PostgresAuditor)(nil).LiveViewStarted(context.Background(), Session{TenantID: tenantA}); err == nil {
		t.Fatal("a missing auditor must fail")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	if err := NewPostgresAuditor(mock).LiveViewStarted(context.Background(), Session{TenantID: "not-a-uuid"}); err == nil {
		t.Fatal("an invalid tenant must fail")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite}).WillReturnError(errors.New("database down"))
	if err := NewPostgresAuditor(mock).LiveViewStarted(context.Background(), Session{TenantID: tenantA}); err == nil {
		t.Fatal("a database failure must fail")
	}
}
