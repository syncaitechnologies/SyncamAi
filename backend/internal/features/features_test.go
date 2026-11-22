package features

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	cameraA = "44444444-4444-4444-8444-444444444444"
	cameraB = "45444444-4444-4444-8444-444444444444"
)

func TestDefaultsGiveEveryPlanTheAvailableFeaturesButNotPreviews(t *testing.T) {
	effective := Resolve(Snapshot{Plan: "trial"})
	for _, key := range []Key{ZoneAlerts, Loitering, Attendance, LiveView, CameraHealth} {
		if !effective[key].On() {
			t.Fatalf("%s should be on by default: %+v", key, effective[key])
		}
	}
	if effective[Weapon].Included || effective[Weapon].On() {
		t.Fatalf("weapon is a preview and must not be included by default: %+v", effective[Weapon])
	}
	if effective[FireSmoke].Included || effective[FireSmoke].Enabled {
		t.Fatalf("coming-soon features never run: %+v", effective[FireSmoke])
	}
}

func TestOwnerChoicesAndClientSwitchesCombine(t *testing.T) {
	zero := 0
	effective := Resolve(Snapshot{
		Plan:            "active",
		LiveViewMinutes: &zero,
		Included:        map[Key]bool{Weapon: true, Loitering: false},
		Settings: map[Key]Setting{
			Weapon:     {Feature: Weapon, Enabled: true, Cameras: []string{cameraA}},
			ZoneAlerts: {Feature: ZoneAlerts, Enabled: false},
			Loitering:  {Feature: Loitering, Enabled: true},
		},
	})
	if !effective[Weapon].Allows(cameraA) || effective[Weapon].Allows(cameraB) {
		t.Fatalf("weapon should run only on the chosen camera: %+v", effective[Weapon])
	}
	if effective[ZoneAlerts].On() {
		t.Fatal("a client can switch zone alerts off")
	}
	if effective[Loitering].On() {
		t.Fatal("a switch cannot turn on a feature the plan does not include")
	}
	if effective[LiveView].Included {
		t.Fatal("zero live-view minutes means live view is not included")
	}
}

func TestEventTypesMapToFeatures(t *testing.T) {
	for eventType, want := range map[string]Key{"intrusion": ZoneAlerts, "restricted_zone": ZoneAlerts, "loitering": Loitering, "weapon_review": Weapon, "camera_health": CameraHealth} {
		if got, ok := ForEventType(eventType); !ok || got != want {
			t.Fatalf("%s: got %q %v", eventType, got, ok)
		}
	}
	if _, ok := ForEventType("vehicle_activity"); ok {
		t.Fatal("event types without a feature are never filtered")
	}
}

func TestServiceCachesForAMinuteAndUpdatesClearTheCache(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	store.Snapshots[tenantA] = Snapshot{Plan: "active"}
	store.Cameras[tenantA] = map[string]bool{cameraA: true}
	service := NewServiceWithClock(store, func() time.Time { return now })
	ctx := context.Background()

	for range 3 {
		if allowed, err := service.AllowsEvent(ctx, tenantA, "intrusion", cameraA); err != nil || !allowed {
			t.Fatalf("intrusion should be allowed: %v %v", allowed, err)
		}
	}
	if store.LoadRequests != 1 {
		t.Fatalf("expected one load within the cache window, got %d", store.LoadRequests)
	}
	off := false
	if _, err := service.Update(ctx, tenantA, ZoneAlerts, &off, nil, false, "user-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := service.AllowsEvent(ctx, tenantA, "intrusion", cameraA); allowed {
		t.Fatal("an update must apply at once on this instance")
	}
	if allowed, _ := service.AllowsEvent(ctx, tenantA, "camera_health", cameraA); !allowed {
		t.Fatal("camera health is always on")
	}
	if len(store.Changes) != 1 || store.Changes[0].ActorID != "user-1" || store.Changes[0].HadBefore {
		t.Fatalf("the change should be recorded once for audit: %+v", store.Changes)
	}

	store.Snapshots[tenantA] = Snapshot{Plan: "active"}
	if allowed, _ := service.AllowsEvent(ctx, tenantA, "intrusion", cameraA); allowed {
		t.Fatal("changes made elsewhere apply after the cache window, not before")
	}
	now = now.Add(cacheTTL)
	if allowed, _ := service.AllowsEvent(ctx, tenantA, "intrusion", cameraA); !allowed {
		t.Fatal("changes made elsewhere apply within a minute")
	}
}

func TestUpdateRefusesWhatThePlanOrWorkspaceDoesNotHave(t *testing.T) {
	store := NewMemoryStore()
	store.Snapshots[tenantA] = Snapshot{Plan: "trial"}
	store.Cameras[tenantA] = map[string]bool{cameraA: true}
	service := NewService(store)
	ctx := context.Background()
	on := true
	if _, err := service.Update(ctx, tenantA, Weapon, &on, nil, false, "user-1", "req"); !errors.Is(err, ErrNotIncluded) {
		t.Fatalf("weapon is not in a trial plan: %v", err)
	}
	if _, err := service.Update(ctx, tenantA, ZoneAlerts, nil, []string{cameraB}, true, "user-1", "req"); !errors.Is(err, ErrUnknownCamera) {
		t.Fatalf("only the workspace's cameras can be chosen: %v", err)
	}
	if _, err := service.Update(ctx, tenantA, CameraHealth, &on, nil, false, "user-1", "req"); !errors.Is(err, ErrNotSwitchable) {
		t.Fatalf("camera health cannot be switched: %v", err)
	}
	if _, err := service.Update(ctx, tenantA, LiveView, nil, []string{cameraA}, true, "user-1", "req"); !errors.Is(err, ErrNotSwitchable) {
		t.Fatalf("live view is not per camera: %v", err)
	}
	resolved, err := service.Update(ctx, tenantA, ZoneAlerts, nil, []string{cameraA, cameraA}, true, "user-1", "req")
	if err != nil || len(resolved.Cameras) != 1 || !resolved.Allows(cameraA) {
		t.Fatalf("chosen cameras are kept once each: %+v %v", resolved, err)
	}
	off := false
	if _, err := service.Update(ctx, tenantA, Weapon, &off, nil, false, "user-1", "req"); err != nil {
		t.Fatalf("switching off a feature the plan lacks is allowed: %v", err)
	}
}

func TestAllowsFailsOpenWhenStateCannotBeRead(t *testing.T) {
	store := NewMemoryStore()
	store.FailLoad = errors.New("database down")
	allowed, err := NewService(store).AllowsEvent(context.Background(), tenantA, "intrusion", cameraA)
	if err == nil || !allowed {
		t.Fatalf("a missed intrusion is worse than an unwanted alert: %v %v", allowed, err)
	}
}
