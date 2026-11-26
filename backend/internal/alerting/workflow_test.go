package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

const (
	secondAlertID = "77777777-7777-4777-8777-777777777778"
	otherSiteID   = "99999999-9999-4999-8999-999999999999"
	doneAlertID   = "77777777-7777-4777-8777-777777777779"
	hiddenAlertID = "77777777-7777-4777-8777-77777777777a"
)

func dismissCommand(ids ...string) DismissCommand {
	if len(ids) == 0 {
		ids = []string{alertID}
	}
	return DismissCommand{
		TenantID: tenantID, AlertIDs: ids, ActorID: "operator-1",
		RequestID: "88888888-8888-4888-8888-888888888888", Reason: "False positive",
		CanAccessSite: func(site string) bool { return site == siteID },
	}
}

func batchRows(alerts ...[3]string) *pgxmock.Rows {
	created := time.Date(2026, 8, 13, 2, 1, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"alert_id", "tenant_id", "event_id", "site_id", "camera_id", "zone_id", "event_type", "severity", "status", "confidence", "occurred_at", "created_at", "acked_at", "acked_by"})
	for _, alert := range alerts {
		rows.AddRow(alert[0], tenantID, eventID, alert[1], cameraID, zoneID, "intrusion", "high", alert[2], .91, created.Add(-time.Minute), created, nil, "")
	}
	return rows
}

