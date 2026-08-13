package device

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
	siteA   = "33333333-3333-4333-8333-333333333333"
	siteB   = "44444444-4444-4444-8444-444444444444"
	cameraA = "55555555-5555-4555-8555-555555555555"
)

func TestMemoryRepositoryFiltersScopeAndCopiesTags(t *testing.T) {
	repository := NewMemoryRepository([]Camera{
		{ID: "b", TenantID: tenantA, SiteID: siteA, LifecycleStatus: "active", Tags: []string{"gate"}},
		{ID: "a", TenantID: tenantA, SiteID: siteB, LifecycleStatus: "offline"},
		{ID: "retired", TenantID: tenantA, SiteID: siteA, LifecycleStatus: "retired"},
		{ID: "foreign", TenantID: tenantB, SiteID: siteA, LifecycleStatus: "active"},
	})
	all, err := repository.List(context.Background(), tenantA, "")
	if err != nil || len(all) != 2 || all[0].ID != "a" || all[1].ID != "b" {
		t.Fatalf("unexpected tenant list: %+v %v", all, err)
	}
	site, err := repository.List(context.Background(), tenantA, siteA)
	if err != nil || len(site) != 1 || site[0].ID != "b" {
		t.Fatalf("unexpected site list: %+v %v", site, err)
	}
	site[0].Tags[0] = "mutated"
	stored, err := repository.Get(context.Background(), tenantA, "b")
	if err != nil || stored.Tags[0] != "gate" {
		t.Fatalf("repository state leaked: %+v %v", stored, err)
	}
	if _, err := repository.Get(context.Background(), tenantA, "retired"); !errors.Is(err, ErrCameraNotFound) {
		t.Fatalf("retired camera must be hidden, got %v", err)
	}
}

func TestMemoryRepositoryCreatesReplaysAndEnforcesTenantSerials(t *testing.T) {
	repository := NewMemoryRepository(nil)
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixed }
	command := CreateCameraCommand{
		TenantID: tenantA, ActorID: "user-1", RequestID: "request-1", IdempotencyKey: "camera-create-1",
		SiteID: siteA, SerialNumber: " sn-001 ", Name: " Front gate ", GroupName: " Perimeter ", Tags: []string{" Gate ", "gate", "LPR"},
	}
	created, err := repository.Create(context.Background(), command)
	if err != nil || created.Replayed || created.Camera.SerialNumber != "SN-001" || created.Camera.Name != "Front gate" || created.Camera.LifecycleStatus != "pending" || created.Camera.ConfigVersion != 1 || created.Camera.CreatedAt != fixed {
		t.Fatalf("unexpected create: %+v %v", created, err)
	}
	if len(created.Camera.Tags) != 2 || created.Camera.Tags[0] != "gate" || created.Camera.Tags[1] != "lpr" {
		t.Fatalf("tags were not normalized: %+v", created.Camera.Tags)
	}
	replayed, err := repository.Create(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.Camera.ID != created.Camera.ID {
		t.Fatalf("unexpected replay: %+v %v", replayed, err)
	}
	command.Name = "Different"
	if _, err := repository.Create(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	command.IdempotencyKey = "camera-create-2"
	if _, err := repository.Create(context.Background(), command); !errors.Is(err, ErrSerialConflict) {
		t.Fatalf("expected serial conflict, got %v", err)
	}
	command.TenantID = tenantB
	command.IdempotencyKey = "camera-create-3"
	if _, err := repository.Create(context.Background(), command); err != nil {
		t.Fatalf("serial must only be unique inside a tenant: %v", err)
	}
}

func TestMemoryRepositoryUpdatesVersionAndLifecycle(t *testing.T) {
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Camera{{
		ID: cameraA, TenantID: tenantA, SiteID: siteA, Name: "Gate", Tags: []string{"gate"},
		LifecycleStatus: "pending", ConfigVersion: 1, CreatedAt: fixed, UpdatedAt: fixed,
	}})
	repository.now = func() time.Time { return fixed.Add(time.Minute) }
	name, group, status := " Gate north ", " Perimeter ", "active"
	tags := []string{"north", "gate", "north"}
	updated, err := repository.Update(context.Background(), UpdateCameraCommand{
		TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 1, Name: &name, GroupName: &group, Tags: &tags, LifecycleStatus: &status,
	})
	if err != nil || updated.ConfigVersion != 2 || updated.Name != "Gate north" || updated.GroupName != "Perimeter" || updated.LifecycleStatus != "active" || updated.UpdatedAt != fixed.Add(time.Minute) {
		t.Fatalf("unexpected update: %+v %v", updated, err)
	}
	if _, err := repository.Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 1, Name: &name}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	invalid := "pending"
	if _, err := repository.Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 2, LifecycleStatus: &invalid}); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("expected lifecycle conflict, got %v", err)
	}
	unchanged, err := repository.Update(context.Background(), UpdateCameraCommand{TenantID: tenantA, CameraID: cameraA, ExpectedVersion: 2, Name: &updated.Name})
	if err != nil || unchanged.ConfigVersion != 2 {
		t.Fatalf("no-op update must preserve version: %+v %v", unchanged, err)
	}
}

func TestMemoryRepositoryRetiresIdempotently(t *testing.T) {
	repository := NewMemoryRepository([]Camera{{ID: cameraA, TenantID: tenantA, SiteID: siteA, LifecycleStatus: "active", ConfigVersion: 4}})
	retired, err := repository.Retire(context.Background(), RetireCameraCommand{TenantID: tenantA, CameraID: cameraA})
	if err != nil || retired.LifecycleStatus != "retired" || retired.ConfigVersion != 5 {
		t.Fatalf("unexpected retirement: %+v %v", retired, err)
	}
	replayed, err := repository.Retire(context.Background(), RetireCameraCommand{TenantID: tenantA, CameraID: cameraA})
	if err != nil || replayed.ConfigVersion != 5 {
		t.Fatalf("retirement replay changed state: %+v %v", replayed, err)
	}
	if _, err := repository.Retire(context.Background(), tenantRetire(tenantB)); !errors.Is(err, ErrCameraNotFound) {
		t.Fatalf("cross-tenant retirement must not find camera: %v", err)
	}
}

func tenantRetire(tenantID string) RetireCameraCommand {
	return RetireCameraCommand{TenantID: tenantID, CameraID: cameraA}
}

func TestLifecycleTransitions(t *testing.T) {
	allowed := [][2]string{{"pending", "pending"}, {"pending", "active"}, {"active", "offline"}, {"offline", "active"}, {"offline", "retired"}}
	for _, transition := range allowed {
		if !canTransition(transition[0], transition[1]) {
			t.Fatalf("expected transition %v", transition)
		}
	}
	for _, transition := range [][2]string{{"pending", "offline"}, {"active", "pending"}, {"retired", "active"}, {"unknown", "active"}} {
		if canTransition(transition[0], transition[1]) {
			t.Fatalf("unexpected transition %v", transition)
		}
	}
}
