package privacymasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

func TestPostgresReleaseTransportPullsAndReportsOnlyDeviceScopedMetadata(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	created := time.Date(2026, 8, 24, 5, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("edge.pull_privacy_mask_release").WithArgs(releaseDeviceID, int64(0)).WillReturnRows(pgxmock.NewRows([]string{"release_id", "tenant_id", "site_id", "camera_id", "request_id", "device_id", "version", "candidate", "pipeline", "hil_evidence", "candidate_hash", "evidence_hash", "created_at"}).AddRow("77777777-7777-4777-8777-777777777777", tenantID, siteID, cameraID, "55555555-5555-4555-8555-555555555555", releaseDeviceID, int64(1), []byte(`{"candidate":"metadata"}`), []byte(`{"stages":["decode","mask","encode"]}`), []byte(`{"evidence":"opaque"}`), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", created))
	mock.ExpectCommit()
	pulled, err := NewPostgresReleaseTransportRepository(mock).Pull(context.Background(), releaseDeviceID, 0)
	if err != nil || pulled.Manifest == nil || pulled.Manifest.DeviceID != releaseDeviceID {
		t.Fatalf("pull release: %#v %v", pulled, err)
	}
	reported := created.Add(time.Minute)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("edge.report_privacy_mask_release").WithArgs(releaseDeviceID, "77777777-7777-4777-8777-777777777777", int64(1), "accepted", "").WillReturnRows(pgxmock.NewRows([]string{"tenant_id", "device_id", "release_id", "version", "state", "error_code", "reported_at", "accepted_at"}).AddRow(tenantID, releaseDeviceID, "77777777-7777-4777-8777-777777777777", int64(1), "accepted", "", reported, reported))
	mock.ExpectCommit()
	status, err := NewPostgresReleaseTransportRepository(mock).Report(context.Background(), ReportReleaseCommand{DeviceID: releaseDeviceID, ReleaseID: "77777777-7777-4777-8777-777777777777", Version: 1, State: "accepted"})
	if err != nil || status.AcceptedAt == nil {
		t.Fatalf("report release: %#v %v", status, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyReleaseDeviceErrorHidesAuthorizationAndPreservesAvailabilityFailures(t *testing.T) {
	if !errors.Is(classifyReleaseDeviceError(&pgconn.PgError{Code: "28000"}), ErrReleaseDeviceNotFound) {
		t.Fatal("authorization errors must be hidden")
	}
	if !errors.Is(classifyReleaseDeviceError(&pgconn.PgError{Code: "22023"}), ErrReleaseDeviceNotFound) {
		t.Fatal("invalid device release must be hidden")
	}
	if errors.Is(classifyReleaseDeviceError(errors.New("storage unavailable")), ErrReleaseDeviceNotFound) {
		t.Fatal("storage failures must remain distinguishable")
	}
}

func TestPostgresReleaseTransportFailsClosedAndReturnsEmptyPulls(t *testing.T) {
	if _, err := NewPostgresReleaseTransportRepository(nil).Pull(context.Background(), releaseDeviceID, 0); err != ErrReleaseDeviceNotFound {
		t.Fatalf("nil pull: %v", err)
	}
	if _, err := NewPostgresReleaseTransportRepository(nil).Pull(context.Background(), releaseDeviceID, -1); err != ErrReleaseDeviceNotFound {
		t.Fatalf("negative pull: %v", err)
	}
	if _, err := NewPostgresReleaseTransportRepository(nil).Report(context.Background(), ReportReleaseCommand{}); err != ErrReleaseDeviceNotFound {
		t.Fatalf("nil report: %v", err)
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("edge.pull_privacy_mask_release").WithArgs(releaseDeviceID, int64(1)).WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	result, err := NewPostgresReleaseTransportRepository(mock).Pull(context.Background(), releaseDeviceID, 1)
	if err != nil || result.Manifest != nil {
		t.Fatalf("empty pull: %#v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
