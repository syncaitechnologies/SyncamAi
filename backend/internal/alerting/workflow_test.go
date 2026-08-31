package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const alertID = "77777777-7777-4777-8777-777777777777"

func acknowledgeCommand() AcknowledgeCommand {
	return AcknowledgeCommand{
		TenantID: tenantID, SiteID: siteID, AlertID: alertID, ActorID: "operator-1",
		RequestID: "88888888-8888-4888-8888-888888888888", IdempotencyKey: "ack-alert-1",
	}
}

func alertRows(status string, ackedAt any, ackedBy string) *pgxmock.Rows {
	created := time.Date(2026, 8, 13, 2, 1, 0, 0, time.UTC)
	return pgxmock.NewRows([]string{"alert_id", "tenant_id", "event_id", "site_id", "camera_id", "zone_id", "event_type", "severity", "status", "confidence", "occurred_at", "created_at", "acked_at", "acked_by"}).
		AddRow(alertID, tenantID, eventID, siteID, cameraID, zoneID, "intrusion", "high", status, .91, created.Add(-time.Minute), created, ackedAt, ackedBy)
}

func TestPostgresRepositoryAcknowledgesAtomically(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := acknowledgeCommand()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantID + ":" + command.IdempotencyKey).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantID, command.IdempotencyKey).WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("SELECT alert_id::text").WithArgs(tenantID, alertID, siteID).WillReturnRows(alertRows("unacknowledged", nil, ""))
	mock.ExpectExec("UPDATE alerts.alerts").WithArgs(tenantID, alertID, siteID, pgxmock.AnyArg(), command.ActorID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO alerts.alert_actions").WithArgs(pgxmock.AnyArg(), tenantID, alertID, command.ActorID, command.RequestID).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), command.ActorID, "alert.acknowledged",
		"alert", alertID, command.RequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("INSERT INTO syncam_realtime.site_sequences").WithArgs(tenantID, siteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(2)))
	mock.ExpectExec("INSERT INTO syncam_realtime.messages").WithArgs(tenantID, siteID, int64(2), "alerts.state", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").WithArgs(tenantID, command.IdempotencyKey, acknowledgeHash(alertID), alertID, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	result, err := NewPostgresRepository(mock).Acknowledge(context.Background(), command)
	if err != nil || result.Replayed || result.Alert.Status != "acknowledged" || result.Alert.AckedAt == nil || result.Alert.AckedBy != command.ActorID {
		t.Fatalf("unexpected acknowledgment: %+v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryReplaysExactAcknowledgment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := acknowledgeCommand()
	acked := time.Date(2026, 8, 13, 2, 2, 0, 0, time.UTC)
	alert := Alert{ID: alertID, TenantID: tenantID, SiteID: siteID, Status: "acknowledged", AckedAt: &acked, AckedBy: command.ActorID}
	response, err := json.Marshal(alert)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantID + ":" + command.IdempotencyKey).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantID, command.IdempotencyKey).WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(acknowledgeHash(alertID), response))
	mock.ExpectCommit()
	result, err := NewPostgresRepository(mock).Acknowledge(context.Background(), command)
	if err != nil || !result.Replayed || result.Alert.ID != alertID {
		t.Fatalf("unexpected replay: %+v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryGetsAlertAndRejectsStateConflict(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT alert_id::text").WithArgs(tenantID, alertID).WillReturnRows(alertRows("unacknowledged", nil, ""))
	mock.ExpectCommit()
	alert, err := NewPostgresRepository(mock).Get(context.Background(), tenantID, alertID)
	if err != nil || alert.ID != alertID || alert.Status != "unacknowledged" {
		t.Fatalf("unexpected alert: %+v %v", alert, err)
	}

	command := acknowledgeCommand()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantID + ":" + command.IdempotencyKey).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantID, command.IdempotencyKey).WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("SELECT alert_id::text").WithArgs(tenantID, alertID, siteID).WillReturnRows(alertRows("acknowledged", time.Now().UTC(), command.ActorID))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Acknowledge(context.Background(), command); !errors.Is(err, ErrAlertStateConflict) {
		t.Fatalf("expected state conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsIdempotencyConflict(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := acknowledgeCommand()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantID + ":" + command.IdempotencyKey).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantID, command.IdempotencyKey).WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow("different", []byte(`{}`)))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Acknowledge(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertWorkflowRejectsInvalidAndConflictingState(t *testing.T) {
	if _, err := (*PostgresRepository)(nil).Get(context.Background(), tenantID, alertID); err == nil {
		t.Fatal("nil repository accepted")
	}
	command := acknowledgeCommand()
	command.AlertID = "bad"
	if _, err := (&MemoryRepository{}).Acknowledge(context.Background(), command); err == nil {
		t.Fatal("invalid acknowledgment accepted")
	}
	repository := &MemoryRepository{Alerts: []Alert{{ID: alertID, TenantID: tenantID, SiteID: siteID, Status: "acknowledged"}}}
	if _, err := repository.Acknowledge(context.Background(), acknowledgeCommand()); !errors.Is(err, ErrAlertStateConflict) {
		t.Fatalf("expected state conflict, got %v", err)
	}
	if _, err := repository.Get(context.Background(), tenantID, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	repository = &MemoryRepository{Alerts: []Alert{{ID: alertID, TenantID: tenantID, SiteID: siteID, Status: "unacknowledged"}}}
	result, err := repository.Acknowledge(context.Background(), acknowledgeCommand())
	if err != nil || result.Alert.Status != "acknowledged" || result.Alert.AckedAt == nil {
		t.Fatalf("memory acknowledgment failed: %+v %v", result, err)
	}
	command = acknowledgeCommand()
	command.ActorID = ""
	if _, err := repository.Acknowledge(context.Background(), command); err == nil {
		t.Fatal("missing actor accepted")
	}
}
