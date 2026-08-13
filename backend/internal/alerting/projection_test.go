package alerting

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
)

const (
	tenantID  = "11111111-1111-4111-8111-111111111111"
	eventID   = "22222222-2222-4222-8222-222222222222"
	siteID    = "33333333-3333-4333-8333-333333333333"
	cameraID  = "44444444-4444-4444-8444-444444444444"
	zoneID    = "55555555-5555-4555-8555-555555555555"
	messageID = "66666666-6666-4666-8666-666666666666"
)

func projectionMessage(t *testing.T) outbox.Message {
	t.Helper()
	event := eventing.DetectionEvent{EventID: eventID, TenantID: tenantID, DedupeKey: "camera:1", OccurredAt: time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC), SiteID: siteID, CameraID: cameraID, ZoneID: zoneID, EventType: "intrusion", ModelVersion: "model-1", Confidence: .91, EvidenceRefs: []string{}, RequiresHumanReview: true, ReviewState: "pending"}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Message{MessageID: messageID, TenantID: tenantID, Topic: eventing.OutboxTopic, Payload: payload, OccurredAt: event.OccurredAt}
}

func TestProjectorCreatesOneAlertAndReceipt(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	message := projectionMessage(t)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("INSERT INTO messaging.consumer_receipts").WithArgs(tenantID, ConsumerName, messageID).WillReturnRows(pgxmock.NewRows([]string{"message_id"}).AddRow(messageID))
	mock.ExpectExec("INSERT INTO alerts.alerts").WithArgs(pgxmock.AnyArg(), tenantID, eventID, siteID, cameraID, zoneID, "intrusion", "high", .91, 400, message.OccurredAt, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), "system:alert-projector", "alert.created",
		"alert", pgxmock.AnyArg(), messageID, pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("INSERT INTO realtime.site_sequences").WithArgs(tenantID, siteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(1)))
	mock.ExpectExec("INSERT INTO realtime.messages").WithArgs(tenantID, siteID, int64(1), "alerts.created", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	if err := NewProjector(mock).Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectorReplaysConsumerReceipt(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	message := projectionMessage(t)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("INSERT INTO messaging.consumer_receipts").WithArgs(tenantID, ConsumerName, messageID).WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	if err := NewProjector(mock).Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertRepositoryListsTenantQueue(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	created := time.Date(2026, 8, 13, 2, 1, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT alert_id::text").WithArgs(nil).WillReturnRows(pgxmock.NewRows([]string{"alert_id", "tenant_id", "event_id", "site_id", "camera_id", "zone_id", "event_type", "severity", "status", "confidence", "occurred_at", "created_at", "acked_at", "acked_by"}).AddRow(messageID, tenantID, eventID, siteID, cameraID, zoneID, "intrusion", "high", "unacknowledged", .91, created.Add(-time.Minute), created, nil, ""))
	mock.ExpectCommit()
	queue, err := NewPostgresRepository(mock).List(context.Background(), tenantID)
	if err != nil || len(queue) != 1 || queue[0].Severity != "high" {
		t.Fatalf("unexpected queue: %+v %v", queue, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertRepositoryListsOneSiteAtTheDatabaseBoundary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT alert_id::text").WithArgs(siteID).WillReturnRows(pgxmock.NewRows([]string{"alert_id", "tenant_id", "event_id", "site_id", "camera_id", "zone_id", "event_type", "severity", "status", "confidence", "occurred_at", "created_at", "acked_at", "acked_by"}))
	mock.ExpectCommit()
	queue, err := NewPostgresRepository(mock).ListSite(context.Background(), tenantID, siteID)
	if err != nil || len(queue) != 0 {
		t.Fatalf("unexpected site queue: %+v %v", queue, err)
	}
	if _, err := NewPostgresRepository(mock).ListSite(context.Background(), tenantID, "bad"); err == nil {
		t.Fatal("invalid site accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionRejectsInvalidMessagesAndClassifies(t *testing.T) {
	if err := NewProjector(nil).Publish(context.Background(), outbox.Message{}); err == nil {
		t.Fatal("missing pool must fail")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	projector := NewProjector(mock)
	message := projectionMessage(t)
	message.Topic = "unknown"
	if err := projector.Publish(context.Background(), message); err == nil {
		t.Fatal("unknown topic accepted")
	}
	message = projectionMessage(t)
	message.Payload = []byte("{")
	if err := projector.Publish(context.Background(), message); err == nil {
		t.Fatal("invalid payload accepted")
	}
	message = projectionMessage(t)
	message.TenantID = "77777777-7777-4777-8777-777777777777"
	if err := projector.Publish(context.Background(), message); err == nil {
		t.Fatal("tenant mismatch accepted")
	}
	for eventType, expected := range map[string]string{"weapon_review": "critical", "intrusion": "high", "loitering": "medium", "attendance_review": "info", "unknown": "low"} {
		severity, _ := classify(eventType)
		if severity != expected {
			t.Fatalf("%s: got %s", eventType, severity)
		}
	}
	if _, err := (*PostgresRepository)(nil).List(context.Background(), tenantID); err == nil {
		t.Fatal("nil repository must fail")
	}
	visible, err := (&MemoryRepository{Alerts: []Alert{{TenantID: tenantID}, {TenantID: "other"}}}).List(context.Background(), tenantID)
	if err != nil || len(visible) != 1 {
		t.Fatalf("memory isolation failed: %+v %v", visible, err)
	}
}
