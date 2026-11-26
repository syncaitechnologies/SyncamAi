package analytics

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
	door   = "33333333-3333-4333-8333-333333333333"
	table  = "34333333-3333-4333-8333-333333333333"
	runA   = "44444444-4444-4444-8444-444444444444"
	runB   = "45444444-4444-4444-8444-444444444444"
)

func TestMemorySummaryAddsRunsAndGroupsBySiteLocalDay(t *testing.T) {
	store := NewMemoryStore()
	store.Names[door] = [2]string{"Front door", "door_line"}
	store.Names[table] = [2]string{"Table 4", "table"}
	ctx := context.Background()
	toronto, _ := time.LoadLocation("America/Toronto")
	evening := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC) // 24 Nov 19:00 in Toronto
	noon := time.Date(2026, 11, 25, 17, 0, 0, 0, time.UTC)   // 25 Nov 12:00 in Toronto
	if err := store.SaveHours(ctx, tenant, site, []ZoneHour{
		{ZoneID: door, HourStart: evening, RunID: runA, Entries: 10, Exits: 8},
		{ZoneID: door, HourStart: noon, RunID: runA, Entries: 5},
		{ZoneID: door, HourStart: noon, RunID: runB, Entries: 7}, // the box restarted mid-hour
		{ZoneID: table, HourStart: noon, RunID: runB, OccupiedSeconds: 1800, Sessions: 2, SessionSeconds: 3000, PeakPeople: 4},
	}); err != nil {
		t.Fatal(err)
	}
	// A retry of the same run's row replaces it rather than adding.
	_ = store.SaveHours(ctx, tenant, site, []ZoneHour{{ZoneID: door, HourStart: noon, RunID: runB, Entries: 7}})
	summary, err := store.Summary(ctx, tenant, site, toronto, time.Date(2026, 11, 24, 0, 0, 0, 0, toronto), time.Date(2026, 11, 26, 0, 0, 0, 0, toronto))
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Days) != 2 || summary.Days[0].Date != "2026-11-24" || summary.Days[0].Entries != 10 || summary.Days[1].Entries != 12 {
		t.Fatalf("days: %+v", summary.Days)
	}
	if len(summary.Hours) != 2 || summary.Hours[0].Hour != 12 || summary.Hours[0].Entries != 12 || summary.Hours[1].Hour != 19 {
		t.Fatalf("hours of the day: %+v", summary.Hours)
	}
	if len(summary.Zones) != 2 || summary.Zones[0].Name != "Front door" || summary.Zones[1].Sessions != 2 || summary.Zones[1].PeakPeople != 4 {
		t.Fatalf("zones: %+v", summary.Zones)
	}
	if summary.OpenHours != 2 {
		t.Fatalf("open hours: %d", summary.OpenHours)
	}
	other, _ := store.Summary(ctx, "other", site, toronto, time.Date(2026, 11, 24, 0, 0, 0, 0, toronto), time.Date(2026, 11, 26, 0, 0, 0, 0, toronto))
	if len(other.Days) != 0 {
		t.Fatal("another workspace sees nothing")
	}
	if totals, _ := store.Totals(ctx, tenant, noon); totals != (Totals{Visitors: 12, Sessions: 2, SessionSeconds: 3000}) {
		t.Fatalf("totals since noon: %+v", totals)
	}
	if totals, _ := store.Totals(ctx, "other", evening); totals != (Totals{}) {
		t.Fatalf("another workspace's totals: %+v", totals)
	}
}

func TestPostgresStoreTotals(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	since := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	store := NewPostgresStore(mock)
	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("sum\\(entries\\)").WithArgs(tenant, since).WillReturnRows(pgxmock.NewRows([]string{"v", "s", "ss"}).AddRow(40, 3, int64(5400)))
	mock.ExpectCommit()
	mock.ExpectRollback()
	if totals, err := store.Totals(context.Background(), tenant, since); err != nil || totals != (Totals{Visitors: 40, Sessions: 3, SessionSeconds: 5400}) {
		t.Fatalf("totals: %+v %v", totals, err)
	}
	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("sum\\(entries\\)").WithArgs(tenant, since).WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()
	if _, err := store.Totals(context.Background(), tenant, since); err == nil {
		t.Fatal("a failed read is reported")
	}
	if _, err := store.Totals(context.Background(), "not-a-uuid", since); err == nil {
		t.Fatal("a bad tenant is refused")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectTx(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(tenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func TestPostgresStoreSavesAndSummarises(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	hour := time.Date(2026, 11, 25, 17, 0, 0, 0, time.UTC)
	from, to := hour.Add(-24*time.Hour), hour.Add(24*time.Hour)
	expectTx(mock, pgx.ReadWrite)
	mock.ExpectExec("INSERT INTO platform.zone_hours").WithArgs(tenant, site, door, hour, runA, 5, 1, 0, 0, int64(0), 0).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	store := NewPostgresStore(mock)
	if err := store.SaveHours(context.Background(), tenant, site, []ZoneHour{{ZoneID: door, HourStart: hour, RunID: runA, Entries: 5, Exits: 1}}); err != nil {
		t.Fatal(err)
	}

	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("to_char").WithArgs(site, from, to, "UTC").WillReturnRows(pgxmock.NewRows([]string{"day", "entries", "exits"}).AddRow("2026-11-25", 5, 1))
	mock.ExpectQuery("extract\\(hour").WithArgs(site, from, to, "UTC").WillReturnRows(pgxmock.NewRows([]string{"hour", "entries"}).AddRow(17, 5))
	mock.ExpectQuery("LEFT JOIN config.zones").WithArgs(site, from, to).WillReturnRows(
		pgxmock.NewRows([]string{"zone_id", "name", "kind", "entries", "exits", "occupied", "sessions", "session_seconds", "peak"}).
			AddRow(door, "Front door", "door_line", 5, 1, int64(0), 0, int64(0), 0))
	mock.ExpectQuery("count\\(DISTINCT hour_start\\)").WithArgs(site, from, to).WillReturnRows(pgxmock.NewRows([]string{"open"}).AddRow(1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	summary, err := store.Summary(context.Background(), tenant, site, nil, from, to)
	if err != nil || len(summary.Days) != 1 || summary.Hours[0].Hour != 17 || summary.Zones[0].Name != "Front door" || summary.OpenHours != 1 {
		t.Fatalf("summary: %+v %v", summary, err)
	}

	expectTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("to_char").WithArgs(site, from, to, "UTC").WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()
	if _, err := store.Summary(context.Background(), tenant, site, time.UTC, from, to); err == nil {
		t.Fatal("a database error is returned")
	}
	if _, err := NewPostgresStore(nil).Summary(context.Background(), tenant, site, time.UTC, from, to); err == nil {
		t.Fatal("a missing pool fails")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
