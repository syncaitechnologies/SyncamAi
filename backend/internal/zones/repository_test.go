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
	testTenant  = "11111111-1111-4111-8111-111111111111"
	testSite    = "33333333-3333-4333-8333-333333333333"
	testZone    = "55555555-5555-4555-8555-555555555555"
	testRequest = "66666666-6666-4666-8666-666666666666"
)

var testPolygon = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,0]]]}`)

func TestMemoryRepositoryCreatesReplaysListsAndUpdatesZones(t *testing.T) {
	repository := NewMemoryRepository(nil)
	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-create", SiteID: testSite, Name: " Loading bay ", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	created, err := repository.Create(context.Background(), command)
	if err != nil || created.Replayed || created.Zone.Name != "Loading bay" || created.Zone.ConfigVersion != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}
	replayed, err := repository.Create(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.Zone.ID != created.Zone.ID {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	if _, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, IdempotencyKey: command.IdempotencyKey, SiteID: testSite, Name: "Different", Kind: "intrusion", Geometry: testPolygon}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict: %v", err)
	}
	listed, err := repository.List(context.Background(), testTenant, testSite)
	if err != nil || len(listed) != 1 || listed[0].ID != created.Zone.ID {
		t.Fatalf("list: %+v %v", listed, err)
	}
	name := "Loading bay north"
	enabled := false
	updated, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, Name: &name, Enabled: &enabled})
	if err != nil || updated.ConfigVersion != 2 || updated.Enabled || updated.Name != name {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, Name: &name}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update: %v", err)
	}
	if _, err := repository.Get(context.Background(), testTenant, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get: %v", err)
	}
}

func zoneRows(zone Zone) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "camera_id", "floor", "name", "kind", "geometry", "loiter_seconds", "subject_classes", "enabled", "config_version", "created_at", "updated_at"}).AddRow(zone.ID, zone.TenantID, zone.SiteID, zone.CameraID, zone.Floor, zone.Name, zone.Kind, zone.Geometry, zone.LoiterSeconds, zone.SubjectClasses, zone.Enabled, zone.ConfigVersion, zone.CreatedAt, zone.UpdatedAt)
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
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testSite).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	listed, err := NewPostgresRepository(mock).List(context.Background(), testTenant, testSite)
	if err != nil || len(listed) != 1 || listed[0].Name != zone.Name {
		t.Fatalf("list: %+v %v", listed, err)
	}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	got, err := NewPostgresRepository(mock).Get(context.Background(), testTenant, testZone)
	if err != nil || got.ID != testZone {
		t.Fatalf("get: %+v %v", got, err)
	}

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-create", SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-create").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-create").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-create").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testSite, testTenant).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO config.zones").WithArgs(pgxmock.AnyArg(), testTenant, testSite, "", "Dock", "Loading bay", "intrusion", testPolygon, nil, []string{}, true, "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, createdAt))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").WithArgs(testTenant, "zone-create", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectZoneAudit(mock, "zone.created", pgxmock.AnyArg(), createdAt)
	mock.ExpectCommit()
	created, err := NewPostgresRepository(mock).Create(context.Background(), command)
	if err != nil || created.Replayed || created.Zone.ConfigVersion != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}

	updatedAt := createdAt.Add(time.Minute)
	name := "Loading bay north"
	enabled := false
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectQuery("UPDATE config.zones").WithArgs(testZone, "Dock", name, testPolygon, nil, []string{}, enabled, "user-1").WillReturnRows(pgxmock.NewRows([]string{"config_version", "updated_at"}).AddRow(int64(2), updatedAt))
	expectZoneAudit(mock, "zone.updated", testZone, updatedAt)
	mock.ExpectCommit()
	updated, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, ZoneID: testZone, ExpectedVersion: 1, Name: &name, Enabled: &enabled})
	if err != nil || updated.ConfigVersion != 2 || updated.Enabled {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryFailsClosed(t *testing.T) {
	if _, err := NewPostgresRepository(nil).List(context.Background(), testTenant, ""); err == nil {
		t.Fatal("expected unavailable repository")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	if _, err := NewPostgresRepository(mock).Get(context.Background(), "bad", testZone); err == nil {
		t.Fatal("expected invalid tenant")
	}
}

func TestPostgresRepositoryReplaysNoOpsAndHidesMissingZones(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1, CreatedAt: createdAt, UpdatedAt: createdAt}

	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs("99999999-9999-4999-8999-999999999999").WillReturnRows(zoneRows(zone).RowError(0, pgx.ErrNoRows))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Get(context.Background(), testTenant, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get: %v", err)
	}

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-replay", SiteID: testSite, Floor: "Dock", Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	hash, err := hashCreate(command)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(zone)
	if err != nil {
		t.Fatal(err)
	}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-replay").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-replay").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-replay").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(hash, stored))
	mock.ExpectCommit()
	replayed, err := NewPostgresRepository(mock).Create(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.Zone.ID != testZone {
		t.Fatalf("replay: %+v %v", replayed, err)
	}

	name := zone.Name
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	unchanged, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: testZone, ExpectedVersion: 1, Name: &name})
	if err != nil || unchanged.ConfigVersion != 1 {
		t.Fatalf("unchanged update: %+v %v", unchanged, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestZoneRepositoriesCoverConflictAndValidationPaths(t *testing.T) {
	memory := NewMemoryRepository([]Zone{{ID: testZone, TenantID: testTenant, SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1}})
	if got, err := memory.Get(context.Background(), testTenant, testZone); err != nil || got.ID != testZone {
		t.Fatalf("memory get: %+v %v", got, err)
	}
	if _, err := memory.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: "99999999-9999-4999-8999-999999999999", ExpectedVersion: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("memory missing update: %v", err)
	}
	name := "Loading bay"
	if unchanged, err := memory.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: testZone, ExpectedVersion: 1, Name: &name}); err != nil || unchanged.ConfigVersion != 1 {
		t.Fatalf("memory unchanged update: %+v %v", unchanged, err)
	}
	if _, err := hashCreate(CreateCommand{Geometry: json.RawMessage("{")}); err == nil {
		t.Fatal("invalid geometry must not produce an idempotency hash")
	}

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).List(context.Background(), testTenant, "bad"); err == nil {
		t.Fatal("invalid verified site must fail")
	}

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-conflict", SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-conflict").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-conflict").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-conflict").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []byte(`{}`)))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Create(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("postgres idempotency conflict: %v", err)
	}

	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: testZone, ExpectedVersion: 2}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("postgres stale update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type failingZoneRow struct{}

func (failingZoneRow) Scan(...any) error { return errors.New("row unavailable") }

func TestPostgresRepositoryCoversZoneStorageFailures(t *testing.T) {
	if _, err := scanZone(failingZoneRow{}); err == nil {
		t.Fatal("scan failure must be returned")
	}
	if _, err := (*PostgresRepository)(nil).Get(context.Background(), testTenant, testZone); err == nil {
		t.Fatal("nil repository must fail closed")
	}

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WillReturnRows(zoneRows(zone))
	mock.ExpectCommit()
	if listed, err := NewPostgresRepository(mock).List(context.Background(), testTenant, ""); err != nil || len(listed) != 1 {
		t.Fatalf("unscoped list: %+v %v", listed, err)
	}

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-site", SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-site").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-site").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-site").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testSite, testTenant).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Create(context.Background(), command); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("missing site: %v", err)
	}

	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone).RowError(0, pgx.ErrNoRows))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: testZone, ExpectedVersion: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryFailsClosedOnStorageErrors(t *testing.T) {
	setContextFailure := errors.New("tenant context unavailable")
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenant).WillReturnError(setContextFailure)
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).List(context.Background(), testTenant, ""); !errors.Is(err, setContextFailure) {
		t.Fatalf("set context failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mock.Close()

	queryFailure := errors.New("query unavailable")
	mock, err = pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testSite).WillReturnError(queryFailure)
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).List(context.Background(), testTenant, testSite); !errors.Is(err, queryFailure) {
		t.Fatalf("list failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsBadZoneCreateAndUpdateStorage(t *testing.T) {
	invalid := CreateCommand{TenantID: testTenant, IdempotencyKey: "zone-invalid", Geometry: json.RawMessage("{")}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-invalid").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-invalid").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Create(context.Background(), invalid); err == nil {
		t.Fatal("invalid create must fail")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mock.Close()

	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1}
	mock, err = pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	writeFailure := errors.New("write unavailable")
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectQuery("UPDATE config.zones").WithArgs(testZone, "", "Loading bay north", testPolygon, nil, []string{}, true, "user-1").WillReturnError(writeFailure)
	mock.ExpectRollback()
	name := "Loading bay north"
	if _, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCommand{TenantID: testTenant, ActorID: "user-1", ZoneID: testZone, ExpectedVersion: 1, Name: &name}); !errors.Is(err, writeFailure) {
		t.Fatalf("write failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsUnreadableZoneRowsAndReplays(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	readFailure := errors.New("row unavailable")
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(Zone{}).RowError(0, readFailure))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Get(context.Background(), testTenant, testZone); !errors.Is(err, readFailure) {
		t.Fatalf("row failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mock.Close()

	command := CreateCommand{TenantID: testTenant, ActorID: "user-1", RequestID: testRequest, IdempotencyKey: "zone-bad-replay", SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true}
	hash, err := hashCreate(command)
	if err != nil {
		t.Fatal(err)
	}
	mock, err = pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	expectZoneTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(testTenant + ":zone-bad-replay").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(testTenant, "zone-bad-replay").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(testTenant, "zone-bad-replay").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(hash, []byte(`{`)))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Create(context.Background(), command); err == nil {
		t.Fatal("unreadable replay must fail")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsReadCommitFailures(t *testing.T) {
	commitFailure := errors.New("commit unavailable")
	zone := Zone{ID: testZone, TenantID: testTenant, SiteID: testSite, Name: "Loading bay", Kind: "intrusion", Geometry: testPolygon, Enabled: true, ConfigVersion: 1}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WillReturnRows(zoneRows(zone))
	mock.ExpectCommit().WillReturnError(commitFailure)
	if _, err := NewPostgresRepository(mock).List(context.Background(), testTenant, ""); !errors.Is(err, commitFailure) {
		t.Fatalf("list commit failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mock.Close()

	mock, err = pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	expectZoneTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(testZone).WillReturnRows(zoneRows(zone))
	mock.ExpectCommit().WillReturnError(commitFailure)
	if _, err := NewPostgresRepository(mock).Get(context.Background(), testTenant, testZone); !errors.Is(err, commitFailure) {
		t.Fatalf("get commit failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoiterDurationDefaultsValidatesAndUpdatesWithTheZone(t *testing.T) {
	repository := NewMemoryRepository(nil)
	created, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, SiteID: testSite, Name: "Queue", Kind: "loitering", Geometry: testPolygon, Enabled: true})
	if err != nil || created.Zone.LoiterSeconds == nil || *created.Zone.LoiterSeconds != DefaultLoiterSeconds {
		t.Fatalf("default loiter duration: %+v %v", created.Zone, err)
	}
	duration := 90
	updated, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, LoiterSeconds: &duration})
	if err != nil || updated.LoiterSeconds == nil || *updated.LoiterSeconds != duration || updated.ConfigVersion != 2 {
		t.Fatalf("update loiter duration: %+v %v", updated, err)
	}
	invalid := 29
	if _, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 2, LoiterSeconds: &invalid}); err == nil {
		t.Fatal("below-minimum loiter duration must fail")
	}
	if _, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, SiteID: testSite, Name: "Perimeter", Kind: "intrusion", Geometry: testPolygon, LoiterSeconds: &duration}); err == nil {
		t.Fatal("non-loiter zone must reject a loiter duration")
	}
}

func TestSubjectClassesAreCanonicalVersionedAndBounded(t *testing.T) {
	repository := NewMemoryRepository(nil)
	created, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, SiteID: testSite, Name: "Perimeter", Kind: "intrusion", Geometry: testPolygon, SubjectClasses: []string{" person ", "car"}})
	if err != nil || len(created.Zone.SubjectClasses) != 2 || created.Zone.SubjectClasses[0] != "car" || created.Zone.SubjectClasses[1] != "person" {
		t.Fatalf("create subject classes: %+v %v", created.Zone, err)
	}
	classes := []string{"person"}
	updated, err := repository.Update(context.Background(), UpdateCommand{TenantID: testTenant, ZoneID: created.Zone.ID, ExpectedVersion: 1, SubjectClasses: &classes})
	if err != nil || updated.ConfigVersion != 2 || len(updated.SubjectClasses) != 1 || updated.SubjectClasses[0] != "person" {
		t.Fatalf("update subject classes: %+v %v", updated, err)
	}
	if _, err := repository.Create(context.Background(), CreateCommand{TenantID: testTenant, SiteID: testSite, Name: "Invalid", Kind: "intrusion", Geometry: testPolygon, SubjectClasses: []string{"animal"}}); err == nil {
		t.Fatal("unknown class must fail")
	}
}
