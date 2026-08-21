package configdelivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

func revisionRows(revision Revision) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "revision", "payload", "content_hash", "created_by", "created_at"}).AddRow(revision.ID, revision.TenantID, revision.SiteID, revision.Number, revision.Payload, revision.ContentHash, revision.CreatedBy, revision.CreatedAt)
}

func statusRows(status DeviceStatus) *pgxmock.Rows {
	var applied any
	if status.AppliedAt != nil {
		applied = *status.AppliedAt
	}
	return pgxmock.NewRows([]string{"device_id", "tenant_id", "site_id", "revision", "state", "error_message", "reported_at", "applied_at"}).AddRow(status.DeviceID, status.TenantID, status.SiteID, status.Revision, status.State, status.ErrorMessage, status.ReportedAt, applied)
}

func expectTenantTransaction(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func expectConfigurationAudit(mock pgxmock.PgxPoolIface, createdAt time.Time) {
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(tenantID + ":" + createdAt.Format("2006-01-02")).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, createdAt.Format("2006-01-02")).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), tenantID, createdAt.Format("2006-01-02"), createdAt, "user-1", "configuration.published", "configuration_revision", pgxmock.AnyArg(), "55555555-5555-4555-8555-555555555555", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestPostgresRepositoryReadsAndReportsDeviceConfiguration(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	revision := Revision{ID: "44444444-4444-4444-8444-444444444444", TenantID: tenantID, SiteID: siteID, Number: 2, Payload: []byte(`{"zones":[]}`), ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedBy: "user-1", CreatedAt: now}
	status := DeviceStatus{DeviceID: deviceID, TenantID: tenantID, SiteID: siteID, Revision: 2, State: StatusApplied, ReportedAt: now, AppliedAt: &now}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("edge.pull_device_configuration").WithArgs(deviceID, int64(1)).WillReturnRows(revisionRows(revision))
	mock.ExpectCommit()
	pulled, err := NewPostgresRepository(mock).Pull(context.Background(), deviceID, 1)
	if err != nil || pulled.Revision == nil || pulled.Revision.Number != 2 {
		t.Fatalf("pull: %#v %v", pulled, err)
	}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("desired_device_configuration_revision").WithArgs(deviceID).WillReturnRows(pgxmock.NewRows([]string{"revision"}).AddRow(int64(2)))
	mock.ExpectCommit()
	if desired, err := NewPostgresRepository(mock).DesiredRevision(context.Background(), deviceID); err != nil || desired != 2 {
		t.Fatalf("desired: %d %v", desired, err)
	}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("report_device_configuration").WithArgs(deviceID, int64(2), StatusApplied, "").WillReturnRows(statusRows(status))
	mock.ExpectCommit()
	reported, err := NewPostgresRepository(mock).Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 2, State: StatusApplied})
	if err != nil || reported.AppliedAt == nil {
		t.Fatalf("report: %#v %v", reported, err)
	}

	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.device_configuration_statuses").WithArgs(deviceID).WillReturnRows(statusRows(status))
	mock.ExpectCommit()
	loaded, err := NewPostgresRepository(mock).GetStatus(context.Background(), tenantID, deviceID)
	if err != nil || loaded.DeviceID != deviceID {
		t.Fatalf("get status: %#v %v", loaded, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryListsAndFailsClosed(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	revision := Revision{ID: "44444444-4444-4444-8444-444444444444", TenantID: tenantID, SiteID: siteID, Number: 1, Payload: []byte(`{"zones":[]}`), ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: time.Now().UTC()}
	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.configuration_revisions").WithArgs(siteID).WillReturnRows(revisionRows(revision))
	mock.ExpectCommit()
	listed, err := NewPostgresRepository(mock).List(context.Background(), tenantID, siteID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %#v %v", listed, err)
	}
	if _, err := NewPostgresRepository(nil).Pull(context.Background(), deviceID, 0); err == nil {
		t.Fatal("nil pull repository must fail closed")
	}
	if !errors.Is(classifyDeviceError(&pgconn.PgError{Code: "28000"}), ErrDeviceNotFound) {
		t.Fatal("device authorization must be classified")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryPublishesImmutableConfigurationRevision(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(tenantID + ":" + siteID + ":configuration").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(siteID).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT COALESCE").WithArgs(siteID).WillReturnRows(pgxmock.NewRows([]string{"revision"}).AddRow(int64(1)))
	mock.ExpectQuery("INSERT INTO config.configuration_revisions").WithArgs(pgxmock.AnyArg(), tenantID, siteID, int64(2), pgxmock.AnyArg(), pgxmock.AnyArg(), "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at"}).AddRow(createdAt))
	expectConfigurationAudit(mock, createdAt)
	mock.ExpectCommit()
	revision, err := NewPostgresRepository(mock).Publish(context.Background(), PublishCommand{TenantID: tenantID, SiteID: siteID, ActorID: "user-1", RequestID: "55555555-5555-4555-8555-555555555555", Payload: []byte(`{"zones":[]}`)})
	if err != nil || revision.Number != 2 || revision.CreatedAt != createdAt || revision.ContentHash == "" {
		t.Fatalf("publish: %#v %v", revision, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryHandlesEmptyAndUnauthorizedDeviceResults(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("edge.pull_device_configuration").WithArgs(deviceID, int64(2)).WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	result, err := NewPostgresRepository(mock).Pull(context.Background(), deviceID, 2)
	if err != nil || result.Revision != nil {
		t.Fatalf("empty pull: %#v %v", result, err)
	}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("desired_device_configuration_revision").WithArgs(deviceID).WillReturnError(&pgconn.PgError{Code: "28000"})
	if _, err := NewPostgresRepository(mock).DesiredRevision(context.Background(), deviceID); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("unauthorized desired revision: %v", err)
	}

	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.device_configuration_statuses").WithArgs(deviceID).WillReturnError(pgx.ErrNoRows)
	if _, err := NewPostgresRepository(mock).GetStatus(context.Background(), tenantID, deviceID); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("missing status: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryListsAllSitesAndRejectsUnavailableWrites(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	revision := Revision{ID: "44444444-4444-4444-8444-444444444444", TenantID: tenantID, SiteID: siteID, Number: 1, Payload: []byte(`{"zones":[]}`), ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: time.Now().UTC()}
	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.configuration_revisions").WillReturnRows(revisionRows(revision))
	mock.ExpectCommit()
	listed, err := NewPostgresRepository(mock).List(context.Background(), tenantID, "")
	if err != nil || len(listed) != 1 {
		t.Fatalf("all-site list: %#v %v", listed, err)
	}
	if _, err := NewPostgresRepository(nil).DesiredRevision(context.Background(), deviceID); err == nil {
		t.Fatal("nil desired-revision repository must fail closed")
	}
	if _, err := NewPostgresRepository(nil).Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 1, State: StatusApplied}); err == nil {
		t.Fatal("nil report repository must fail closed")
	}
	if _, err := NewPostgresRepository(nil).GetStatus(context.Background(), tenantID, deviceID); err == nil {
		t.Fatal("nil status repository must fail closed")
	}
	if _, err := NewPostgresRepository(mock).List(context.Background(), "not-a-tenant-id", ""); err == nil {
		t.Fatal("invalid tenant identifier must fail closed")
	}
	if errors.Is(classifyDeviceError(errors.New("database failed")), ErrDeviceNotFound) {
		t.Fatal("generic database errors must not masquerade as a missing device")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
