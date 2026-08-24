package privacymasks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrReleaseDeviceNotFound = errors.New("privacy mask release device not found")
	ErrInvalidReleaseStatus  = errors.New("privacy mask release status is invalid")
)

type DeviceReleaseManifest struct {
	ReleaseID, TenantID, SiteID, CameraID, RequestID, DeviceID string
	Version                                                    int64
	Candidate, Pipeline, HILEvidence                           []byte
	CandidateHash, EvidenceHash                                string
	CreatedAt                                                  time.Time
}

type DeviceReleaseStatus struct {
	TenantID, DeviceID, ReleaseID, State, ErrorCode string
	Version                                         int64
	ReportedAt                                      time.Time
	AcceptedAt                                      *time.Time
}

type PullReleaseResult struct{ Manifest *DeviceReleaseManifest }
type ReportReleaseCommand struct {
	DeviceID, ReleaseID, State, ErrorCode string
	Version                               int64
}

type ReleaseTransportRepository interface {
	Pull(context.Context, string, int64) (PullReleaseResult, error)
	Report(context.Context, ReportReleaseCommand) (DeviceReleaseStatus, error)
}

func validateReleaseReport(command ReportReleaseCommand) error {
	if _, err := uuid.Parse(command.DeviceID); err != nil {
		return ErrInvalidReleaseStatus
	}
	if _, err := uuid.Parse(command.ReleaseID); err != nil || command.Version < 1 {
		return ErrInvalidReleaseStatus
	}
	command.State, command.ErrorCode = strings.TrimSpace(command.State), strings.TrimSpace(command.ErrorCode)
	if (command.State == "accepted" && command.ErrorCode == "") || (command.State == "failed" && (command.ErrorCode == "verification_failed" || command.ErrorCode == "stale_release" || command.ErrorCode == "apply_failed")) {
		return nil
	}
	return ErrInvalidReleaseStatus
}
