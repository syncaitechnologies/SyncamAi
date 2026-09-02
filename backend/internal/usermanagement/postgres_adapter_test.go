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

func expectDisableAudit(pool pgxmock.PgxPool) {
	pool.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectQuery("SELECT record_hash").WithArgs(userTenant, pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	pool.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), userTenant, pgxmock.AnyArg(), pgxmock.AnyArg(), "actor-1", "identity.user.disable.queued", "user_tenant_membership", userID, userRequest, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func expectDisableStart(t *testing.T) pgxmock.PgxPool {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil { t.Fatal(err) }
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	pool.ExpectExec("SELECT set_config").WithArgs(userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	pool.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("syncam-user-disable:"+userTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	return pool
}

func TestPostgresAdapterSuspendsMembershipsAndQueuesDisablement(t *testing.T) {
	pool := expectDisableStart(t)
	defer pool.Close()
	pool.ExpectQuery("SELECT action").WithArgs(userTenant, userRequest).WillReturnError(pgx.ErrNoRows)
	pool.ExpectQuery("SELECT roles FROM identity.user_tenant_memberships").WithArgs(userTenant, userID).WillReturnRows(pgxmock.NewRows([]string{"roles"}).AddRow([]string{"site_admin"}))
	pool.ExpectExec("UPDATE identity.user_tenant_memberships").WithArgs(userTenant, userID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE identity.user_site_memberships").WithArgs(userTenant, userID).WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectQuery("INSERT INTO identity.lifecycle_delivery_requests").WithArgs(pgxmock.AnyArg(), userTenant, userRequest, userID, "actor-1").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("99999999-9999-4999-8999-999999999999"))
	expectDisableAudit(pool)
	pool.ExpectCommit()
	if err := NewPostgresAdapter(pool).Disable(context.Background(), DisableRequest{DisableCommand: disableCommand(), ActorID: "actor-1"}); err != nil { t.Fatalf("disable: %v", err) }
	if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}

func TestPostgresAdapterReturnsExistingDisablementAndRejectsUnsafeTransitions(t *testing.T) {
	t.Run("idempotent request", func(t *testing.T) {
		pool := expectDisableStart(t)
		defer pool.Close()
		pool.ExpectQuery("SELECT action").WithArgs(userTenant, userRequest).WillReturnRows(pgxmock.NewRows([]string{"action", "target_user_id"}).AddRow("disable", userID))
		pool.ExpectCommit()
		if err := NewPostgresAdapter(pool).Disable(context.Background(), DisableRequest{DisableCommand: disableCommand(), ActorID: "actor-1"}); err != nil { t.Fatalf("existing disable: %v", err) }
		if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
	})
	t.Run("request target conflict", func(t *testing.T) {
		pool := expectDisableStart(t)
		defer pool.Close()
		pool.ExpectQuery("SELECT action").WithArgs(userTenant, userRequest).WillReturnRows(pgxmock.NewRows([]string{"action", "target_user_id"}).AddRow("disable", secondSiteID))
		pool.ExpectRollback()
		err := NewPostgresAdapter(pool).Disable(context.Background(), DisableRequest{DisableCommand: disableCommand(), ActorID: "actor-1"})
		if !errors.Is(err, ErrDisableRequestConflict) { t.Fatalf("conflicting target: %v", err) }
		if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
	})
	t.Run("self disable", func(t *testing.T) {
		request := DisableRequest{DisableCommand: disableCommand(), ActorID: userID}
		if err := NewPostgresAdapter(nil).Disable(context.Background(), request); !errors.Is(err, ErrLifecycleUnavailable) { t.Fatalf("nil adapter: %v", err) }
		pool, err := pgxmock.NewPool()
		if err != nil { t.Fatal(err) }
		defer pool.Close()
		if err := NewPostgresAdapter(pool).Disable(context.Background(), request); !errors.Is(err, ErrCannotDisableSelf) { t.Fatalf("self disable: %v", err) }
	})
	t.Run("inactive target", func(t *testing.T) {
		pool := expectDisableStart(t)
		defer pool.Close()
		pool.ExpectQuery("SELECT action").WithArgs(userTenant, userRequest).WillReturnError(pgx.ErrNoRows)
		pool.ExpectQuery("SELECT roles FROM identity.user_tenant_memberships").WithArgs(userTenant, userID).WillReturnError(pgx.ErrNoRows)
		pool.ExpectRollback()
		err := NewPostgresAdapter(pool).Disable(context.Background(), DisableRequest{DisableCommand: disableCommand(), ActorID: "actor-1"})
		if !errors.Is(err, ErrDisableTargetInactive) { t.Fatalf("inactive target: %v", err) }
		if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
	})
	t.Run("last super admin", func(t *testing.T) {
		pool := expectDisableStart(t)
		defer pool.Close()
		pool.ExpectQuery("SELECT action").WithArgs(userTenant, userRequest).WillReturnError(pgx.ErrNoRows)
		pool.ExpectQuery("SELECT roles FROM identity.user_tenant_memberships").WithArgs(userTenant, userID).WillReturnRows(pgxmock.NewRows([]string{"roles"}).AddRow([]string{"super_admin"}))
		pool.ExpectQuery("SELECT count\\(\\*\\) FROM identity.user_tenant_memberships").WithArgs(userTenant).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
		pool.ExpectRollback()
		err := NewPostgresAdapter(pool).Disable(context.Background(), DisableRequest{DisableCommand: disableCommand(), ActorID: "actor-1"})
		if !errors.Is(err, ErrLastActiveSuperAdmin) { t.Fatalf("last super admin: %v", err) }
		if err := pool.ExpectationsWereMet(); err != nil { t.Fatal(err) }
	})
}
