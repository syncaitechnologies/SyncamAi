package zones

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeScheduleCanonicalizesAndRejectsBadInput(t *testing.T) {
	for _, empty := range []string{"", "null", "{}", "  "} {
		value, err := NormalizeSchedule(json.RawMessage(empty))
		if err != nil || value != nil {
			t.Fatalf("%q should mean always active: %v %s", empty, err, value)
		}
	}
	value, err := NormalizeSchedule(json.RawMessage(`{"timezone":"Asia/Kolkata","windows":[{"days":["SUN","mon"],"start":"22:00","end":"06:00"}]}`))
	if err != nil || string(value) != `{"timezone":"Asia/Kolkata","windows":[{"days":["mon","sun"],"start":"22:00","end":"06:00"}]}` {
		t.Fatalf("canonical schedule: %v %s", err, value)
	}
	for _, bad := range []string{
		`{"timezone":"Mars/Base","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`,
		`{"timezone":"UTC","windows":[]}`,
		`{"timezone":"UTC","windows":[{"days":["mon"],"start":"25:00","end":"06:00"}]}`,
		`{"timezone":"UTC","windows":[{"days":["mon"],"start":"06:00","end":"06:00"}]}`,
		`{"timezone":"UTC","windows":[{"days":["funday"],"start":"01:00","end":"06:00"}]}`,
		`{"timezone":"UTC","windows":[{"days":["mon","mon"],"start":"01:00","end":"06:00"}]}`,
		`{"timezone":"UTC","windows":[{"days":["mon"],"start":"01:00","end":"06:00"}],"extra":1}`,
		`[1,2]`,
	} {
		if _, err := NormalizeSchedule(json.RawMessage(bad)); err == nil {
			t.Fatalf("accepted bad schedule %s", bad)
		}
	}
}

func TestActiveHandlesOvernightWindowsAndTimezones(t *testing.T) {
	schedule, _ := NormalizeSchedule(json.RawMessage(`{"timezone":"Asia/Kolkata","windows":[{"days":["mon"],"start":"22:00","end":"06:00"},{"days":["sat"],"start":"09:00","end":"17:00"}]}`))
	ist := time.FixedZone("IST", 5*3600+1800)
	cases := []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, 11, 16, 23, 0, 0, 0, ist), true},   // Monday 23:00
		{time.Date(2026, 11, 17, 5, 59, 0, 0, ist), true},   // Tuesday 05:59, Monday's window
		{time.Date(2026, 11, 17, 6, 0, 0, 0, ist), false},   // window end is exclusive
		{time.Date(2026, 11, 16, 21, 59, 0, 0, ist), false}, // before start
		{time.Date(2026, 11, 21, 12, 0, 0, 0, ist), true},   // Saturday daytime
		{time.Date(2026, 11, 22, 12, 0, 0, 0, ist), false},  // Sunday
	}
	for _, tc := range cases {
		if got := Active(schedule, tc.at.UTC()); got != tc.want {
			t.Fatalf("Active(%s) = %v, want %v", tc.at, got, tc.want)
		}
	}
	if !Active(nil, time.Now()) {
		t.Fatal("nil schedule is always active")
	}
}

func TestMemoryRepositoryStoresAndUpdatesSchedules(t *testing.T) {
	repository := NewMemoryRepository(nil)
	schedule := json.RawMessage(`{"timezone":"UTC","windows":[{"days":["fri"],"start":"18:00","end":"23:00"}]}`)
	created, err := repository.Create(t.Context(), CreateCommand{TenantID: "t", SiteID: "s", IdempotencyKey: "k", Name: "Yard", Kind: "intrusion", Geometry: json.RawMessage(`{}`), Enabled: true, Schedule: schedule})
	if err != nil || len(created.Zone.Schedule) == 0 {
		t.Fatalf("create: %v %+v", err, created.Zone)
	}
	cleared := json.RawMessage(`{}`)
	updated, err := repository.Update(t.Context(), UpdateCommand{TenantID: "t", ZoneID: created.Zone.ID, ExpectedVersion: 1, Schedule: &cleared})
	if err != nil || updated.Schedule != nil || updated.ConfigVersion != 2 {
		t.Fatalf("clear schedule: %v %+v", err, updated)
	}
	bad := json.RawMessage(`{"timezone":"nope","windows":[]}`)
	if _, err := repository.Update(t.Context(), UpdateCommand{TenantID: "t", ZoneID: created.Zone.ID, ExpectedVersion: 2, Schedule: &bad}); err == nil {
		t.Fatal("invalid schedule update accepted")
	}
}
