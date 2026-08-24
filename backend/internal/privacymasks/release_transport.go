package privacymasks

import (
	"context"
	"encoding/json"
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
	ReleaseID     string          `json:"release_id"`
	TenantID      string          `json:"tenant_id"`
	SiteID        string          `json:"site_id"`
	CameraID      string          `json:"camera_id"`
	RequestID     string          `json:"request_id"`
	DeviceID      string          `json:"device_id"`
	Version       int64           `json:"version"`
	Candidate     json.RawMessage `json:"candidate"`
	Pipeline      json.RawMessage `json:"pipeline"`
	HILEvidence   json.RawMessage `json:"hil_evidence"`
	CandidateHash string          `json:"candidate_hash"`
	EvidenceHash  string          `json:"evidence_hash"`
	CreatedAt     time.Time       `json:"created_at"`
}

type DeviceReleaseStatus struct {
	TenantID   string     `json:"tenant_id"`
	DeviceID   string     `json:"device_id"`
	ReleaseID  string     `json:"release_id"`
	Version    int64      `json:"version"`
	State      string     `json:"state"`
	ErrorCode  string     `json:"error_code,omitempty"`
	ReportedAt time.Time  `json:"reported_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
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

// ValidateReleaseReport exposes the fixed, metadata-only status contract to
// the dedicated mTLS HTTP boundary before it invokes storage.
func ValidateReleaseReport(command ReportReleaseCommand) error { return validateReleaseReport(command) }
