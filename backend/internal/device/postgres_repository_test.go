package device

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

const postgresRequestID = "66666666-6666-4666-8666-666666666666"

func cameraRows(camera Camera) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "serial_number", "name", "group_name", "tags", "lifecycle_status", "config_version", "created_at", "updated_at"}).
		AddRow(camera.ID, camera.TenantID, camera.SiteID, camera.SerialNumber, camera.Name, camera.GroupName, camera.Tags, camera.LifecycleStatus, camera.ConfigVersion, camera.CreatedAt, camera.UpdatedAt)
}

func expectTenantTransaction(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantA).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func expectAudit(mock pgxmock.PgxPoolIface, action string, resourceID any, occurredAt time.Time) {
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":2026-08-13").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantA, "2026-08-13").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantA, "2026-08-13", occurredAt, "user-1", action, "camera", resourceID,
		postgresRequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestPostgresRepositoryListsAndGetsCameras(t *testing.T) {
	createdAt := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	camera := Camera{ID: cameraA, TenantID: tenantA, SiteID: siteA, SerialNumber: "SN-01", Name: "Gate", GroupName: "Perimeter", Tags: []string{"gate"}, LifecycleStatus: "active", ConfigVersion: 2, CreatedAt: createdAt, UpdatedAt: createdAt}

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(siteA).WillReturnRows(cameraRows(camera))
	mock.ExpectCommit()
	listed, err := NewPostgresRepository(mock).List(context.Background(), tenantA, siteA)
	if err != nil || len(listed) != 1 || listed[0].ID != cameraA {
		t.Fatalf("unexpected list: %+v %v", listed, err)
	}

	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(camera))
	mock.ExpectCommit()
	got, err := NewPostgresRepository(mock).Get(context.Background(), tenantA, cameraA)
	if err != nil || got.Name != "Gate" {
		t.Fatalf("unexpected get: %+v %v", got, err)
	}

	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("SELECT id::text").WithArgs("99999999-9999-4999-8999-999999999999").WillReturnRows(cameraRows(camera).RowError(0, pgx.ErrNoRows))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Get(context.Background(), tenantA, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrCameraNotFound) {
		t.Fatalf("expected camera not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryCreatesAuditedCameraAndReplays(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	command := CreateCameraCommand{
		TenantID: tenantA, ActorID: "user-1", RequestID: postgresRequestID, IdempotencyKey: "create-camera",
		SiteID: siteA, SerialNumber: " sn-01 ", Name: "Gate", GroupName: "Perimeter", Tags: []string{"Gate"},
	}
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":create-camera").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(tenantA, "create-camera").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "create-camera").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(siteA, tenantA).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO config.cameras").WithArgs(pgxmock.AnyArg(), tenantA, siteA, "SN-01", "Gate", "Perimeter", pgxmock.AnyArg(), "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, createdAt))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").WithArgs(tenantA, "create-camera", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectAudit(mock, "camera.created", pgxmock.AnyArg(), createdAt)
	mock.ExpectCommit()
	created, err := NewPostgresRepository(mock).Create(context.Background(), command)
	if err != nil || created.Replayed || created.Camera.SerialNumber != "SN-01" || created.Camera.ConfigVersion != 1 {
		t.Fatalf("unexpected create: %+v %v", created, err)
	}

	hash, err := hashCreateCamera(command)
	if err != nil {
		t.Fatal(err)
	}
	response, err := json.Marshal(created.Camera)
	if err != nil {
		t.Fatal(err)
	}
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":create-camera").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(tenantA, "create-camera").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "create-camera").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(hash, response))
	mock.ExpectCommit()
	replayed, err := NewPostgresRepository(mock).Create(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.Camera.ID != created.Camera.ID {
		t.Fatalf("unexpected replay: %+v %v", replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsCreateConflicts(t *testing.T) {
	command := CreateCameraCommand{TenantID: tenantA, ActorID: "user-1", RequestID: postgresRequestID, IdempotencyKey: "create-camera", SiteID: siteA, SerialNumber: "SN-01", Name: "Gate"}
	hash, err := hashCreateCamera(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		hash       string
		siteExists bool
		want       error
	}{
		{name: "idempotency", hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: ErrIdempotencyConflict},
		{name: "site", hash: hash, siteExists: false, want: ErrSiteNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			defer mock.Close()
			expectTenantTransaction(mock, pgx.ReadWrite)
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":create-camera").WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(tenantA, "create-camera").WillReturnResult(pgxmock.NewResult("DELETE", 0))
			if test.name == "idempotency" {
				mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "create-camera").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(test.hash, []byte(`{}`)))
			} else {
				mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "create-camera").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
				mock.ExpectQuery("SELECT EXISTS").WithArgs(siteA, tenantA).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(test.siteExists))
			}
			mock.ExpectRollback()
			if _, err := NewPostgresRepository(mock).Create(context.Background(), command); !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresRepositoryUpdatesAndRetiresWithAudit(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	current := Camera{ID: cameraA, TenantID: tenantA, SiteID: siteA, SerialNumber: "SN-01", Name: "Gate", GroupName: "Perimeter", Tags: []string{"gate"}, LifecycleStatus: "pending", ConfigVersion: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	name, status := "Gate north", "active"
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(current))
	mock.ExpectQuery("UPDATE config.cameras SET name").WithArgs(cameraA, name, "Perimeter", pgxmock.AnyArg(), status, "user-1").WillReturnRows(pgxmock.NewRows([]string{"config_version", "updated_at"}).AddRow(int64(2), updatedAt))
	expectAudit(mock, "camera.updated", cameraA, updatedAt)
	mock.ExpectCommit()
	updated, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, ActorID: "user-1", RequestID: postgresRequestID, CameraID: cameraA, ExpectedVersion: 1, Name: &name, LifecycleStatus: &status})
	if err != nil || updated.Name != name || updated.ConfigVersion != 2 || updated.LifecycleStatus != "active" {
		t.Fatalf("unexpected update: %+v %v", updated, err)
	}

	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(updated))
	mock.ExpectQuery("UPDATE config.cameras SET lifecycle_status").WithArgs(cameraA, "user-1").WillReturnRows(pgxmock.NewRows([]string{"config_version", "updated_at"}).AddRow(int64(3), updatedAt.Add(time.Minute)))
	expectAudit(mock, "camera.retired", cameraA, updatedAt.Add(time.Minute))
	mock.ExpectCommit()
	retired, err := NewPostgresRepository(mock).Retire(context.Background(), RetireCameraCommand{TenantID: tenantA, ActorID: "user-1", RequestID: postgresRequestID, CameraID: cameraA})
	if err != nil || retired.ConfigVersion != 3 || retired.LifecycleStatus != "retired" {
		t.Fatalf("unexpected retirement: %+v %v", retired, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryHandlesNoOpStaleAndRetiredMutations(t *testing.T) {
	createdAt := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	current := Camera{ID: cameraA, TenantID: tenantA, SiteID: siteA, SerialNumber: "SN-01", Name: "Gate", LifecycleStatus: "active", ConfigVersion: 2, CreatedAt: createdAt, UpdatedAt: createdAt}

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	name := "Gate"
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(current))
	mock.ExpectCommit()
	unchanged, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 2, Name: &name})
	if err != nil || unchanged.ConfigVersion != 2 {
		t.Fatalf("unexpected no-op: %+v %v", unchanged, err)
	}

	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(current))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 1, Name: &name}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}

	current.LifecycleStatus = "retired"
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT id::text").WithArgs(cameraA).WillReturnRows(cameraRows(current))
	mock.ExpectCommit()
	replayed, err := NewPostgresRepository(mock).Retire(context.Background(), RetireCameraCommand{TenantID: tenantA, CameraID: cameraA})
	if err != nil || replayed.LifecycleStatus != "retired" {
		t.Fatalf("unexpected retired replay: %+v %v", replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryFailsClosedAndClassifiesWrites(t *testing.T) {
	if _, err := (*PostgresRepository)(nil).List(context.Background(), tenantA, ""); err == nil {
		t.Fatal("expected unavailable repository error")
	}
	if _, err := NewPostgresRepository(nil).Get(context.Background(), tenantA, cameraA); err == nil {
		t.Fatal("expected unavailable repository error")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	if _, err := NewPostgresRepository(mock).List(context.Background(), "bad", ""); err == nil {
		t.Fatal("expected invalid tenant error")
	}
	expectTenantTransaction(mock, pgx.ReadOnly)
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).List(context.Background(), tenantA, "bad"); err == nil {
		t.Fatal("expected invalid site error")
	}
	if !errors.Is(classifyCameraWrite(&pgconn.PgError{Code: "23503"}), ErrSiteNotFound) {
		t.Fatal("foreign key must map to site not found")
	}
	if !errors.Is(classifyCameraWrite(&pgconn.PgError{Code: "23505"}), ErrSerialConflict) {
		t.Fatal("unique violation must map to serial conflict")
	}
	if classifyCameraWrite(errors.New("database unavailable")) == nil {
		t.Fatal("unexpected database error must be wrapped")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
