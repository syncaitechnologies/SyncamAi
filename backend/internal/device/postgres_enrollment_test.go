package device

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
)

func expectEnrollmentAudit(mock pgxmock.PgxPoolIface, action string, actorID string, resourceID any, occurredAt time.Time) {
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":2026-08-13").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantA, "2026-08-13").WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(
		pgxmock.AnyArg(), tenantA, "2026-08-13", occurredAt, actorID, action, "edge_device", resourceID,
		postgresRequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestPostgresEnrollmentIssuesAuditedClaimAndReplaysWithoutStoredSecret(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	tokens := testClaimTokens(t)
	repository := NewPostgresEnrollmentRepository(mock, tokens)
	createdAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(claimLifetime)
	command := IssueClaimCommand{
		TenantID: tenantA, ActorID: "user-1", RequestID: postgresRequestID, IdempotencyKey: "device-claim-1",
		SiteID: siteA, SerialNumber: " edge-01 ", HardwareTier: "m", Model: "Jetson Orin",
	}
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":device-claim-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(tenantA, "device-claim-1").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "device-claim-1").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(siteA, tenantA).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO config.edge_devices").WithArgs(pgxmock.AnyArg(), tenantA, siteA, "EDGE-01", "m", "Jetson Orin", "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, createdAt))
	mock.ExpectQuery("INSERT INTO platform.device_claims").WithArgs(pgxmock.AnyArg(), tenantA, pgxmock.AnyArg(), pgxmock.AnyArg(), "user-1").WillReturnRows(pgxmock.NewRows([]string{"created_at", "expires_at"}).AddRow(createdAt, expiresAt))
	mock.ExpectExec("INSERT INTO platform.idempotency_keys").WithArgs(tenantA, "device-claim-1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	expectEnrollmentAudit(mock, "device.claim_issued", "user-1", pgxmock.AnyArg(), createdAt)
	mock.ExpectCommit()
	issued, err := repository.IssueClaim(context.Background(), command)
	if err != nil || issued.Replayed || issued.ClaimToken == "" || issued.Claim.ExpiresAt != expiresAt || issued.Claim.SerialNumber != "EDGE-01" {
		t.Fatalf("unexpected issuance: %+v %v", issued, err)
	}

	requestHash, _ := hashIssueClaim(command)
	storedResponse, _ := json.Marshal(issued.Claim)
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(tenantA + ":device-claim-1").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec("DELETE FROM platform.idempotency_keys").WithArgs(tenantA, "device-claim-1").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectQuery("SELECT request_hash, response_body").WithArgs(tenantA, "device-claim-1").WillReturnRows(pgxmock.NewRows([]string{"request_hash", "response_body"}).AddRow(requestHash, storedResponse))
	mock.ExpectCommit()
	replayed, err := repository.IssueClaim(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.ClaimToken != issued.ClaimToken {
		t.Fatalf("unexpected issuance replay: %+v %v", replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEnrollmentActivatesAndConsumesClaimAtomically(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	tokens := testClaimTokens(t)
	repository := NewPostgresEnrollmentRepository(mock, tokens)
	claimID := "77777777-7777-4777-8777-777777777777"
	deviceID := "88888888-8888-4888-8888-888888888888"
	token, _ := tokens.Token(claimID, tenantA)
	createdAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	activatedAt := createdAt.Add(time.Minute)
	expiresAt := createdAt.Add(claimLifetime)
	expectTenantTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT d.id::text").WithArgs(claimID, deviceID).WillReturnRows(pgxmock.NewRows([]string{
		"id", "tenant_id", "site_id", "serial", "tier", "model", "status", "cert_status", "activated_at", "created_at", "updated_at", "token_hash", "expires_at", "consumed_at", "now",
	}).AddRow(deviceID, tenantA, siteA, "EDGE-01", "m", "Jetson Orin", "pending", "pending", nil, createdAt, createdAt, claimTokenHash(token), expiresAt, nil, activatedAt))
	mock.ExpectQuery("UPDATE platform.device_claims").WithArgs(claimID).WillReturnRows(pgxmock.NewRows([]string{"consumed_at"}).AddRow(activatedAt))
	mock.ExpectQuery("UPDATE config.edge_devices").WithArgs(deviceID, activatedAt, "device:"+deviceID).WillReturnRows(pgxmock.NewRows([]string{"updated_at"}).AddRow(activatedAt))
	expectEnrollmentAudit(mock, "device.activated", "device:"+deviceID, deviceID, activatedAt)
	mock.ExpectCommit()
	activated, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: deviceID, ClaimToken: token, SerialNumber: " edge-01 ", RequestID: postgresRequestID})
	if err != nil || activated.Status != "active" || activated.ActivatedAt == nil || *activated.ActivatedAt != activatedAt || activated.CertificateStatus != "pending" {
		t.Fatalf("unexpected activation: %+v %v", activated, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEnrollmentRejectsConflictsAndFailsClosed(t *testing.T) {
	if _, err := (*PostgresEnrollmentRepository)(nil).IssueClaim(context.Background(), IssueClaimCommand{TenantID: tenantA}); err == nil {
		t.Fatal("nil repository must fail closed")
	}
	if _, err := NewPostgresEnrollmentRepository(nil, testClaimTokens(t)).Activate(context.Background(), ActivateDeviceCommand{ClaimToken: "invalid"}); !errors.Is(err, ErrClaimInvalid) {
		t.Fatalf("invalid token must fail before database use: %v", err)
	}
	if !errors.Is(classifyEnrollmentWrite(&pgconn.PgError{Code: "23503"}), ErrSiteNotFound) {
		t.Fatal("foreign key must map to site not found")
	}
	if !errors.Is(classifyEnrollmentWrite(&pgconn.PgError{Code: "23505"}), ErrDeviceSerialConflict) {
		t.Fatal("unique violation must map to device serial conflict")
	}
	if classifyEnrollmentWrite(errors.New("database unavailable")) == nil {
		t.Fatal("unexpected database error must be wrapped")
	}
}
