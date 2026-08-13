package audit

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v5"
)

func TestAppendBuildsAndStoresHashChainEntry(t *testing.T) {
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

	tenantID := "11111111-1111-4111-8111-111111111111"
	occurredAt := time.Date(2026, 8, 12, 10, 30, 0, 123, time.UTC)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs(tenantID + ":2026-08-12").
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").
		WithArgs(tenantID, "2026-08-12").
		WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").
		WithArgs(
			pgxmock.AnyArg(), tenantID, "2026-08-12", occurredAt,
			"user-1", "site.created", "site", "site-1", "22222222-2222-4222-8222-222222222222",
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	hash, err := Append(context.Background(), tx, Event{
		TenantID: tenantID, ActorID: "user-1", Action: "site.created",
		ResourceType: "site", ResourceID: "site-1",
		RequestID:  "22222222-2222-4222-8222-222222222222",
		AfterState: map[string]string{"name": "Pilot"}, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 32 {
		t.Fatalf("expected SHA-256 record hash, got %d bytes", len(hash))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppendFailsClosedWithoutTransaction(t *testing.T) {
	if _, err := Append(context.Background(), nil, Event{}); err == nil {
		t.Fatal("expected missing transaction error")
	}
}
