package privacymasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

type releaseAuthorizerStub struct {
	err   error
	calls int
}

func (s *releaseAuthorizerStub) Authorize(Request, []Approval, ReleaseEvidence) error {
	s.calls++
	return s.err
}

func releaseCommand() CreateReleaseCommand {
	return CreateReleaseCommand{ReleaseID: "77777777-7777-4777-8777-777777777777", TenantID: tenantID, SiteID: siteID, CameraID: cameraID, RequestID: "55555555-5555-4555-8555-555555555555", DeviceID: releaseDeviceID, Version: 1, Candidate: []byte(`{"candidate":"metadata"}`), Pipeline: []byte(`{"stages":["decode","mask","encode"]}`), HILEvidence: []byte(`{"attestation":"opaque"}`), CandidateHash: strings.Repeat("a", 64), EvidenceHash: strings.Repeat("b", 64), ActorID: "release-admin", AuditRequestID: auditRequestID}
}

func expectReleaseAudit(mock pgxmock.PgxPoolIface, releaseID string, occurredAt time.Time) {
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(tenantID + ":" + occurredAt.Format("2006-01-02")).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("SELECT record_hash").WithArgs(tenantID, occurredAt.Format("2006-01-02")).WillReturnRows(pgxmock.NewRows([]string{"record_hash"}))
	mock.ExpectExec("INSERT INTO audit.events").WithArgs(pgxmock.AnyArg(), tenantID, occurredAt.Format("2006-01-02"), occurredAt, "release-admin", "privacy_mask.release.created", "privacy_mask_release", releaseID, auditRequestID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func expectReleaseWrite(mock pgxmock.PgxPoolIface, request Request, approved bool, createdAt time.Time) {
	expectPrivacyMaskTransaction(mock, pgx.ReadWrite)
	mock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	mock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(approvedReleaseApprovals()))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(releaseDeviceID, siteID).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(approved))
	if approved {
		mock.ExpectQuery("INSERT INTO config.privacy_mask_release_manifests").WithArgs("77777777-7777-4777-8777-777777777777", tenantID, siteID, cameraID, request.ID, releaseDeviceID, int64(1), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), strings.Repeat("a", 64), strings.Repeat("b", 64), "release-admin").WillReturnRows(pgxmock.NewRows([]string{"created_at"}).AddRow(createdAt))
	}
}

func TestPostgresReleaseRepositoryAuthorizesAndAtomicallyAuditsManifest(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	request, createdAt := approvedReleaseRequest(), time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC)
	stub := &releaseAuthorizerStub{}
	expectReleaseWrite(mock, request, true, createdAt)
	expectReleaseAudit(mock, releaseCommand().ReleaseID, createdAt)
	mock.ExpectCommit()
	release, err := NewPostgresReleaseRepository(mock, stub).Create(context.Background(), releaseCommand())
	if err != nil || release.ID != releaseCommand().ReleaseID || release.CreatedAt != createdAt || stub.calls != 1 {
		t.Fatalf("create authorized release: %#v %v calls=%d", release, err, stub.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresReleaseRepositoryFailsClosedWithoutDeviceAuthorizationEvidenceOrAudit(t *testing.T) {
	request := approvedReleaseRequest()
	deviceMock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer deviceMock.Close()
	expectReleaseWrite(deviceMock, request, false, time.Time{})
	if _, err := NewPostgresReleaseRepository(deviceMock, &releaseAuthorizerStub{}).Create(context.Background(), releaseCommand()); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("unauthorized device must fail: %v", err)
	}
	if err := deviceMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	evidenceMock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer evidenceMock.Close()
	expectPrivacyMaskTransaction(evidenceMock, pgx.ReadWrite)
	evidenceMock.ExpectQuery("FROM config.privacy_mask_requests.*FOR UPDATE").WithArgs(request.ID).WillReturnRows(privacyMaskRows(request))
	evidenceMock.ExpectQuery("FROM config.privacy_mask_approvals").WithArgs(request.ID).WillReturnRows(privacyMaskApprovalRows(approvedReleaseApprovals()))
	evidenceMock.ExpectQuery("SELECT EXISTS").WithArgs(releaseDeviceID, siteID).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	if _, err := NewPostgresReleaseRepository(evidenceMock, &releaseAuthorizerStub{err: errors.New("bad evidence")}).Create(context.Background(), releaseCommand()); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("bad evidence must fail: %v", err)
	}
	if err := evidenceMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	if _, err := NewPostgresReleaseRepository(nil, nil).Create(context.Background(), releaseCommand()); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("missing repository must fail: %v", err)
	}
	invalid := releaseCommand()
	invalid.Version = 0
	if _, err := NewPostgresReleaseRepository(nil, &releaseAuthorizerStub{}).Create(context.Background(), invalid); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("invalid input must fail: %v", err)
	}
}
