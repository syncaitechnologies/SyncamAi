package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryRepositoryFiltersTenantAndSorts(t *testing.T) {
	repository := NewMemoryRepository([]Site{
		{ID: "site-b", TenantID: "tenant-a", Name: "B"},
		{ID: "site-x", TenantID: "tenant-b", Name: "X"},
		{ID: "site-a", TenantID: "tenant-a", Name: "A"},
	})
	sites, err := repository.ListSites(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].ID != "site-a" || sites[1].ID != "site-b" {
		t.Fatalf("unexpected sites: %+v", sites)
	}
}

func TestMemoryRepositoryCreatesAndReplaysSites(t *testing.T) {
	repository := NewMemoryRepository(nil)
	command := CreateSiteCommand{
		TenantID: "tenant-a", ActorID: "user-a", RequestID: "request-a",
		IdempotencyKey: "create-a", Name: " Pilot ", Address: " Pune ", Timezone: "Asia/Kolkata",
	}
	created, err := repository.CreateSite(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if created.Replayed || created.Site.Name != "Pilot" || created.Site.Address != "Pune" {
		t.Fatalf("unexpected create result: %+v", created)
	}
	replayed, err := repository.CreateSite(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Site != created.Site {
		t.Fatalf("unexpected replay result: %+v", replayed)
	}
	command.Name = "Different"
	if _, err := repository.CreateSite(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}
