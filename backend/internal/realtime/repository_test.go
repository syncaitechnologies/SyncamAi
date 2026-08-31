package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	testTenantID = "11111111-1111-4111-8111-111111111111"
	testSiteID   = "22222222-2222-4222-8222-222222222222"
)

func TestAppendAllocatesSiteSequenceAndStoresEnvelope(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBegin()
	tx, err := mock.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("INSERT INTO syncam_realtime.site_sequences").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(7)))
	mock.ExpectExec("INSERT INTO syncam_realtime.messages").WithArgs(testTenantID, testSiteID, int64(7), TopicAlertState, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	sequence, err := Append(context.Background(), tx, Event{TenantID: testTenantID, SiteID: testSiteID, Topic: TopicAlertState, Payload: map[string]any{"status": "acknowledged"}})
	if err != nil || sequence != 7 {
		t.Fatalf("unexpected append: sequence=%d err=%v", sequence, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryReplaysInOrder(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	created := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT last_sequence").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT COALESCE").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"min"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT tenant_id::text").WithArgs(testTenantID, testSiteID, int64(0), ReplayLimit).WillReturnRows(
		pgxmock.NewRows([]string{"tenant_id", "site_id", "sequence", "topic", "payload", "created_at"}).
			AddRow(testTenantID, testSiteID, int64(1), TopicAlertCreated, []byte(`{"alert":{"id":"a"}}`), created).
			AddRow(testTenantID, testSiteID, int64(2), TopicAlertState, []byte(`{"alert":{"id":"a"}}`), created.Add(time.Second)),
	)
	mock.ExpectCommit()
	replay, err := NewPostgresRepository(mock).Replay(context.Background(), testTenantID, testSiteID, 0)
	if err != nil || replay.Gap || replay.CurrentSequence != 2 || len(replay.Messages) != 2 || replay.Messages[1].Sequence != 2 {
		t.Fatalf("unexpected replay: %+v %v", replay, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositorySignalsExpiredGapAndEmptyCurrent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT last_sequence").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(12)))
	mock.ExpectQuery("SELECT COALESCE").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"min"}).AddRow(int64(8)))
	mock.ExpectCommit()
	replay, err := NewPostgresRepository(mock).Replay(context.Background(), testTenantID, testSiteID, 2)
	if err != nil || !replay.Gap || len(replay.Messages) != 0 {
		t.Fatalf("expected gap: %+v %v", replay, err)
	}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT last_sequence").WithArgs(testTenantID, testSiteID).WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	current, err := NewPostgresRepository(mock).Current(context.Background(), testTenantID, testSiteID)
	if err != nil || current != 0 {
		t.Fatalf("unexpected current sequence: %d %v", current, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRealtimeBoundariesFailClosed(t *testing.T) {
	if _, err := Append(context.Background(), nil, Event{}); err == nil {
		t.Fatal("nil transaction accepted")
	}
	if _, err := NewPostgresRepository(nil).Current(context.Background(), testTenantID, testSiteID); err == nil {
		t.Fatal("nil repository accepted")
	}
	if _, err := NewPostgresRepository(nil).Replay(context.Background(), testTenantID, testSiteID, -1); err == nil {
		t.Fatal("negative sequence accepted")
	}
	if !errors.Is(ErrInvalidTicket, ErrInvalidTicket) {
		t.Fatal("sentinel error changed")
	}
}

func TestAppendAndRepositoryRejectInvalidBoundaries(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBegin()
	tx, err := mock.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{TenantID: "bad", SiteID: testSiteID, Topic: TopicAlertState, Payload: map[string]any{}},
		{TenantID: testTenantID, SiteID: "bad", Topic: TopicAlertState, Payload: map[string]any{}},
		{TenantID: testTenantID, SiteID: testSiteID, Topic: "unknown", Payload: map[string]any{}},
		{TenantID: testTenantID, SiteID: testSiteID, Topic: TopicAlertState, Payload: make(chan int)},
	} {
		if _, err := Append(context.Background(), tx, event); err == nil {
			t.Fatalf("invalid event accepted: %+v", event)
		}
	}
	if _, err := NewPostgresRepository(mock).Current(context.Background(), "bad", testSiteID); err == nil {
		t.Fatal("invalid tenant accepted")
	}
}

func TestCurrentAndAheadResumeBoundary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT last_sequence").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(4)))
	mock.ExpectCommit()
	current, err := NewPostgresRepository(mock).Current(context.Background(), testTenantID, testSiteID)
	if err != nil || current != 4 {
		t.Fatalf("unexpected current: %d %v", current, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT last_sequence").WithArgs(testTenantID, testSiteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(4)))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Replay(context.Background(), testTenantID, testSiteID, 5); err == nil {
		t.Fatal("ahead resume accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
