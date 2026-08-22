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
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000002_event_outbox.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS events").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000002_event_outbox.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000003_alert_projection.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("ALTER TABLE messaging.outbox_messages").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000003_alert_projection.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000004_alert_workflow_realtime.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("ALTER TABLE alerts.alerts").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000004_alert_workflow_realtime.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000005_camera_registry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("DO \\$\\$").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("DO", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000005_camera_registry.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000006_device_claim_activation.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS config.edge_devices").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000006_device_claim_activation.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000007_device_heartbeat.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS edge").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000007_device_heartbeat.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000008_device_health_telemetry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("ALTER TABLE config.edge_devices").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000008_device_health_telemetry.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000009_zone_registry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("DO \\$\\$").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("DO", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000009_zone_registry.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000010_configuration_delivery.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS config.configuration_revisions").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000010_configuration_delivery.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000011_zone_loiter_duration.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("ALTER TABLE config.zones").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000011_zone_loiter_duration.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000012_zone_subject_classes.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("ALTER TABLE config.zones").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000012_zone_subject_classes.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000013_privacy_mask_approval_ledger.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS config.privacy_mask_requests").WithArgs(pgx.QueryExecModeSimpleProtocol).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO platform.schema_migrations").WithArgs("000013_privacy_mask_approval_ledger.sql").WillReturnResult(pgxmock.NewResult("INSERT", 1))
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
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000002_event_outbox.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000003_alert_projection.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000004_alert_workflow_realtime.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000005_camera_registry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000006_device_claim_activation.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000007_device_heartbeat.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000008_device_health_telemetry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000009_zone_registry.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000010_configuration_delivery.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000011_zone_loiter_duration.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000012_zone_subject_classes.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("000013_privacy_mask_approval_ledger.sql").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
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
