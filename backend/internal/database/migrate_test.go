package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestApplyMigrationsAppliesOnceAndThenSkips(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS platform").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS platform.schema_migrations").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000001_identity_tenancy_audit.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS pgcrypto").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000001_identity_tenancy_audit.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	if err := ApplyMigrations(context.Background(), mock); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS platform").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS platform.schema_migrations").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000001_identity_tenancy_audit.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	if err := ApplyMigrations(context.Background(), mock); err != nil {
		t.Fatalf("second migration pass must be idempotent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyMigrationsFailsClosed(t *testing.T) {
	if err := ApplyMigrations(context.Background(), nil); err == nil {
		t.Fatal("expected nil pool error")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	want := errors.New("database unavailable")
	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS platform").WillReturnError(want)
	if err := ApplyMigrations(context.Background(), mock); !errors.Is(err, want) {
		t.Fatalf("expected wrapped database error, got %v", err)
	}
}
