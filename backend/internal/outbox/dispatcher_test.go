package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const (
	tenantID  = "11111111-1111-4111-8111-111111111111"
	workerID  = "22222222-2222-4222-8222-222222222222"
	messageID = "33333333-3333-4333-8333-333333333333"
)

func TestPostgresStoreClaimsAndCompletesLease(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	occurredAt := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("WITH candidates AS").WithArgs(tenantID, 25, workerID).WillReturnRows(
		pgxmock.NewRows([]string{"message_id", "tenant_id", "topic", "partition_key", "payload", "headers", "occurred_at"}).
			AddRow(messageID, tenantID, "detection-events-v1", "partition", []byte(`{"event_id":"event"}`), []byte(`{}`), occurredAt),
	)
	mock.ExpectCommit()
	store := NewPostgresStore(mock)
	messages, err := store.Claim(context.Background(), tenantID, workerID, 25)
	if err != nil || len(messages) != 1 || messages[0].MessageID != messageID {
		t.Fatalf("unexpected claim: %+v %v", messages, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("SELECT set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("UPDATE messaging.outbox_messages").WithArgs(tenantID, messageID, workerID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()
	if err := store.MarkPublished(context.Background(), tenantID, workerID, messageID); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type memoryStore struct {
	messages          []Message
	published, failed int
}

func (s *memoryStore) Claim(context.Context, string, string, int) ([]Message, error) {
	return s.messages, nil
}
func (s *memoryStore) MarkPublished(context.Context, string, string, string) error {
	s.published++
	return nil
}
func (s *memoryStore) MarkFailed(context.Context, string, string, string, string) error {
	s.failed++
	return nil
}

type publisherFunc func(context.Context, Message) error

func (f publisherFunc) Publish(ctx context.Context, message Message) error { return f(ctx, message) }

func TestDispatcherMarksSuccessAndFailure(t *testing.T) {
	store := &memoryStore{messages: []Message{{MessageID: "ok"}, {MessageID: "bad"}}}
	dispatcher := Dispatcher{Store: store, WorkerID: workerID, Publisher: publisherFunc(func(_ context.Context, message Message) error {
		if message.MessageID == "bad" {
			return errors.New("publish failed")
		}
		return nil
	})}
	result, err := dispatcher.DispatchTenant(context.Background(), tenantID)
	if err == nil || result.Claimed != 2 || result.Published != 1 || result.Failed != 1 || store.published != 1 || store.failed != 1 {
		t.Fatalf("unexpected dispatch: %+v published=%d failed=%d err=%v", result, store.published, store.failed, err)
	}
	if _, err := (Dispatcher{}).DispatchTenant(context.Background(), tenantID); err == nil {
		t.Fatal("missing dependencies must fail")
	}
}

func TestPostgresStoreRejectsInvalidBoundaries(t *testing.T) {
	store := NewPostgresStore(nil)
	if _, err := store.Claim(context.Background(), tenantID, workerID, 25); err == nil {
		t.Fatal("missing pool must fail")
	}
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	store = NewPostgresStore(mock)
	for _, input := range []struct {
		tenant, worker string
		limit          int
	}{{"bad", workerID, 25}, {tenantID, "bad", 25}, {tenantID, workerID, 0}, {tenantID, workerID, 101}} {
		if _, err := store.Claim(context.Background(), input.tenant, input.worker, input.limit); err == nil {
			t.Fatal("invalid claim accepted")
		}
	}
	if err := store.MarkPublished(context.Background(), tenantID, "bad", messageID); err == nil {
		t.Fatal("invalid worker accepted")
	}
	if err := store.MarkPublished(context.Background(), tenantID, workerID, "bad"); err == nil {
		t.Fatal("invalid message accepted")
	}
}
