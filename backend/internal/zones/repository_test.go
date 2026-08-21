package zones

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	testTenant = "11111111-1111-4111-8111-111111111111"
	testSite = "33333333-3333-4333-8333-333333333333"
	testZone = "55555555-5555-4555-8555-555555555555"
	testRequest = "66666666-6666-4666-8666-666666666666"
)

var testPolygon = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,0]]]}`)

func TestMemoryRepositoryCreatesReplaysListsAndUpdatesZones(t *testing.T) {
	repository := NewMemoryRepository(nil)
	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-create", SiteID: testSite, Name: " Loading bay ", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	created, err := repository.Create(context.Background(), command)
	if err != nil || created.Replayed || created.Zone.Name != "Loading bay" || created.Zone.ConfigVersion != 1 { t.Fatalf("create: %+v %v", created, err) }
	replayed, err := repository.Create(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.Zone.ID != created.Zone.ID { t.Fatalf("replay: %+v %v", replayed, err) }
	if _, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, IdempotencyKey: command.IdempotencyKey, SiteID: testSite, Name: "Different", Kind: "intrusion", Geometry: testPolygon}); !errors.Is(err, ErrIdempotencyConflict) { t.Fatalf("idempotency conflict: %v", err) }
	listed, err := repository.List(context.Background(), testTenant, testSite)
	if err != nil || len(listed) != 1 || listed[0].ID != created.Zone.ID { t.Fatalf("list: %+v %v", listed, err) }
	name := "Loading bay north"; enabled := false
	updated, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, Name: &name, Enabled: &enabled})
	if err != nil || updated.ConfigVersion != 2 || updated.Enabled || updated.Name != name { t.Fatalf("update: %+v %v", updated, err) }
	if _, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, Name: &name}); !errors.Is(err, ErrVersionConflict) { t.Fatalf("stale update: %v", err) }
	if _, err := repository.Get(context.Background(), testTenant, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) { t.Fatalf("missing get: %v", err) }
}

func zoneRows(zone Zone) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "camera_id", "floor", "name", "kind", "geometry", "enabled", "config_version", "created_at", "updated_at"}).AddRow(zone.ID, zone.TenantID, zone.SiteID, zone.CameraID, zone.Floor, zone.Name, zone.Kind, zone.Geometry, zone.Enabled, zone.ConfigVersion, zone.CreatedAt, zone.UpdatedAt)
}

func expectZoneTransaction(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func expectZoneAudit(mock pgxmock.PgxPoolIface, action string, resourceID any, occurredAt time.Time) {
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":2026-08-21").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(testTenant, "2026-08-21").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), testTenant, "2026-08-21", occurredAt, "user-1", action, "zone", resourceID, testRequest, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestPostgresRepositoryReadsAndWritesZones(t *testing.T) {
	mock, err := pgxmock.NewPool(); if err != nil { t.Fatal(err) }; defer mock.Close()
	createdAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testSite).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	listed, err := NewPostgresRepository(mock).List(context.Background(), testTenant, testSite)
	if err != nil || len(listed) != 1 || listed[0].Name != zone.Name { t.Fatalf("list: %+v %v", listed, err) }
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	got, err := NewPostgresRepository(mock).Get(context.Background(), testTenant, testZone)
	if err != nil || got.ID != testZone { t.Fatalf("get: %+v %v", got, err) }

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-create", SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-create").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-create").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-create").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testSite, testTenant).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO config.zones").WithArgs(pgxmock.AnyArg(), testTenant, testSite, "", "Dock", "Loading bay", "intrusion", testPolygon, true, "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, createdAt))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").WithArgs(testTenant, "zone-create", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectZoneAudit(mock, "zone.created", pgxmock.AnyArg(), createdAt)
	mock.ExpectCommit()
	created, err := NewPostgresRepository(mock).Create(context.Background(), command)
	if err != nil || created.Replayed || created.Zone.ConfigVersion != 1 { t.Fatalf("create: %+v %v", created, err) }

	updatedAt := createdAt.Add(time.Minute); name := "Loading bay north"; enabled := false
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectQuery("UPDATE config.zones").WithArgs(testZone, "Dock", name, testPolygon, enabled, "user-1").WillReturnRows(pgxmock.NewRows([]string{"config_version", "updated_at"}).AddRow(int64(2), updatedAt))
	expectZoneAudit(mock, "zone.updated", testZone, updatedAt)
	mock.ExpectCommit()
	updated, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, ZoneID: testZone, ExpectedVersion: 1, Name: &name, Enabled: &enabled})
	if err != nil || updated.ConfigVersion != 2 || updated.Enabled { t.Fatalf("update: %+v %v", updated, err) }
	if err := mock.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}

func TestPostgresRepositoryFailsClosed(t *testing.T) {
	if _, err := NewPostgresRepository(nil).List(context.Background(), testTenant, ""); err == nil { t.Fatal("expected unavailable repository") }
	mock, err := pgxmock.NewPool(); if err != nil { t.Fatal(err) }; defer mock.Close()
	if _, err := NewPostgresRepository(mock).Get(context.Background(), "bad", testZone); err == nil { t.Fatal("expected invalid tenant") }
}
