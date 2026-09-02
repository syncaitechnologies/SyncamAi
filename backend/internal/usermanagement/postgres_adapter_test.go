package usermanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

type failingPostgresPool struct{ err error }

func (p failingPostgresPool) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, p.err
}

func TestPostgresAdapterQueuesInvitationIntentAndAudit(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("INSERT INTO identity.lifecycle_delivery_requests").WithArgs(pgxmock.AnyArg(), userTenant, userRequest, pgxmock.AnyArg(), "actor-1").WillReturnRows(pgxmock.NewRows([]string{"id", "payload", "created"}).AddRow("99999999-9999-4999-8999-999999999999", []byte(`{"email":"new.user@example.test"}`), true))
	pool.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT record_hash").WithArgs(userTenant, pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	pool.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), userTenant, pgxmock.AnyArg(), pgxmock.AnyArg(), "actor-1", "identity.invitation.queued", "lifecycle_delivery_request", pgxmock.AnyArg(), userRequest, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectCommit()
	invite, err := NewPostgresAdapter(pool).Invite(context.Background(), InviteRequest{InviteCommand: inviteCommand(), ActorID: "actor-1"})
	if err != nil || invite.ID == "" || invite.Email != "new.user@example.test" || !invite.Queued { t.Fatalf("queue invitation: %#v %v", invite, err) }
	if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}

func TestPostgresAdapterReturnsExistingInvitationWithoutAnotherAudit(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("INSERT INTO identity.lifecycle_delivery_requests").WithArgs(pgxmock.AnyArg(), userTenant, userRequest, pgxmock.AnyArg(), "actor-1").WillReturnRows(pgxmock.NewRows([]string{"id", "payload", "created"}).AddRow("99999999-9999-4999-8999-999999999999", []byte(`{"email":"new.user@example.test"}`), false))
	pool.ExpectCommit()

	invite, err := NewPostgresAdapter(pool).Invite(context.Background(), InviteRequest{InviteCommand: inviteCommand(), ActorID: "actor-1"})
	if err != nil || invite.ID != "99999999-9999-4999-8999-999999999999" || !invite.Queued {
		t.Fatalf("return existing invitation: %#v %v", invite, err)
	}
	if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}

func TestPostgresAdapterRejectsReusedRequestIDWithDifferentPayload(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("INSERT INTO identity.lifecycle_delivery_requests").WithArgs(pgxmock.AnyArg(), userTenant, userRequest, pgxmock.AnyArg(), "actor-1").WillReturnRows(pgxmock.NewRows([]string{"id", "payload", "created"}).AddRow("99999999-9999-4999-8999-999999999999", []byte(`{"email":"other.user@example.test"}`), false))
	pool.ExpectRollback()

	_, err = NewPostgresAdapter(pool).Invite(context.Background(), InviteRequest{InviteCommand: inviteCommand(), ActorID: "actor-1"})
	if err == nil || err.Error() != "invitation request identifier conflicts with a different payload" {
		t.Fatalf("reuse conflict error = %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}

func TestPostgresAdapterRejectsUnavailableAndBeginFailure(t *testing.T) {
	if _, err := NewPostgresAdapter(nil).Invite(context.Background(), InviteRequest{}); !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("nil pool error = %v", err)
	}
	beginErr := errors.New("database unavailable")
	if _, err := NewPostgresAdapter(failingPostgresPool{err: beginErr}).Invite(context.Background(), InviteRequest{}); !errors.Is(err, beginErr) {
		t.Fatalf("begin error = %v", err)
	}
}
