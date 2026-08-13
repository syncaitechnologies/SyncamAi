package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

func edgeDeviceRows(device EdgeDevice) *pgxmock.Rows {
	var lastHeartbeat, activatedAt any
	if device.LastHeartbeat != nil {
		lastHeartbeat = *device.LastHeartbeat
	}
	if device.ActivatedAt != nil {
		activatedAt = *device.ActivatedAt
	}
	return pgxmock.NewRows([]string{
		"id", "tenant_id", "site_id", "serial_number", "hardware_tier", "model", "status", "cert_status",
		"firmware_version", "store_forward_depth", "uptime_seconds", "last_heartbeat", "activated_at", "created_at", "updated_at",
	}).AddRow(
		device.ID, device.TenantID, device.SiteID, device.SerialNumber, device.HardwareTier, device.Model, device.Status, device.CertificateStatus,
		device.FirmwareVersion, device.StoreForwardDepth, device.UptimeSeconds, lastHeartbeat, activatedAt, device.CreatedAt, device.UpdatedAt,
	)
}

func TestPostgresStatusRepositoryListsAndDerivesOffline(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-DeviceOfflineAfter - time.Second)
	device := EdgeDevice{
		ID: edgeDeviceA, TenantID: tenantA, SiteID: siteA, SerialNumber: "EDGE-01", HardwareTier: "m",
		Status: "active", CertificateStatus: "active", FirmwareVersion: "1.2.3", ActivatedAt: &stale, CreatedAt: stale, UpdatedAt: stale,
	}
	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(siteA).WillReturnRows(edgeDeviceRows(device))
	mock.ExpectCommit()
	listed, err := NewPostgresStatusRepository(mock).ListDevices(context.Background(), tenantA, siteA, now)
	if err != nil || len(listed) != 1 || listed[0].Status != "offline" {
		t.Fatalf("unexpected device list: %+v %v", listed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStatusRepositoryRecordsHeartbeatThroughGuardedFunction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	heartbeatID := "66666666-6666-4666-8666-666666666666"
	command := HeartbeatCommand{DeviceID: edgeDeviceA, HeartbeatID: heartbeatID, ReportedAt: now.Add(-time.Second), UptimeSeconds: 42, StoreForwardDepth: 7, FirmwareVersion: " 1.2.3 "}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("FROM edge.record_device_heartbeat").WithArgs(edgeDeviceA, heartbeatID, pgxmock.AnyArg(), command.ReportedAt, int64(42), int64(7), "1.2.3").WillReturnRows(pgxmock.NewRows([]string{
		"device_id", "tenant_id", "site_id", "serial_number", "hardware_tier", "model", "status", "certificate_status",
		"firmware_version", "store_forward_depth", "uptime_seconds", "last_heartbeat", "activated_at", "created_at", "updated_at", "observed_at", "replayed",
	}).AddRow(edgeDeviceA, tenantA, siteA, "EDGE-01", "m", "Jetson", "active", "active", "1.2.3", int64(7), int64(42), now, now.Add(-time.Hour), now.Add(-time.Hour), now, now, false))
	mock.ExpectCommit()
	result, err := NewPostgresStatusRepository(mock).RecordHeartbeat(context.Background(), command)
	if err != nil || result.Replayed || result.Device.TenantID != tenantA || result.Device.LastHeartbeat == nil || result.Device.FirmwareVersion != "1.2.3" {
		t.Fatalf("unexpected heartbeat result: %+v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStatusRepositoryFailsClosedAndClassifiesHeartbeatErrors(t *testing.T) {
	if _, err := (*PostgresStatusRepository)(nil).ListDevices(context.Background(), tenantA, "", time.Now()); err == nil {
		t.Fatal("nil list repository must fail closed")
	}
	if _, err := NewPostgresStatusRepository(nil).RecordHeartbeat(context.Background(), HeartbeatCommand{}); err == nil {
		t.Fatal("nil heartbeat repository must fail closed")
	}
	if !errors.Is(classifyHeartbeatError(&pgconn.PgError{Code: "28000"}), ErrDeviceUnauthorized) {
		t.Fatal("authorization SQLSTATE must be classified")
	}
	if !errors.Is(classifyHeartbeatError(&pgconn.PgError{Code: "23505"}), ErrHeartbeatConflict) {
		t.Fatal("unique SQLSTATE must be classified")
	}
	if classifyHeartbeatError(errors.New("database unavailable")) == nil {
		t.Fatal("unexpected database error must be wrapped")
	}
}
