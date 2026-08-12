// Package tenant defines tenant-scoped domain resources and persistence ports.
package tenant

import (
	"context"
	"sort"
)

// Site is a facility owned by exactly one tenant.
type Site struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// Repository always receives the verified tenant claim as its partition key.
type Repository interface {
	ListSites(context.Context, string) ([]Site, error)
}

// MemoryRepository is for deterministic tests and local bootstrap only. A
// Postgres/RLS implementation replaces it before persisted tenant CRUD ships.
type MemoryRepository struct {
	sites []Site
}

// NewMemoryRepository copies seed data so callers cannot mutate repository state.
func NewMemoryRepository(sites []Site) *MemoryRepository {
	return &MemoryRepository{sites: append([]Site(nil), sites...)}
}

// ListSites returns only rows whose tenant key matches the verified claim.
func (r *MemoryRepository) ListSites(_ context.Context, tenantID string) ([]Site, error) {
	result := make([]Site, 0)
	for _, site := range r.sites {
		if site.TenantID == tenantID {
			result = append(result, site)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
