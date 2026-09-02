package usermanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const lifecycleWorkerID = "66666666-6666-4666-8666-666666666666"
const lifecycleRequestID = "77777777-7777-4777-8777-777777777777"

func TestPostgresDeliveryStoreClaimsAndCompletesLease(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("WITH candidates AS").WithArgs(userTenant, 25, lifecycleWorkerID).WillReturnRows(
		pgxmock.NewRows([]string{"id", "tenant_id", "request_id", "action", "payload", "provider_operation_id"}).
			AddRow(lifecycleRequestID, userTenant, userRequest, invitationDeliveryAction, []byte(`{"email":"new.user@example.test"}`), "lifecycle:"+lifecycleRequestID),
	)
	pool.ExpectCommit()

	store := NewPostgresDeliveryStore(pool)
	requests, err := store.Claim(context.Background(), userTenant, lifecycleWorkerID, 25)
	if err != nil || len(requests) != 1 || requests[0].ProviderOperationID != "lifecycle:"+lifecycleRequestID {
		t.Fatalf("claim lifecycle delivery: %#v %v", requests, err)
	}

	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("UPDATE identity.lifecycle_delivery_requests").WithArgs(userTenant, lifecycleRequestID, lifecycleWorkerID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	if err := store.MarkDelivered(context.Background(), userTenant, lifecycleWorkerID, lifecycleRequestID); err != nil {
		t.Fatal(err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type memoryDeliveryStore struct {
	requests  []DeliveryRequest
	delivered []string
	failed    []string
}

func (s *memoryDeliveryStore) Claim(context.Context, string, string, int) ([]DeliveryRequest, error) {
	return s.requests, nil
}
func (s *memoryDeliveryStore) MarkDelivered(_ context.Context, _ string, _ string, requestID string) error {
	s.delivered = append(s.delivered, requestID)
	return nil
}
func (s *memoryDeliveryStore) MarkFailed(_ context.Context, _ string, _ string, requestID, failure string) error {
	s.failed = append(s.failed, requestID+":"+failure)
	return nil
}

type invitationProviderFunc func(context.Context, DeliveryRequest) error

func (f invitationProviderFunc) DeliverInvitation(ctx context.Context, request DeliveryRequest) error {
	return f(ctx, request)
}

func TestDeliveryWorkerMarksDeliveryAndSafeFailure(t *testing.T) {
	store := &memoryDeliveryStore{requests: []DeliveryRequest{
		{ID: "ok", Action: invitationDeliveryAction, ProviderOperationID: "lifecycle:ok"},
		{ID: "bad", Action: invitationDeliveryAction, ProviderOperationID: "lifecycle:bad"},
		{ID: "unsupported", Action: "disable"},
	}}
	worker := DeliveryWorker{Store: store, WorkerID: lifecycleWorkerID, Provider: invitationProviderFunc(func(_ context.Context, request DeliveryRequest) error {
		if request.ID == "bad" {
			return errors.New("the provider returned a token")
		}
		return nil
	})}
	result, err := worker.DispatchTenant(context.Background(), userTenant)
	if err == nil || result.Claimed != 3 || result.Delivered != 1 || result.Failed != 2 {
		t.Fatalf("dispatch lifecycle delivery: %#v %v", result, err)
	}
	if len(store.delivered) != 1 || store.delivered[0] != "ok" {
		t.Fatalf("delivered = %#v", store.delivered)
	}
	if len(store.failed) != 2 || store.failed[0] != "bad:provider delivery failed" {
		t.Fatalf("failed = %#v", store.failed)
	}
	if _, err := (DeliveryWorker{}).DispatchTenant(context.Background(), userTenant); err == nil {
		t.Fatal("missing worker dependencies must fail")
	}
}

func TestPostgresDeliveryStoreRejectsInvalidBoundary(t *testing.T) {
	store := NewPostgresDeliveryStore(nil)
	if _, err := store.Claim(context.Background(), userTenant, lifecycleWorkerID, 25); err == nil {
		t.Fatal("missing pool must fail")
	}
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store = NewPostgresDeliveryStore(pool)
	for _, input := range []struct {
		tenant, worker string
		limit          int
	}{
		{"bad", lifecycleWorkerID, 25},
		{userTenant, "bad", 25},
		{userTenant, lifecycleWorkerID, 0},
		{userTenant, lifecycleWorkerID, 101},
	} {
		if _, err := store.Claim(context.Background(), input.tenant, input.worker, input.limit); err == nil {
			t.Fatal("invalid lifecycle delivery claim accepted")
		}
	}
	if err := store.MarkDelivered(context.Background(), userTenant, "bad", lifecycleRequestID); err == nil {
		t.Fatal("invalid worker accepted")
	}
	if err := store.MarkDelivered(context.Background(), userTenant, lifecycleWorkerID, "bad"); err == nil {
		t.Fatal("invalid request accepted")
	}
}
