package attendance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	tenant = "11111111-1111-4111-8111-111111111111"
	site   = "22222222-2222-4222-8222-222222222222"
	staff  = "33333333-3333-4333-8333-333333333333"
	camera = "44444444-4444-4444-8444-444444444444"
	record = "55555555-5555-4555-8555-555555555555"
)

func TestMemoryRepositoryLifecycle(t *testing.T) {
	repository := NewMemoryRepository(tenant + ":" + site)
	ctx := context.Background()
	if _, err := repository.CreateStaff(ctx, CreateStaffCommand{TenantID: tenant, SiteID: "other"}); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("unknown site: %v", err)
	}
	member, err := repository.CreateStaff(ctx, CreateStaffCommand{TenantID: tenant, SiteID: site, ActorID: "admin", DisplayName: " Asha ", EmployeeCode: "E1"})
	if err != nil || member.DisplayName != "Asha" || !member.Active || member.ConsentRecordedBy != "admin" {
		t.Fatalf("create: %v %+v", err, member)
	}
	now := time.Now().UTC()
	sighting := Record{ID: record, TenantID: tenant, SiteID: site, StaffID: member.ID, CameraID: camera, OccurredAt: now, Confidence: 0.8}
	if created, err := repository.RecordSighting(ctx, sighting); err != nil || !created {
		t.Fatalf("record: %v %v", created, err)
	}
	if created, err := repository.RecordSighting(ctx, sighting); err != nil || created {
		t.Fatalf("replay must be accepted without duplicates: %v %v", created, err)
	}
	records, _ := repository.ListRecords(ctx, tenant, site, now.Add(-time.Hour), now.Add(time.Hour))
	if len(records) != 1 || records[0].StaffName != "Asha" {
		t.Fatalf("records: %+v", records)
	}
	if _, err := repository.SetStaffActive(ctx, "other", member.ID, false); !errors.Is(err, ErrStaffNotFound) {
		t.Fatalf("cross-tenant update: %v", err)
	}
	if updated, err := repository.SetStaffActive(ctx, tenant, member.ID, false); err != nil || updated.Active {
		t.Fatalf("deactivate: %v", err)
	}
	sighting.ID = "66666666-6666-4666-8666-666666666666"
	if _, err := repository.RecordSighting(ctx, sighting); !errors.Is(err, ErrStaffNotFound) {
		t.Fatalf("inactive staff must not be recorded: %v", err)
	}
	listed, _ := repository.ListStaff(ctx, tenant, "")
	if len(listed) != 1 {
		t.Fatalf("list: %+v", listed)
	}
}

func expectTx(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(tenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func staffRow(active bool) *pgxmock.Rows {
	now := time.Now()
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "display_name", "employee_code", "consent_recorded_at", "consent_recorded_by", "active", "created_at"}).
		AddRow(staff, tenant, site, "Asha", "E1", now, "admin", active, now)
}

func TestPostgresRepository(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	repository := NewPostgresRepository(mock)
	ctx := context.Background()
	now := time.Now()

	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM attendance.staff").WithArgs(site).WillReturnRows(staffRow(true))
	mock.ExpectCommit()
	if listed, err := repository.ListStaff(ctx, tenant, site); err != nil || len(listed) != 1 {
		t.Fatalf("list staff: %v %+v", err, listed)
	}

	expectTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("INSERT INTO attendance.staff").WithArgs(pgxmock.AnyArg(), tenant, site, "Asha", "E1", "admin").WillReturnRows(pgxmock.NewRows([]string{"consent_recorded_at", "created_at"}).AddRow(now, now))
	mock.ExpectCommit()
	if created, err := repository.CreateStaff(ctx, CreateStaffCommand{TenantID: tenant, SiteID: site, ActorID: "admin", DisplayName: "Asha", EmployeeCode: "E1"}); err != nil || created.ConsentRecordedAt.IsZero() {
		t.Fatalf("create staff: %v", err)
	}

	expectTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("UPDATE attendance.staff").WithArgs(staff, false).WillReturnRows(staffRow(false))
	mock.ExpectCommit()
	if updated, err := repository.SetStaffActive(ctx, tenant, staff, false); err != nil || updated.Active {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := repository.SetStaffActive(ctx, tenant, "bad", false); !errors.Is(err, ErrStaffNotFound) {
		t.Fatalf("bad id: %v", err)
	}

	sighting := Record{ID: record, TenantID: tenant, SiteID: site, StaffID: staff, CameraID: camera, OccurredAt: now, Confidence: 0.8}
	expectTx(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO attendance.records").WithArgs(record, tenant, site, staff, camera, now, 0.8).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	if created, err := repository.RecordSighting(ctx, sighting); err != nil || !created {
		t.Fatalf("record: %v %v", created, err)
	}
	expectTx(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO attendance.records").WithArgs(record, tenant, site, staff, camera, now, 0.8).WillReturnResult(pgxmock.NewResult("INSERT", 0))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(record).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	if _, err := repository.RecordSighting(ctx, sighting); !errors.Is(err, ErrStaffNotFound) {
		t.Fatalf("inactive staff: %v", err)
	}
	expectTx(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO attendance.records").WithArgs(record, tenant, site, staff, camera, now, 0.8).WillReturnResult(pgxmock.NewResult("INSERT", 0))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(record).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectCommit()
	if created, err := repository.RecordSighting(ctx, sighting); err != nil || created {
		t.Fatalf("replay: %v %v", created, err)
	}

	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM attendance.records").WithArgs(site, now.Add(-time.Hour), now).WillReturnRows(
		pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "staff_id", "display_name", "camera_id", "occurred_at", "confidence"}).
			AddRow(record, tenant, site, staff, "Asha", camera, now, float32(0.8)))
	mock.ExpectCommit()
	records, err := repository.ListRecords(ctx, tenant, site, now.Add(-time.Hour), now)
	if err != nil || len(records) != 1 || records[0].StaffName != "Asha" {
		t.Fatalf("records: %v %+v", err, records)
	}

	failure := errors.New("down")
	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM attendance.records").WithArgs(site, now.Add(-time.Hour), now).WillReturnError(failure)
	mock.ExpectRollback()
	if _, err := repository.ListRecords(ctx, tenant, site, now.Add(-time.Hour), now); !errors.Is(err, failure) {
		t.Fatalf("failure: %v", err)
	}
	if _, err := repository.ListStaff(ctx, "bad", ""); err == nil {
		t.Fatal("invalid tenant must fail")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
