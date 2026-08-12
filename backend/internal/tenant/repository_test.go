package tenant

import (
	"context"
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
