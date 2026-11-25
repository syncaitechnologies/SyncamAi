package attendance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestMemoryDailyGroupsBySiteLocalDay(t *testing.T) {
	repository := NewMemoryRepository(tenant + ":" + site)
	ctx := context.Background()
	member, err := repository.CreateStaff(ctx, CreateStaffCommand{TenantID: tenant, SiteID: site, ActorID: "admin", DisplayName: "Asha", EmployeeCode: "E1"})
	if err != nil {
		t.Fatal(err)
	}
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	sightings := []time.Time{
		time.Date(2026, 11, 22, 3, 30, 0, 0, time.UTC),  // 22 Nov 09:00 IST
		time.Date(2026, 11, 22, 12, 30, 0, 0, time.UTC), // 22 Nov 18:00 IST
		time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC),  // 23 Nov 01:30 IST: the next local day
		time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC),   // 24 Nov 08:30 IST
	}
	for index, at := range sightings {
		id := []string{"a1111111-1111-4111-8111-111111111111", "a2222222-2222-4222-8222-222222222222", "a3333333-3333-4333-8333-333333333333", "a4444444-4444-4444-8444-444444444444"}[index]
		if _, err := repository.RecordSighting(ctx, Record{ID: id, TenantID: tenant, SiteID: site, StaffID: member.ID, CameraID: camera, OccurredAt: at, Confidence: 0.8}); err != nil {
			t.Fatal(err)
		}
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, kolkata)
	to := time.Date(2026, 11, 25, 0, 0, 0, 0, kolkata)
	days, err := repository.ListDaily(ctx, tenant, site, kolkata, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[0].Date != "2026-11-24" || days[1].Date != "2026-11-23" || days[2].Date != "2026-11-22" {
		t.Fatalf("one row per local day, newest first: %+v", days)
	}
	if days[2].Sightings != 2 || !days[2].FirstSeen.Equal(sightings[0]) || !days[2].LastSeen.Equal(sightings[1]) || days[2].EmployeeCode != "E1" {
		t.Fatalf("first and last sighting of the day: %+v", days[2])
	}
}

func TestPostgresDailySummary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	first := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("AT TIME ZONE").WithArgs(site, from, to, "UTC", MaxDailyRows).WillReturnRows(
		pgxmock.NewRows([]string{"day", "staff_id", "display_name", "employee_code", "first_seen", "last_seen", "sightings", "camera_id"}).
			AddRow("2026-11-24", staff, "Asha", "E1", first, first.Add(9*time.Hour), int64(42), camera))
	mock.ExpectCommit()
	mock.ExpectRollback()
	days, err := NewPostgresRepository(mock).ListDaily(context.Background(), tenant, site, nil, from, to)
	if err != nil || len(days) != 1 || days[0].Sightings != 42 || days[0].Date != "2026-11-24" || days[0].CameraID != camera {
		t.Fatalf("daily: %+v %v", days, err)
	}

	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("AT TIME ZONE").WithArgs(site, from, to, "UTC", MaxDailyRows).WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()
	if _, err := NewPostgresRepository(mock).ListDaily(context.Background(), tenant, site, time.UTC, from, to); err == nil {
		t.Fatal("a database error is returned")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
