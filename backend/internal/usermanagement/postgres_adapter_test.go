package usermanagement

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func TestPostgresAdapterQueuesInvitationIntentAndAudit(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("INSERT INTO identity.lifecycle_delivery_requests").WithArgs(pgxmock.AnyArg(), userTenant, userRequest, pgxmock.AnyArg(), "actor-1").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT record_hash").WithArgs(userTenant, pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	pool.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), userTenant, pgxmock.AnyArg(), pgxmock.AnyArg(), "actor-1", "identity.invitation.queued", "lifecycle_delivery_request", pgxmock.AnyArg(), userRequest, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectCommit()
	invite, err := NewPostgresAdapter(pool).Invite(context.Background(), InviteRequest{InviteCommand: inviteCommand(), ActorID: "actor-1"})
	if err != nil || invite.ID == "" || invite.Email != "new.user@example.test" { t.Fatalf("queue invitation: %#v %v", invite, err) }
	if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}
