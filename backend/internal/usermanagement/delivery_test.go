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
	pool.ExpectQuery("WITH candidates AS").WithArgs(userTenant, 25, lifecycleWorkerID, []string{invitationDeliveryAction}).WillReturnRows(
		pgxmock.NewRows([]string{"id", "tenant_id", "request_id", "action", "target_user_id", "payload", "provider_operation_id"}).
			AddRow(lifecycleRequestID, userTenant, userRequest, invitationDeliveryAction, "", []byte(`{"email":"new.user@example.test"}`), "lifecycle:"+lifecycleRequestID),
	)
	pool.ExpectCommit()

	store := NewPostgresDeliveryStore(pool)
	requests, err := store.Claim(context.Background(), userTenant, lifecycleWorkerID, 25, []string{invitationDeliveryAction})
	if err != nil || len(requests) != 1 || requests[0].TargetUserID != "" || requests[0].ProviderOperationID != "lifecycle:"+lifecycleRequestID {
		t.Fatalf("claim lifecycle delivery: %#v %v", requests, err)
	}

	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("UPDATE identity.lifecycle_delivery_requests").WithArgs(userTenant, lifecycleRequestID, lifecycleWorkerID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	if err := store.MarkDelivered(context.Background(), userTenant, lifecycleWorkerID, lifecycleRequestID); err != nil {
		t.Fatal(err)
	}

	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("UPDATE identity.lifecycle_delivery_requests").WithArgs(userTenant, lifecycleRequestID, lifecycleWorkerID, "provider delivery failed").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	if err := store.MarkFailed(context.Background(), userTenant, lifecycleWorkerID, lifecycleRequestID, ""); err != nil {
		t.Fatal(err)
	}

	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("UPDATE identity.lifecycle_delivery_requests").WithArgs(userTenant, lifecycleRequestID, lifecycleWorkerID, "Supabase invitation result requires reconciliation").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	if err := store.MarkReconciliationRequired(context.Background(), userTenant, lifecycleWorkerID, lifecycleRequestID, "Supabase invitation result requires reconciliation"); err != nil {
		t.Fatal(err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresDeliveryStorePreservesTargetUserID(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("WITH candidates AS").WithArgs(userTenant, 1, lifecycleWorkerID, []string{disablementDeliveryAction}).WillReturnRows(
		pgxmock.NewRows([]string{"id", "tenant_id", "request_id", "action", "target_user_id", "payload", "provider_operation_id"}).
			AddRow(lifecycleRequestID, userTenant, userRequest, disablementDeliveryAction, userID, []byte(`{}`), "lifecycle:"+lifecycleRequestID),
	)
	pool.ExpectCommit()

	requests, err := NewPostgresDeliveryStore(pool).Claim(context.Background(), userTenant, lifecycleWorkerID, 1, []string{disablementDeliveryAction})
	if err != nil || len(requests) != 1 || requests[0].TargetUserID != userID {
		t.Fatalf("claim lifecycle disablement target: %#v %v", requests, err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type memoryDeliveryStore struct {
	requests  []DeliveryRequest
	delivered []string
	failed    []string
	reconcile []string
}

func (s *memoryDeliveryStore) Claim(context.Context, string, string, int, []string) ([]DeliveryRequest, error) {
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
func (s *memoryDeliveryStore) MarkReconciliationRequired(_ context.Context, _ string, _ string, requestID, reason string) error {
	s.reconcile = append(s.reconcile, requestID+":"+reason)
	return nil
}

type invitationProviderFunc func(context.Context, DeliveryRequest) error

func (invitationProviderFunc) DeliveryActions() []string { return []string{invitationDeliveryAction} }
func (f invitationProviderFunc) Deliver(ctx context.Context, request DeliveryRequest) error {
	return f(ctx, request)
}

type deliveryProviderFunc struct {
	actions []string
	deliver func(context.Context, DeliveryRequest) error
}

func (p deliveryProviderFunc) DeliveryActions() []string { return p.actions }
func (p deliveryProviderFunc) Deliver(ctx context.Context, request DeliveryRequest) error {
	return p.deliver(ctx, request)
}

type invalidDeliveryProvider []string

func (p invalidDeliveryProvider) DeliveryActions() []string { return []string(p) }
func (invalidDeliveryProvider) Deliver(context.Context, DeliveryRequest) error {
	return nil
}

type unrecognizedReconciliationError struct{}

func (unrecognizedReconciliationError) Error() string { return "unrecognized provider reconciliation detail" }
func (unrecognizedReconciliationError) SafeReconciliationReason() string {
	return "unrecognized provider reconciliation detail"
}

func TestDeliveryWorkerMarksDeliveryAndSafeFailure(t *testing.T) {
	store := &memoryDeliveryStore{requests: []DeliveryRequest{
		{ID: "ok", Action: invitationDeliveryAction, ProviderOperationID: "lifecycle:ok"},
		{ID: "bad", Action: invitationDeliveryAction, ProviderOperationID: "lifecycle:bad"},
		{ID: "unsupported", Action: "disable"},
		{ID: "reconcile", Action: invitationDeliveryAction},
	}}
	worker := DeliveryWorker{Store: store, WorkerID: lifecycleWorkerID, Provider: invitationProviderFunc(func(_ context.Context, request DeliveryRequest) error {
		if request.Action != invitationDeliveryAction {
			return errors.New("unsupported lifecycle delivery action")
		}
		if request.ID == "bad" {
			return errors.New("the provider returned a token")
		}
		if request.ID == "reconcile" {
			return supabaseReconciliationRequiredError{}
		}
		return nil
	})}
	result, err := worker.DispatchTenant(context.Background(), userTenant)
	if err == nil || result.Claimed != 4 || result.Delivered != 1 || result.Failed != 2 || result.ReconciliationRequired != 1 {
		t.Fatalf("dispatch lifecycle delivery: %#v %v", result, err)
	}
	if len(store.delivered) != 1 || store.delivered[0] != "ok" {
		t.Fatalf("delivered = %#v", store.delivered)
	}
	if len(store.failed) != 2 || store.failed[0] != "bad:provider delivery failed" {
		t.Fatalf("failed = %#v", store.failed)
	}
	if len(store.reconcile) != 1 || store.reconcile[0] != "reconcile:Supabase invitation result requires reconciliation" {
		t.Fatalf("reconciliation holds = %#v", store.reconcile)
	}
	if _, err := (DeliveryWorker{}).DispatchTenant(context.Background(), userTenant); err == nil {
		t.Fatal("missing worker dependencies must fail")
	}
}

func TestPostgresDeliveryStoreRejectsInvalidBoundary(t *testing.T) {
	store := NewPostgresDeliveryStore(nil)
	if _, err := store.Claim(context.Background(), userTenant, lifecycleWorkerID, 25, []string{invitationDeliveryAction}); err == nil {
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
		actions        []string
	}{
		{"bad", lifecycleWorkerID, 25, []string{invitationDeliveryAction}},
		{userTenant, "bad", 25, []string{invitationDeliveryAction}},
		{userTenant, lifecycleWorkerID, 0, []string{invitationDeliveryAction}},
		{userTenant, lifecycleWorkerID, 101, []string{invitationDeliveryAction}},
		{userTenant, lifecycleWorkerID, 25, nil},
		{userTenant, lifecycleWorkerID, 25, []string{"unknown"}},
		{userTenant, lifecycleWorkerID, 25, []string{invitationDeliveryAction, invitationDeliveryAction}},
	} {
		if _, err := store.Claim(context.Background(), input.tenant, input.worker, input.limit, input.actions); err == nil {
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

func TestDeliveryWorkerRejectsInvalidProviderActionDeclarations(t *testing.T) {
	for _, actions := range [][]string{nil, {"unknown"}, {invitationDeliveryAction, invitationDeliveryAction}} {
		worker := DeliveryWorker{
			Store:    &memoryDeliveryStore{},
			Provider: invalidDeliveryProvider(actions),
			WorkerID: lifecycleWorkerID,
		}
		if _, err := worker.DispatchTenant(context.Background(), userTenant); err == nil {
			t.Fatalf("invalid provider action declaration accepted: %#v", actions)
		}
	}
}

func TestDeliveryWorkerRejectsInvalidOrUnclaimedTargetsBeforeProvider(t *testing.T) {
	store := &memoryDeliveryStore{requests: []DeliveryRequest{
		{ID: "invite-target", Action: invitationDeliveryAction, TargetUserID: userID},
		{ID: "disable-missing-target", Action: disablementDeliveryAction},
		{ID: "valid-disable", Action: disablementDeliveryAction, TargetUserID: userID},
	}}
	providerCalls := 0
	worker := DeliveryWorker{
		Store:    store,
		WorkerID: lifecycleWorkerID,
		Provider: deliveryProviderFunc{
			actions: []string{invitationDeliveryAction, disablementDeliveryAction},
			deliver: func(context.Context, DeliveryRequest) error {
				providerCalls++
				return nil
			},
		},
	}
	result, err := worker.DispatchTenant(context.Background(), userTenant)
	if err == nil || result.Claimed != 3 || result.Delivered != 1 || result.Failed != 2 || providerCalls != 1 {
		t.Fatalf("dispatch invalid lifecycle targets: %#v %v calls=%d", result, err, providerCalls)
	}
	if len(store.failed) != 2 || store.failed[0] != "invite-target:provider delivery failed" || store.failed[1] != "disable-missing-target:provider delivery failed" {
		t.Fatalf("failed = %#v", store.failed)
	}

	store = &memoryDeliveryStore{requests: []DeliveryRequest{{ID: "unclaimed-disable", Action: disablementDeliveryAction, TargetUserID: userID}}}
	worker.Store = store
	worker.Provider = deliveryProviderFunc{actions: []string{invitationDeliveryAction}, deliver: func(context.Context, DeliveryRequest) error {
		providerCalls++
		return nil
	}}
	if _, err := worker.DispatchTenant(context.Background(), userTenant); err == nil || providerCalls != 1 || len(store.failed) != 1 || store.failed[0] != "unclaimed-disable:provider delivery failed" {
		t.Fatalf("unclaimed lifecycle action reached provider: calls=%d failures=%#v", providerCalls, store.failed)
	}
}

func TestDeliveryWorkerRecordsOnlyAllowlistedReconciliationReasons(t *testing.T) {
	store := &memoryDeliveryStore{requests: []DeliveryRequest{{ID: "unknown-reason", Action: invitationDeliveryAction}}}
	worker := DeliveryWorker{
		Store:    store,
		WorkerID: lifecycleWorkerID,
		Provider: deliveryProviderFunc{
			actions: []string{invitationDeliveryAction},
			deliver: func(context.Context, DeliveryRequest) error {
				return unrecognizedReconciliationError{}
			},
		},
	}
	result, err := worker.DispatchTenant(context.Background(), userTenant)
	if err == nil || result.ReconciliationRequired != 1 || len(store.reconcile) != 1 || store.reconcile[0] != "unknown-reason:"+genericDeliveryReconciliationRequiredReason {
		t.Fatalf("reconciliation reason was not safely bounded: %#v %v %#v", result, err, store.reconcile)
	}
}