func TestPostgresRepositoryDismissesABatchInOneTransaction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := dismissCommand(alertID, secondAlertID, doneAlertID, hiddenAlertID, "not-a-uuid", alertID)
	changed := []string{alertID, secondAlertID}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`alert_id = ANY\(\$2::uuid\[\]\)[\s\S]*FOR UPDATE`).
		WithArgs(tenantID, []string{alertID, secondAlertID, doneAlertID, hiddenAlertID}).
		WillReturnRows(batchRows(
			[3]string{alertID, siteID, "unacknowledged"}, [3]string{secondAlertID, siteID, "acknowledged"},
			[3]string{doneAlertID, siteID, "dismissed"}, [3]string{hiddenAlertID, otherSiteID, "unacknowledged"}))
	mock.ExpectExec("UPDATE alerts.alerts SET status = 'dismissed'").WithArgs(tenantID, changed, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	// Every action row gets its own request_id: (tenant_id, request_id) is unique.
	mock.ExpectExec(`INSERT INTO alerts.alert_actions[\s\S]*'user', \$2, gen_random_uuid\(\), \$3::jsonb[\s\S]*unnest\(\$4::uuid\[\]\)`).
		WithArgs(tenantID, command.ActorID, []byte(`{"reason":"False positive","request_id":"88888888-8888-4888-8888-888888888888"}`), changed).
		WillReturnResult(pgxmock.NewResult("INSERT", 2))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), command.ActorID, "alert.dismissed",
		"alert_batch", command.RequestID, command.RequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("INSERT INTO syncam_realtime.site_sequences").WithArgs(tenantID, siteID, int64(2)).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(12)))
	mock.ExpectExec("INSERT INTO syncam_realtime.messages").WithArgs(tenantID, siteID, int64(10), "alerts.state", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 2))
	mock.ExpectCommit()
	mock.ExpectRollback()
	dismissed, err := NewPostgresRepository(mock).Dismiss(context.Background(), command)
	if err != nil || strings.Join(dismissed, ",") != alertID+","+secondAlertID+","+doneAlertID {
		t.Fatalf("accessible alerts (including the already dismissed one) are reported: %v %v", dismissed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryDismissesOneAlertWithAnAlertAuditRecord(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	command := dismissCommand()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FOR UPDATE").WithArgs(tenantID, []string{alertID}).WillReturnRows(batchRows([3]string{alertID, siteID, "unacknowledged"}))
	mock.ExpectExec("UPDATE alerts.alerts").WithArgs(tenantID, []string{alertID}, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO alerts.alert_actions").WithArgs(tenantID, command.ActorID, pgxmock.AnyArg(), []string{alertID}).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), command.ActorID, "alert.dismissed",
		"alert", alertID, command.RequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("INSERT INTO syncam_realtime.site_sequences").WithArgs(tenantID, siteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(3)))
	mock.ExpectExec("INSERT INTO syncam_realtime.messages").WithArgs(tenantID, siteID, int64(3), "alerts.state", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	dismissed, err := NewPostgresRepository(mock).Dismiss(context.Background(), command)
	if err != nil || len(dismissed) != 1 || dismissed[0] != alertID {
		t.Fatalf("unexpected dismissal: %v %v", dismissed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryDismissIsIdempotentAndSurfacesFailures(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FOR UPDATE").WithArgs(tenantID, []string{alertID}).WillReturnRows(batchRows([3]string{alertID, siteID, "dismissed"}))
	mock.ExpectCommit()
	dismissed, err := NewPostgresRepository(mock).Dismiss(context.Background(), dismissCommand())
	if err != nil || len(dismissed) != 1 {
		t.Fatalf("an already dismissed alert is reported without writes: %v %v", dismissed, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FOR UPDATE").WithArgs(tenantID, []string{alertID}).WillReturnRows(batchRows([3]string{alertID, siteID, "unacknowledged"}))
	mock.ExpectExec("UPDATE alerts.alerts").WithArgs(tenantID, []string{alertID}, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO alerts.alert_actions").WithArgs(tenantID, "operator-1", pgxmock.AnyArg(), []string{alertID}).WillReturnError(errors.New("unique violation"))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Dismiss(context.Background(), dismissCommand()); err == nil {
		t.Fatal("an action insert failure rolls back and is reported")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FOR UPDATE").WithArgs(tenantID, []string{alertID}).WillReturnError(errors.New("db down"))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Dismiss(context.Background(), dismissCommand()); err == nil {
		t.Fatal("a lock failure is reported")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if _, err := (*PostgresRepository)(nil).Dismiss(context.Background(), dismissCommand()); err == nil {
		t.Fatal("nil repository accepted")
	}
}

func TestDismissValidatesAndHidesAlertsFromQueue(t *testing.T) {
	tooMany := make([]string, MaxDismissBatch+1)
	for index := range tooMany {
		tooMany[index] = alertID
	}
	invalid := []func(*DismissCommand){
		func(c *DismissCommand) { c.RequestID = "bad" },
		func(c *DismissCommand) { c.ActorID = "" },
		func(c *DismissCommand) { c.Reason = "because" },
		func(c *DismissCommand) { c.CanAccessSite = nil },
		func(c *DismissCommand) { c.AlertIDs = nil },
		func(c *DismissCommand) { c.AlertIDs = tooMany },
	}
	for _, mutate := range invalid {
		command := dismissCommand()
		mutate(&command)
		if _, err := (&MemoryRepository{}).Dismiss(context.Background(), command); err == nil {
			t.Fatalf("invalid dismissal accepted: %+v", command)
		}
		if _, err := NewPostgresRepository(nil).Dismiss(context.Background(), command); err == nil {
			t.Fatalf("invalid dismissal accepted by postgres: %+v", command)
		}
	}
	repository := &MemoryRepository{Alerts: []Alert{
		{ID: alertID, TenantID: tenantID, SiteID: siteID, Status: "unacknowledged"},
		{ID: hiddenAlertID, TenantID: tenantID, SiteID: otherSiteID, Status: "unacknowledged"},
	}}
	dismissed, err := repository.Dismiss(context.Background(), dismissCommand(alertID, hiddenAlertID))
	if err != nil || len(dismissed) != 1 || dismissed[0] != alertID {
		t.Fatalf("only the accessible alert is dismissed: %v %v", dismissed, err)
	}
	queue, _ := repository.List(context.Background(), tenantID)
	siteQueue, _ := repository.ListSite(context.Background(), tenantID, siteID)
	if len(queue) != 1 || queue[0].ID != hiddenAlertID || len(siteQueue) != 0 {
		t.Fatalf("dismissed alert still queued: %+v %+v", queue, siteQueue)
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
