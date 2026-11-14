package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestPendingTenantsUsesNarrowFunction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	store := NewPostgresStore(mock)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("pending_outbox_tenants").WillReturnRows(pgxmock.NewRows([]string{"tenant_id"}).AddRow("a").AddRow("b"))
	mock.ExpectCommit()
	tenants, err := store.PendingTenants(context.Background())
	if err != nil || len(tenants) != 2 {
		t.Fatalf("tenants: %v %v", tenants, err)
	}
	failure := errors.New("down")
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("pending_outbox_tenants").WillReturnError(failure)
	mock.ExpectRollback()
	if _, err := store.PendingTenants(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("failure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	var missing *PostgresStore
	if _, err := missing.PendingTenants(context.Background()); err == nil {
		t.Fatal("nil store must fail")
	}
}

type tenantListerFunc func(context.Context) ([]string, error)

func (f tenantListerFunc) PendingTenants(ctx context.Context) ([]string, error) { return f(ctx) }

type tenantStore struct {
	mu      sync.Mutex
	claimed map[string]int
}

func (s *tenantStore) Claim(_ context.Context, tenantID, _ string, _ int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed[tenantID]++
	if tenantID == "broken" {
		return nil, errors.New("claim failed")
	}
	return []Message{{MessageID: tenantID + "-m", TenantID: tenantID}}, nil
}
func (s *tenantStore) MarkPublished(context.Context, string, string, string) error { return nil }
func (s *tenantStore) MarkFailed(context.Context, string, string, string, string) error {
	return nil
}

func TestRunAllTenantsDispatchesEveryPendingTenant(t *testing.T) {
	store := &tenantStore{claimed: map[string]int{}}
	var published []string
	var mu sync.Mutex
	dispatcher := Dispatcher{Store: store, Publisher: publisherFunc(func(_ context.Context, message Message) error {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, message.TenantID)
		return nil
	}), WorkerID: "w"}
	calls := 0
	lister := tenantListerFunc(func(context.Context) ([]string, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("list failed")
		}
		return []string{"tenant-a", "tenant-b", "broken"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	var logs []string
	RunAllTenants(ctx, dispatcher, lister, 10*time.Millisecond, func(format string, _ ...any) { logs = append(logs, format) })
	mu.Lock()
	defer mu.Unlock()
	if store.claimed["tenant-a"] == 0 || store.claimed["tenant-b"] == 0 || store.claimed["broken"] == 0 || len(published) == 0 {
		t.Fatalf("not every tenant was dispatched: %+v %v", store.claimed, published)
	}
	if len(logs) == 0 {
		t.Fatal("failures and progress must be logged")
	}
}
