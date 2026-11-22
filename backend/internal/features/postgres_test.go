package features

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func expectTenant(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantA).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func TestPostgresStoreLoadsPlanOverridesAndSwitches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	updated := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	minutes := int32(10)
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM identity.tenants").WithArgs(tenantA).WillReturnRows(pgxmock.NewRows([]string{"plan", "live_view_minutes"}).AddRow("trial", &minutes))
	mock.ExpectQuery("FROM identity.tenant_features").WithArgs(tenantA).WillReturnRows(pgxmock.NewRows([]string{"feature", "included"}).AddRow("weapon", true))
	mock.ExpectQuery("FROM config.feature_settings").WithArgs(tenantA).WillReturnRows(pgxmock.NewRows([]string{"feature", "enabled", "camera_ids", "updated_at", "updated_by"}).
		AddRow("weapon", true, []string{cameraA}, updated, "user-1").
		AddRow("zone_alerts", false, []string(nil), updated, "user-1"))
	mock.ExpectCommit()
	mock.ExpectRollback()

	snapshot, err := NewPostgresStore(mock).Load(context.Background(), tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Plan != "trial" || snapshot.LiveViewMinutes == nil || *snapshot.LiveViewMinutes != 10 || !snapshot.Included[Weapon] {
		t.Fatalf("plan: %+v", snapshot)
	}
	effective := Resolve(snapshot)
	if !effective[Weapon].Allows(cameraA) || effective[Weapon].Allows(cameraB) || effective[ZoneAlerts].On() {
		t.Fatalf("effective: %+v", effective)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStoreSavesTheSwitchWithAnAuditRecord(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	setting := Setting{Feature: Weapon, Enabled: true, Cameras: []string{cameraA}, UpdatedAt: at, UpdatedBy: "user-1"}
	expectTenant(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO config.feature_settings").WithArgs(tenantA, "weapon", true, []string{cameraA}, at, "user-1").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantA, "2026-11-22").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantA, "2026-11-22", at, "user-1", "feature.settings.changed", "feature", "weapon",
		"0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	err = NewPostgresStore(mock).Save(context.Background(), tenantA, setting, Change{ActorID: "user-1", RequestID: "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", HadBefore: true, Before: Setting{Feature: Weapon}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStoreReadsCapabilitiesActivityAndCameras(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	since := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM platform.site_keys").WithArgs(tenantA).WillReturnRows(pgxmock.NewRows([]string{"capabilities"}).
		AddRow([]byte(`["zone_alerts","weapon"]`)).AddRow([]byte(`not json`)))
	mock.ExpectCommit()
	mock.ExpectRollback()
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM alerts.alerts").WithArgs(tenantA, since).WillReturnRows(pgxmock.NewRows([]string{"event_type", "count"}).AddRow("intrusion", int64(4)))
	mock.ExpectQuery("FROM attendance.records").WithArgs(tenantA, since).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(9)))
	mock.ExpectCommit()
	mock.ExpectRollback()
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.cameras").WithArgs(tenantA, cameraA).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	mock.ExpectRollback()

	store := NewPostgresStore(mock)
	ctx := context.Background()
	capabilities, reported, err := store.Capabilities(ctx, tenantA)
	if err != nil || !reported || !capabilities[Weapon] || capabilities[Attendance] {
		t.Fatalf("capabilities: %v %v %v", capabilities, reported, err)
	}
	alerts, sightings, err := store.Activity(ctx, tenantA, since)
	if err != nil || alerts["intrusion"] != 4 || sightings != 9 {
		t.Fatalf("activity: %v %d %v", alerts, sightings, err)
	}
	exists, err := store.CameraExists(ctx, tenantA, cameraA)
	if err != nil || !exists {
		t.Fatalf("camera: %v %v", exists, err)
	}
	if exists, err := store.CameraExists(ctx, tenantA, "not-a-uuid"); err != nil || exists {
		t.Fatalf("a malformed camera id never exists: %v %v", exists, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStoreFailsClosedOnBadInputAndErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := NewPostgresStore(nil).Load(ctx, tenantA); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a missing pool is unavailable: %v", err)
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)
	if _, err := store.Load(ctx, "not-a-uuid"); err == nil {
		t.Fatal("a malformed tenant is refused before the database")
	}
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM identity.tenants").WithArgs(tenantA).WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()
	if _, err := store.Load(ctx, tenantA); err == nil {
		t.Fatal("a read error is returned")
	}
	expectTenant(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO config.feature_settings").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(errors.New("denied"))
	mock.ExpectRollback()
	if err := store.Save(ctx, tenantA, Setting{Feature: Weapon}, Change{}); err == nil {
		t.Fatal("a write error is returned")
	}
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM platform.site_keys").WithArgs(tenantA).WillReturnError(errors.New("denied"))
	mock.ExpectRollback()
	if _, _, err := store.Capabilities(ctx, tenantA); err == nil {
		t.Fatal("a capability read error is returned")
	}
	expectTenant(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM alerts.alerts").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(errors.New("denied"))
	mock.ExpectRollback()
	if _, _, err := store.Activity(ctx, tenantA, time.Now()); err == nil {
		t.Fatal("an activity read error is returned")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
