package livefeed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

type memoryUsage struct {
	limits map[string]*int
	used   map[usageKey]int64
	loads  int
	fail   error
}

func (m *memoryUsage) Load(_ context.Context, tenantID string, month time.Time) (*int, int64, error) {
	m.loads++
	if m.fail != nil {
		return nil, 0, m.fail
	}
	return m.limits[tenantID], m.used[usageKey{tenantID, month}], nil
}

func (m *memoryUsage) Add(_ context.Context, tenantID string, month time.Time, seconds int64) error {
	if m.fail != nil {
		return m.fail
	}
	m.used[usageKey{tenantID, month}] += seconds
	return nil
}

func minutes(n int) *int { return &n }

func newTestMeter(limits map[string]*int) (*Meter, *memoryUsage, *clock) {
	store := &memoryUsage{limits: limits, used: map[usageKey]int64{}}
	c := &clock{now: time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)}
	return NewMeterWithClock(store, c.Now), store, c
}

func TestMeterCountsViewingTimeAgainstTheAllowance(t *testing.T) {
	meter, store, _ := newTestMeter(map[string]*int{tenantA: minutes(10)})
	ctx := context.Background()
	usage, err := meter.Usage(ctx, tenantA)
	if err != nil || !usage.Limited() || usage.Remaining() != 10*time.Minute || usage.Exhausted() {
		t.Fatalf("fresh allowance: %+v %v", usage, err)
	}
	if !usage.Month.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !usage.ResetsAt().Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month window: %v %v", usage.Month, usage.ResetsAt())
	}
	usage, _ = meter.Charge(ctx, tenantA, 9*time.Minute+30*time.Second)
	if usage.Remaining() != 30*time.Second || usage.Exhausted() {
		t.Fatalf("after 9.5 minutes: %+v", usage)
	}
	if err := meter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.used[usageKey{tenantA, usage.Month}]; got != 570 {
		t.Fatalf("stored seconds = %d", got)
	}
	usage, _ = meter.Charge(ctx, tenantA, 31*time.Second)
	if !usage.Exhausted() || usage.Remaining() != 0 {
		t.Fatalf("over the allowance: %+v", usage)
	}
	if store.loads != 1 {
		t.Fatalf("usage is cached between polls, loads = %d", store.loads)
	}
}

func TestMeterUnlimitedAndOff(t *testing.T) {
	meter, _, _ := newTestMeter(map[string]*int{tenantA: nil, "off": minutes(0)})
	ctx := context.Background()
	usage, err := meter.Charge(ctx, tenantA, 10*time.Hour)
	if err != nil || usage.Limited() || usage.Exhausted() {
		t.Fatalf("unlimited workspace: %+v %v", usage, err)
	}
	if usage, _ := meter.Usage(ctx, "off"); !usage.Exhausted() {
		t.Fatalf("a zero allowance means live view is off: %+v", usage)
	}
}

func TestMeterReloadsAfterAMinuteAndAtANewMonth(t *testing.T) {
	limits := map[string]*int{tenantA: minutes(10)}
	meter, store, c := newTestMeter(limits)
	ctx := context.Background()
	_, _ = meter.Usage(ctx, tenantA)
	limits[tenantA] = minutes(60)
	c.Advance(30 * time.Second)
	if usage, _ := meter.Usage(ctx, tenantA); *usage.LimitMinutes != 10 {
		t.Fatal("within a minute the cached allowance is used")
	}
	c.Advance(31 * time.Second)
	if usage, _ := meter.Usage(ctx, tenantA); *usage.LimitMinutes != 60 {
		t.Fatal("a raised allowance applies within a minute")
	}
	_, _ = meter.Charge(ctx, tenantA, 5*time.Minute)
	c.now = time.Date(2026, 12, 1, 0, 0, 1, 0, time.UTC)
	usage, _ := meter.Usage(ctx, tenantA)
	if usage.Used != 0 || !usage.Month.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("a new month starts from zero: %+v", usage)
	}
	if err := meter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.used[usageKey{tenantA, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}]; got != 300 {
		t.Fatalf("last month's viewing is saved to last month: %d", got)
	}
}

func TestMeterKeepsUnsavedTimeAndFailsClosed(t *testing.T) {
	meter, store, _ := newTestMeter(map[string]*int{tenantA: minutes(10)})
	ctx := context.Background()
	_, _ = meter.Charge(ctx, tenantA, 90*time.Second+500*time.Millisecond)
	store.fail = errors.New("database down")
	if err := meter.Flush(ctx); err == nil {
		t.Fatal("a failed save is reported")
	}
	store.fail = nil
	if err := meter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.used[usageKey{tenantA, monthStart(meter.now())}]; got != 90 {
		t.Fatalf("time is saved once the database is back: %d", got)
	}
	if usage, _ := meter.Usage(ctx, tenantA); usage.Used != 90*time.Second+500*time.Millisecond {
		t.Fatalf("part-seconds stay counted: %v", usage.Used)
	}
	cold, failing, _ := newTestMeter(nil)
	failing.fail = errors.New("database down")
	if _, err := cold.Usage(ctx, tenantA); err == nil {
		t.Fatal("without a readable allowance the meter fails closed")
	}
	var missing *Meter
	if _, err := missing.Usage(ctx, tenantA); err == nil {
		t.Fatal("a missing meter fails closed")
	}
}

func TestPostgresUsageReadsAndAdds(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	limit := int32(10)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantA).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("live_view_minutes").WithArgs(tenantA, "2026-11-01").WillReturnRows(pgxmock.NewRows([]string{"limit", "used"}).AddRow(&limit, int64(75)))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantA).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("INSERT INTO platform.live_view_usage").WithArgs(tenantA, "2026-11-01", int64(30)).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	store := NewPostgresUsage(mock)
	got, used, err := store.Load(context.Background(), tenantA, month)
	if err != nil || got == nil || *got != 10 || used != 75 {
		t.Fatalf("load: %v %d %v", got, used, err)
	}
	if err := store.Add(context.Background(), tenantA, month, 30); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background(), "not-a-uuid", month); err == nil {
		t.Fatal("an invalid tenant fails")
	}
}
