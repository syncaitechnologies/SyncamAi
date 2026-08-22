package privacymasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const auditRequestID = "44444444-4444-4444-8444-444444444444"

func privacyMaskRows(request Request) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "camera_id", "name", "geometry", "status", "requested_by", "requested_at"}).AddRow(
		request.ID, request.TenantID, request.SiteID, request.CameraID, request.Name,
		request.Geometry, request.Status, request.RequestedBy, request.RequestedAt,
	)
}

func privacyMaskApprovalRows(approvals []Approval) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{"approver_id", "approved_at"})
	for _, approval := range approvals {
		rows.AddRow(approval.ApproverID, approval.ApprovedAt)
	}
	return rows
}

func expectPrivacyMaskTransaction(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("set_config").WithArgs(tenantID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func expectPrivacyMaskAudit(mock pgxmock.PgxPoolIface, actor, action string, resourceID any, requestID string, occurredAt time.Time) {
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(tenantID + ":" + occurredAt.Format("2006-01-02")).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, occurredAt.Format("2006-01-02")).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), tenantID, occurredAt.Format("2006-01-02"), occurredAt, actor, action, "privacy_mask_request", resourceID, requestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestPostgresRepositoryReadsAndAtomicallyAuditsPrivacyMaskWorkflow(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	createdAt := time.Date(2026, 8, 22, 11, 0, 0, 0, time.UTC)
	request := Request{ID: "55555555-5555-4555-8555-555555555555", TenantID: tenantID, SiteID: siteID, CameraID: cameraID, Name: "Entry privacy", Geometry: command("requester").Geometry, Status: StatusPending, RequestedBy: "requester", RequestedAt: createdAt}

	expectPrivacyMaskTransaction(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM config.privacy_mask_requests").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(nil))
	mock.ExpectCommit()
	loaded, err := NewPostgresRepository(mock).Get(context.Background(), tenantID, request.ID)
	if err != nil || loaded.ID != request.ID || len(loaded.Approvals) != 0 {
		t.Fatalf("get request: %#v %v", loaded, err)
	}

	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("INSERT INTO config.privacy_mask_requests").WithArgs(pgxmock.AnyArg(), tenantID, siteID, cameraID, "Entry privacy", request.Geometry, StatusPending, "requester").WillReturnRows(pgxmock.NewRows([]string{"requested_at"}).AddRow(createdAt))
	expectPrivacyMaskAudit(mock, "requester", "privacy_mask.requested", pgxmock.AnyArg(), auditRequestID, createdAt)
	mock.ExpectCommit()
	created, err := NewPostgresRepository(mock).Create(context.Background(), CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", RequestID: auditRequestID, Name: " Entry privacy ", Geometry: request.Geometry})
	if err != nil || created.Status != StatusPending || created.ID == "" || created.RequestedAt != createdAt {
		t.Fatalf("create request: %#v %v", created, err)
	}

	firstAt := createdAt.Add(time.Minute)
	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(nil))
	mock.ExpectQuery("INSERT INTO config.privacy_mask_approvals").WithArgs(request.ID, tenantID, "approver-a").WillReturnRows(pgxmock.NewRows([]string{"approved_at"}).AddRow(firstAt))
	expectPrivacyMaskAudit(mock, "approver-a", "privacy_mask.approval.recorded", request.ID, auditRequestID, firstAt)
	mock.ExpectCommit()
	first, err := NewPostgresRepository(mock).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-a", AuditRequestID: auditRequestID})
	if err != nil || first.Status != StatusPending || len(first.Approvals) != 1 {
		t.Fatalf("first approval: %#v %v", first, err)
	}

	secondAt := firstAt.Add(time.Minute)
	request.Approvals = []Approval{{ApproverID: "approver-a", ApprovedAt: firstAt}}
	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(request.Approvals))
	mock.ExpectQuery("INSERT INTO config.privacy_mask_approvals").WithArgs(request.ID, tenantID, "approver-b").WillReturnRows(pgxmock.NewRows([]string{"approved_at"}).AddRow(secondAt))
	mock.ExpectQuery("UPDATE config.privacy_mask_requests").WithArgs(request.ID, StatusApproved).WillReturnRows(pgxmock.NewRows([]string{"approved_at"}).AddRow(secondAt))
	expectPrivacyMaskAudit(mock, "approver-b", "privacy_mask.approval.recorded", request.ID, auditRequestID, secondAt)
	mock.ExpectCommit()
	second, err := NewPostgresRepository(mock).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-b", AuditRequestID: auditRequestID})
	if err != nil || second.Status != StatusApproved || len(second.Approvals) != 2 {
		t.Fatalf("second approval: %#v %v", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryRejectsReplaysConflictsAndAuditFailures(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	request := Request{ID: "55555555-5555-4555-8555-555555555555", TenantID: tenantID, SiteID: siteID, CameraID: cameraID, Name: "Entry privacy", Geometry: command("requester").Geometry, Status: StatusPending, RequestedBy: "requester", RequestedAt: now}

	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(nil))
	if _, err := NewPostgresRepository(mock).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "requester", AuditRequestID: auditRequestID}); !errors.Is(err, ErrRequesterCannotApprove) {
		t.Fatalf("requester approval must fail: %v", err)
	}

	request.Approvals = []Approval{{ApproverID: "approver-a", ApprovedAt: now}}
	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(request.Approvals))
	mock.ExpectCommit()
	replayed, err := NewPostgresRepository(mock).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-a", AuditRequestID: auditRequestID})
	if err != nil || len(replayed.Approvals) != 1 {
		t.Fatalf("duplicate approval must be idempotent: %#v %v", replayed, err)
	}

	request.Status = StatusApproved
	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(request.Approvals))
	if _, err := NewPostgresRepository(mock).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-b", AuditRequestID: auditRequestID}); !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("surplus approval must fail: %v", err)
	}

	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("INSERT INTO config.privacy_mask_requests").WithArgs(pgxmock.AnyArg(), tenantID, siteID, cameraID, "Entry privacy", request.Geometry, StatusPending, "requester").WillReturnRows(pgxmock.NewRows([]string{"requested_at"}).AddRow(now))
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(tenantID + ":2026-08-22").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, "2026-08-22").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), tenantID, "2026-08-22", now, "requester", "privacy_mask.requested", "privacy_mask_request", pgxmock.AnyArg(), auditRequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(errors.New("audit unavailable"))
	if _, err := NewPostgresRepository(mock).Create(context.Background(), CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", RequestID: auditRequestID, Name: "Entry privacy", Geometry: request.Geometry}); err == nil {
		t.Fatal("audit failure must roll privacy-mask create back")
	}

	if _, err := NewPostgresRepository(nil).Get(context.Background(), tenantID, request.ID); err == nil {
		t.Fatal("nil repository must fail closed")
	}
	if _, err := NewPostgresRepository(mock).Create(context.Background(), CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", Name: "Entry privacy", Geometry: request.Geometry}); err == nil {
		t.Fatal("missing audit request identifier must fail closed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryFailsClosedOnUnavailableStorageAndInvalidInput(t *testing.T) {
	request := Request{ID: "55555555-5555-4555-8555-555555555555", TenantID: tenantID, SiteID: siteID, CameraID: cameraID, Name: "Entry privacy", Geometry: command("requester").Geometry, Status: StatusPending, RequestedBy: "requester", RequestedAt: time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)}
	if _, err := NewPostgresRepository(nil).Create(context.Background(), CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", RequestID: auditRequestID, Name: "Entry privacy", Geometry: request.Geometry}); err == nil {
		t.Fatal("nil create repository must fail closed")
	}
	if _, err := NewPostgresRepository(nil).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver", AuditRequestID: auditRequestID}); err == nil {
		t.Fatal("nil approve repository must fail closed")
	}
	if _, err := NewPostgresRepository(nil).Get(context.Background(), "not-a-tenant", request.ID); err == nil {
		t.Fatal("invalid tenant must fail closed")
	}
	if _, err := NewPostgresRepository(nil).Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: "not-a-request", ActorID: "approver", AuditRequestID: auditRequestID}); err == nil {
		t.Fatal("invalid request identifier must fail closed")
	}

	setContextMock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer setContextMock.Close()
	setContextMock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	setContextMock.ExpectExec("set_config").WithArgs(tenantID).WillReturnError(errors.New("tenant context unavailable"))
	if _, err := NewPostgresRepository(setContextMock).Get(context.Background(), tenantID, request.ID); err == nil {
		t.Fatal("tenant-context failure must fail closed")
	}
	if err := setContextMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	notFoundMock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer notFoundMock.Close()
	expectPrivacyMaskTransaction(notFoundMock, pgx.ReadOnly)
	notFoundMock.ExpectQuery("FROM config.privacy_mask_requests").WithArgs(request.ID).WillReturnError(pgx.ErrNoRows)
	if _, err := NewPostgresRepository(notFoundMock).Get(context.Background(), tenantID, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request must be hidden: %v", err)
	}
	if err := notFoundMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	commitMock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer commitMock.Close()
	expectPrivacyMaskTransaction(commitMock, pgx.ReadWrite)
	commitMock.ExpectQuery("INSERT INTO config.privacy_mask_requests").WithArgs(pgxmock.AnyArg(), tenantID, siteID, cameraID, "Entry privacy", request.Geometry, StatusPending, "requester").WillReturnRows(pgxmock.NewRows([]string{"requested_at"}).AddRow(request.RequestedAt))
	expectPrivacyMaskAudit(commitMock, "requester", "privacy_mask.requested", pgxmock.AnyArg(), auditRequestID, request.RequestedAt)
	commitMock.ExpectCommit().WillReturnError(errors.New("commit unavailable"))
	if _, err := NewPostgresRepository(commitMock).Create(context.Background(), CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", RequestID: auditRequestID, Name: "Entry privacy", Geometry: request.Geometry}); err == nil {
		t.Fatal("commit failure must fail privacy-mask create")
	}
	if err := commitMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
