package eventing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	testTenantID = "11111111-1111-4111-8111-111111111111"
	testEventID  = "22222222-2222-4222-8222-222222222222"
	testSiteID   = "33333333-3333-4333-8333-333333333333"
	testCameraID = "44444444-4444-4444-8444-444444444444"
	testZoneID   = "55555555-5555-4555-8555-555555555555"
	testRequest  = "66666666-6666-4666-8666-666666666666"
)

func testCommand() IngestCommand {
	return IngestCommand{ActorID: "edge-1", RequestID: testRequest, Event: DetectionEvent{
		EventID: testEventID, TenantID: testTenantID, DedupeKey: "camera-1:42",
		OccurredAt: time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC), SiteID: testSiteID,
		CameraID: testCameraID, ZoneID: testZoneID, EventType: "intrusion",
		ModelVersion: "detector-1", Confidence: 0.91, EvidenceRefs: []string{"evidence://clip-1"},
		RequiresHumanReview: true, ReviewState: "pending",
	}}
}

func TestPostgresRepositoryIngestsEventOutboxAndAuditAtomically(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := testCommand()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenantID + ":camera-1:42").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT event_id::text, request_hash").WithArgs(testTenantID, "camera-1:42").WillReturnRows(pgxmock.NewRows([]string{"event_id", "request_hash"}))
	mock.ExpectExec("INSERT INTO events.detection_events").WithArgs(
		testEventID, testTenantID, "camera-1:42", pgxmock.AnyArg(), command.Event.OccurredAt,
		testSiteID, testCameraID, testZoneID, "intrusion", "detector-1", 0.91,
		pgxmock.AnyArg(), true, "pending", pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO messaging.outbox_messages").WithArgs(
		pgxmock.AnyArg(), testTenantID, testEventID, OutboxTopic,
		testTenantID+":"+testCameraID, pgxmock.AnyArg(), pgxmock.AnyArg(), command.Event.OccurredAt,
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(testTenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), testTenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), "edge-1", "event.accepted",
		"detection_event", testEventID, testRequest, pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	result, err := NewPostgresRepository(mock).Ingest(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.Replayed || result.EventID != testEventID {
		t.Fatalf("unexpected result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryReplaysAndRejectsDedupeConflict(t *testing.T) {
	command := testCommand()
	_, hash, err := canonicalPayload(command.Event)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		hash string
		want error
	}{{"replay", hash, nil}, {"conflict", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ErrDedupeConflict}} {
		t.Run(test.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			defer mock.Close()
			mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
			mock.ExpectExec("SELECT set_config").WithArgs(testTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenantID + ":camera-1:42").WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectQuery("SELECT event_id::text, request_hash").WithArgs(testTenantID, "camera-1:42").WillReturnRows(
				pgxmock.NewRows([]string{"event_id", "request_hash"}).AddRow(testEventID, test.hash),
			)
			if test.want == nil {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			result, err := NewPostgresRepository(mock).Ingest(context.Background(), command)
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
			if test.want == nil && (!result.Accepted || !result.Replayed || result.EventID != testEventID) {
				t.Fatalf("unexpected replay: %+v", result)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMemoryRepositoryDedupeAndFailureClassification(t *testing.T) {
	repository := NewMemoryRepository()
	command := testCommand()
	first, err := repository.Ingest(context.Background(), command)
	if err != nil || first.Replayed {
		t.Fatalf("first ingest failed: %+v %v", first, err)
	}
	replay, err := repository.Ingest(context.Background(), command)
	if err != nil || !replay.Replayed || replay.EventID != testEventID {
		t.Fatalf("replay failed: %+v %v", replay, err)
	}
	command.Event.Confidence = 0.5
	if _, err := repository.Ingest(context.Background(), command); !errors.Is(err, ErrDedupeConflict) {
		t.Fatalf("expected dedupe conflict, got %v", err)
	}
	if _, err := (*PostgresRepository)(nil).Ingest(context.Background(), command); err == nil {
		t.Fatal("nil repository must fail closed")
	}
	if _, err := NewPostgresRepository(nil).Ingest(context.Background(), command); err == nil {
		t.Fatal("missing pool must fail closed")
	}
	invalid := command
	invalid.Event.TenantID = "not-a-uuid"
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	if _, err := NewPostgresRepository(mock).Ingest(context.Background(), invalid); err == nil {
		t.Fatal("invalid verified tenant must fail before opening a transaction")
	}
	if !errors.Is(classifyInsertError(&pgconn.PgError{Code: "23503"}), ErrSiteNotFound) {
		t.Fatal("foreign key must map to site not found")
	}
	if !errors.Is(classifyInsertError(&pgconn.PgError{Code: "23505"}), ErrEventConflict) {
		t.Fatal("unique violation must map to event conflict")
	}
	if classifyInsertError(errors.New("database unavailable")) == nil {
		t.Fatal("unexpected errors must be preserved")
	}
}
