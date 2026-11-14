package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
)

type recordingNotifier struct {
	alerts []Alert
	refs   [][]string
}

func (n *recordingNotifier) AlertCreated(_ context.Context, alert Alert, refs []string) {
	n.alerts = append(n.alerts, alert)
	n.refs = append(n.refs, refs)
}

func expectNewAlertProjection(mock pgxmock.PgxPoolIface, occurredAt time.Time) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("INSERT INTO messaging.consumer_receipts").WithArgs(tenantID, ConsumerName, messageID).WillReturnRows(pgxmock.NewRows([]string{"message_id"}).AddRow(messageID))
	mock.ExpectExec("INSERT INTO alerts.alerts").WithArgs(pgxmock.AnyArg(), tenantID, eventID, siteID, cameraID, zoneID, "weapon_review", "critical", .8, 500, occurredAt, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantID, pgxmock.AnyArg(), pgxmock.AnyArg(), "system:alert-projector", "alert.created",
		"alert", pgxmock.AnyArg(), messageID, pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("INSERT INTO syncam_realtime.site_sequences").WithArgs(tenantID, siteID).WillReturnRows(pgxmock.NewRows([]string{"last_sequence"}).AddRow(int64(1)))
	mock.ExpectExec("INSERT INTO syncam_realtime.messages").WithArgs(tenantID, siteID, int64(1), "alerts.created", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
}

func TestProjectorNotifiesOnlyNewAlertsWithEvidence(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	occurredAt := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	event := eventing.DetectionEvent{EventID: eventID, TenantID: tenantID, DedupeKey: "weapon:1", OccurredAt: occurredAt, SiteID: siteID, CameraID: cameraID, ZoneID: zoneID, EventType: "weapon_review", ModelVersion: "m", Confidence: .8, EvidenceRefs: []string{"snapshot:a"}, RequiresHumanReview: true, ReviewState: "pending"}
	payload, _ := json.Marshal(event)
	message := outbox.Message{MessageID: messageID, TenantID: tenantID, Topic: eventing.OutboxTopic, Payload: payload, OccurredAt: occurredAt}
	notifier := &recordingNotifier{}
	projector := NewProjectorWithNotifier(mock, notifier)

	expectNewAlertProjection(mock, occurredAt)
	if err := projector.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	// A replayed message must not notify a second time.
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("INSERT INTO messaging.consumer_receipts").WithArgs(tenantID, ConsumerName, messageID).WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	if err := projector.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(notifier.alerts) != 1 || notifier.alerts[0].Severity != "critical" || len(notifier.refs[0]) != 1 || notifier.refs[0][0] != "snapshot:a" {
		t.Fatalf("unexpected notifications: %+v %+v", notifier.alerts, notifier.refs)
	}
}

func TestEvidenceRefsReadsSourceEvent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	repository := NewPostgresRepository(mock)
	alertID := "77777777-7777-4777-8777-777777777777"
	if _, err := repository.EvidenceRefs(context.Background(), tenantID, "bad"); !errors.Is(err, ErrEvidenceNotFound) {
		t.Fatalf("bad id: %v", err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM alerts.alerts AS a").WithArgs(tenantID, alertID).WillReturnRows(pgxmock.NewRows([]string{"site_id", "evidence_refs"}).AddRow(siteID, []byte(`["snapshot:x"]`)))
	mock.ExpectCommit()
	refs, err := repository.EvidenceRefs(context.Background(), tenantID, alertID)
	if err != nil || len(refs) != 1 || refs[0] != "snapshot:x" {
		t.Fatalf("refs: %v %v", refs, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM alerts.alerts AS a").WithArgs(tenantID, alertID).WillReturnRows(pgxmock.NewRows([]string{"site_id", "evidence_refs"}))
	mock.ExpectRollback()
	if _, err := repository.EvidenceRefs(context.Background(), tenantID, alertID); !errors.Is(err, ErrEvidenceNotFound) {
		t.Fatalf("missing alert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
