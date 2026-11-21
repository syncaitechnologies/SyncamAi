package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

const unitOperatorID = "88888888-8888-4888-8888-888888888888"

func TestPostgresAdminListsAndUpdatesWorkspaces(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	created := time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC)
	minutes := int32(10)
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("admin_list_workspaces").WithArgs(unitOperatorID).WillReturnRows(pgxmock.NewRows(
		[]string{"tenant_id", "name", "slug", "plan", "trial_ends_at", "site_limit", "camera_limit", "live_view_minutes", "live_view_seconds", "sites", "cameras", "created_at"}).
		AddRow(unitTenantID, "Client A", "client-a", "trial", &created, (*int32)(nil), (*int32)(nil), &minutes, int64(90), int64(1), int64(3), created))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectExec("admin_update_workspace").WithArgs(unitOperatorID, unitRequestID, unitTenantID, `{"live_view_minutes":300}`).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	repository := NewPostgresRepository(mock)
	workspaces, err := repository.ListWorkspaces(context.Background(), unitOperatorID)
	if err != nil || len(workspaces) != 1 || workspaces[0].LiveViewMinutes == nil || *workspaces[0].LiveViewMinutes != 10 || workspaces[0].CameraLimit != nil || workspaces[0].Cameras != 3 {
		t.Fatalf("list: %+v %v", workspaces, err)
	}
	if err := repository.UpdateWorkspace(context.Background(), unitOperatorID, unitRequestID, unitTenantID, map[string]any{"live_view_minutes": 300}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAdminMapsDatabaseRefusals(t *testing.T) {
	for code, want := range map[string]error{"42501": ErrNotOperator, "22023": ErrAdminInvalid, "P0002": ErrWorkspaceNotFound} {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
		mock.ExpectExec("admin_update_workspace").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(&pgconn.PgError{Code: code})
		mock.ExpectRollback()
		err = NewPostgresRepository(mock).UpdateWorkspace(context.Background(), unitOperatorID, unitRequestID, unitTenantID, map[string]any{"plan": "active"})
		if !errors.Is(err, want) {
			t.Errorf("code %s: got %v, want %v", code, err, want)
		}
		mock.Close()
	}
	repository := NewPostgresRepository(nil)
	if _, err := repository.ListWorkspaces(context.Background(), unitOperatorID); err == nil {
		t.Fatal("a missing pool fails")
	}
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	if err := NewPostgresRepository(mock).UpdateWorkspace(context.Background(), "not-a-uuid", unitRequestID, unitTenantID, map[string]any{"plan": "active"}); !errors.Is(err, ErrAdminInvalid) {
		t.Fatalf("a bad actor id is refused before the database: %v", err)
	}
	if err := NewPostgresRepository(mock).UpdateWorkspace(context.Background(), unitOperatorID, unitRequestID, unitTenantID, map[string]any{}); !errors.Is(err, ErrAdminInvalid) {
		t.Fatalf("an empty change is refused: %v", err)
	}
}
