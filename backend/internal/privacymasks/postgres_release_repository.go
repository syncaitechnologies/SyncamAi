package privacymasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

// Release is durable, privacy-only release metadata. It excludes all media
// and executable controls and is immutable once written.
type Release struct {
	ID, TenantID, SiteID, CameraID, RequestID, DeviceID string
	Version                                             int64
	Candidate, Pipeline, HILEvidence                    json.RawMessage
	CandidateHash, EvidenceHash, CreatedBy              string
	CreatedAt                                           time.Time
}

type CreateReleaseCommand struct {
	ReleaseID, TenantID, SiteID, CameraID, RequestID, DeviceID string
	Version                                                    int64
	Candidate, Pipeline, HILEvidence                           json.RawMessage
	CandidateHash, EvidenceHash, ActorID, AuditRequestID       string
}

// PostgresReleaseRepository writes releases only after governed request and
// evidence checks. It is intentionally not an HTTP or device transport.
type PostgresReleaseRepository struct {
	pool       transactionPool
	authorizer ReleaseAuthorizer
}

func NewPostgresReleaseRepository(pool transactionPool, authorizer ReleaseAuthorizer) *PostgresReleaseRepository {
	return &PostgresReleaseRepository{pool: pool, authorizer: authorizer}
}

func (r *PostgresReleaseRepository) Create(ctx context.Context, command CreateReleaseCommand) (Release, error) {
	if err := validateCreateRelease(command); err != nil {
		return Release{}, err
	}
	if r == nil || r.pool == nil || r.authorizer == nil {
		return Release{}, ErrReleaseNotAuthorized
	}
	tx, err := (&PostgresRepository{pool: r.pool}).begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	request, err := loadRequest(ctx, tx, command.RequestID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrReleaseNotAuthorized
	}
	if err != nil {
		return Release{}, fmt.Errorf("lock privacy mask release request: %w", err)
	}
	approvals, err := loadApprovals(ctx, tx, command.RequestID)
	if err != nil {
		return Release{}, fmt.Errorf("read privacy mask release approvals: %w", err)
	}
	if request.Status != StatusApproved || request.SiteID != command.SiteID || request.CameraID != command.CameraID || len(approvals) != 2 {
		return Release{}, ErrReleaseNotAuthorized
	}
	var deviceAuthorized bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM config.edge_devices
		WHERE id = $1::uuid AND site_id = $2::uuid
		  AND status IN ('active', 'offline') AND cert_status = 'active'
	)`, command.DeviceID, command.SiteID).Scan(&deviceAuthorized); err != nil {
		return Release{}, fmt.Errorf("read privacy mask release device: %w", err)
	}
	if !deviceAuthorized {
		return Release{}, ErrReleaseNotAuthorized
	}
	evidence := ReleaseEvidence{Candidate: command.Candidate, Pipeline: command.Pipeline, HILEvidence: command.HILEvidence, CandidateHash: command.CandidateHash, EvidenceHash: command.EvidenceHash}
	if err := r.authorizer.Authorize(request, approvals, evidence); err != nil {
		return Release{}, ErrReleaseNotAuthorized
	}
	release := Release{ID: command.ReleaseID, TenantID: command.TenantID, SiteID: command.SiteID, CameraID: command.CameraID, RequestID: command.RequestID, DeviceID: command.DeviceID, Version: command.Version, Candidate: append(json.RawMessage(nil), command.Candidate...), Pipeline: append(json.RawMessage(nil), command.Pipeline...), HILEvidence: append(json.RawMessage(nil), command.HILEvidence...), CandidateHash: command.CandidateHash, EvidenceHash: command.EvidenceHash, CreatedBy: strings.TrimSpace(command.ActorID)}
	if err := tx.QueryRow(ctx, `INSERT INTO config.privacy_mask_release_manifests (
		id, tenant_id, site_id, camera_id, request_id, device_id, version, candidate,
		pipeline, hil_evidence, candidate_hash, evidence_hash, created_by
	) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7,
		$8::jsonb, $9::jsonb, $10::jsonb, $11, $12, $13) RETURNING created_at`,
		release.ID, release.TenantID, release.SiteID, release.CameraID, release.RequestID,
		release.DeviceID, release.Version, release.Candidate, release.Pipeline, release.HILEvidence,
		release.CandidateHash, release.EvidenceHash, release.CreatedBy).Scan(&release.CreatedAt); err != nil {
		return Release{}, fmt.Errorf("write privacy mask release manifest: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{TenantID: command.TenantID, ActorID: command.ActorID, Action: "privacy_mask.release.created", ResourceType: "privacy_mask_release", ResourceID: release.ID, RequestID: command.AuditRequestID, AfterState: release, OccurredAt: release.CreatedAt}); err != nil {
		return Release{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("commit privacy mask release manifest: %w", err)
	}
	return cloneRelease(release), nil
}

func validateCreateRelease(command CreateReleaseCommand) error {
	for _, id := range []string{command.ReleaseID, command.TenantID, command.SiteID, command.CameraID, command.RequestID, command.DeviceID, command.AuditRequestID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.Version() != 4 {
			return ErrReleaseNotAuthorized
		}
	}
	if command.Version < 1 || strings.TrimSpace(command.ActorID) == "" || len(strings.TrimSpace(command.ActorID)) > 128 || !validReleaseHash(command.CandidateHash) || !validReleaseHash(command.EvidenceHash) {
		return ErrReleaseNotAuthorized
	}
	return nil
}

func cloneRelease(release Release) Release {
	release.Candidate = append(json.RawMessage(nil), release.Candidate...)
	release.Pipeline = append(json.RawMessage(nil), release.Pipeline...)
	release.HILEvidence = append(json.RawMessage(nil), release.HILEvidence...)
	return release
}
