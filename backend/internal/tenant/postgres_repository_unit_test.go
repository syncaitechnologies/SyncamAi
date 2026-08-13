package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	unitTenantID  = "11111111-1111-4111-8111-111111111111"
	unitRequestID = "22222222-2222-4222-8222-222222222222"
)

func TestPostgresRepositoryListsRLSScopedSites(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(unitTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT id::text").WillReturnRows(
		pgxmock.NewRows([]string{"id", "tenant_id", "name", "address", "timezone", "status", "created_at"}).
			AddRow("site-1", unitTenantID, "Pilot", "Pune", "Asia/Kolkata", "active", createdAt),
	)
	mock.ExpectCommit()

	sites, err := NewPostgresRepository(mock).ListSites(context.Background(), unitTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Name != "Pilot" || sites[0].CreatedAt != createdAt {
		t.Fatalf("unexpected sites: %+v", sites)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryCreatesAuditedIdempotentSite(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	command := CreateSiteCommand{
		TenantID: unitTenantID, ActorID: "user-1", RequestID: unitRequestID,
		IdempotencyKey: "create-site", Name: " Pilot ", Address: " Pune ", Timezone: "Asia/Kolkata",
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(unitTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(unitTenantID + ":create-site").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(unitTenantID, "create-site").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(unitTenantID, "create-site").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("INSERT INTO config.sites").
		WithArgs(pgxmock.AnyArg(), unitTenantID, "Pilot", "Pune", "Asia/Kolkata", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"created_at"}).AddRow(createdAt))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").
		WithArgs(unitTenantID, "create-site", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(unitTenantID + ":2026-08-12").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(unitTenantID, "2026-08-12").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), unitTenantID, "2026-08-12", createdAt,
		"user-1", "site.created", "site", pgxmock.AnyArg(), unitRequestID,
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	result, err := NewPostgresRepository(mock).CreateSite(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Site.Name != "Pilot" || result.Site.Address != "Pune" || result.Site.CreatedAt != createdAt {
		t.Fatalf("unexpected result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryReplaysExactResponseAndRejectsDifferentBody(t *testing.T) {
	command := CreateSiteCommand{TenantID: unitTenantID, ActorID: "user-1", RequestID: unitRequestID, IdempotencyKey: "create-site", Name: "Pilot", Timezone: "Asia/Kolkata"}
	hash, err := hashCreateSite(command)
	if err != nil {
		t.Fatal(err)
	}
	stored := Site{ID: "33333333-3333-4333-8333-333333333333", TenantID: unitTenantID, Name: "Pilot", Timezone: "Asia/Kolkata", Status: "provisioning", CreatedAt: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)}
	response, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		storedHash string
		wantErr    error
	}{
		{name: "exact", storedHash: hash},
		{name: "different", storedHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantErr: ErrIdempotencyConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			defer mock.Close()
			mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
			mock.ExpectExec("SELECT set_config").WithArgs(unitTenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(unitTenantID + ":create-site").WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(unitTenantID, "create-site").WillReturnResult(pgxmock.NewResult("DELETE", 0))
			mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(unitTenantID, "create-site").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(test.storedHash, response))
			if test.wantErr == nil {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			result, err := NewPostgresRepository(mock).CreateSite(context.Background(), command)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("expected %v, got %v", test.wantErr, err)
			}
			if test.wantErr == nil && (!result.Replayed || result.Site != stored) {
				t.Fatalf("unexpected replay: %+v", result)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresRepositoryFailsClosedAndClassifiesConflicts(t *testing.T) {
	if _, err := (*PostgresRepository)(nil).ListSites(context.Background(), unitTenantID); err == nil {
		t.Fatal("expected unavailable repository error")
	}
	if _, err := NewPostgresRepository(nil).CreateSite(context.Background(), CreateSiteCommand{}); err == nil {
		t.Fatal("expected unavailable repository error")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	if _, err := NewPostgresRepository(mock).ListSites(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("expected invalid verified tenant error")
	}
	if !errors.Is(classifyCreateError(&pgconn.PgError{Code: "23503"}), ErrTenantNotFound) {
		t.Fatal("foreign-key violation must map to tenant not found")
	}
	if !errors.Is(classifyCreateError(&pgconn.PgError{Code: "23505"}), ErrSiteConflict) {
		t.Fatal("unique violation must map to site conflict")
	}
	if classifyCreateError(errors.New("database unavailable")) == nil {
		t.Fatal("unexpected database error must be preserved")
	}
}
