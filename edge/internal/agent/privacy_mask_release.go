package agent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"

	"github.com/google/uuid"
)

var ErrInvalidPrivacyMaskRelease = errors.New("privacy mask controlled release is invalid")

// PrivacyMaskReleaseManifest is a separate metadata-only release channel. It
// is intentionally not a ConfigurationRevision and cannot enter generic zone
// delivery. The applier contract below is a release-slot update, not encoder or
// frame processing.
type PrivacyMaskReleaseManifest struct {
	ReleaseID   string                    `json:"release_id"`
	DeviceID    string                    `json:"device_id"`
	Version     int64                     `json:"version"`
	Candidate   PrivacyMaskCandidate      `json:"candidate"`
	Pipeline    PreEncodePipeline         `json:"pipeline"`
	HILEvidence PrivacyMaskHILAttestation `json:"hil_evidence"`
}

type PrivacyMaskReleaseState string

const (
	PrivacyMaskReleaseAccepted PrivacyMaskReleaseState = "accepted"
	PrivacyMaskReleaseFailed   PrivacyMaskReleaseState = "failed"
)

// PrivacyMaskReleaseStatus contains safe operational metadata only. ErrorCode
// is a fixed category, so frame data, topology, or encoder details cannot leak
// through status reporting.
type PrivacyMaskReleaseStatus struct {
	ReleaseID     string                  `json:"release_id"`
	DeviceID      string                  `json:"device_id"`
	Version       int64                   `json:"version"`
	CandidateHash string                  `json:"candidate_hash"`
	EvidenceHash  string                  `json:"evidence_hash,omitempty"`
	State         PrivacyMaskReleaseState `json:"state"`
	ErrorCode     string                  `json:"error_code,omitempty"`
}

// PrivacyMaskReleaseApplier receives only a release manifest whose candidate
// and HIL evidence were verified. Its atomic contract preserves the prior
// accepted metadata release on error; it must not execute masking itself.
type PrivacyMaskReleaseApplier interface {
	ApplyVerifiedRelease(context.Context, PrivacyMaskReleaseManifest) error
}

// PrivacyMaskReleaseReporter sends safe release state to a future dedicated
// status transport. It cannot change controller state or approve a release.
type PrivacyMaskReleaseReporter interface {
	ReportPrivacyMaskRelease(context.Context, PrivacyMaskReleaseStatus) error
}

// ControlledPrivacyMaskRelease is the edge release gate. It accepts metadata
// only after both the strict pre-encode plan and the signed physical HIL proof
// verify for the same candidate and device.
type ControlledPrivacyMaskRelease struct {
	trustedHarnessKeys map[string]ed25519.PublicKey
	applier            PrivacyMaskReleaseApplier
	reporter           PrivacyMaskReleaseReporter
	mu                 sync.Mutex
	lastAccepted       *PrivacyMaskReleaseStatus
}

func NewControlledPrivacyMaskRelease(trustedHarnessKeys map[string]ed25519.PublicKey, applier PrivacyMaskReleaseApplier, reporter PrivacyMaskReleaseReporter) (*ControlledPrivacyMaskRelease, error) {
	if len(trustedHarnessKeys) == 0 || applier == nil || reporter == nil {
		return nil, ErrInvalidPrivacyMaskRelease
	}
	keys := make(map[string]ed25519.PublicKey, len(trustedHarnessKeys))
	for harnessID, key := range trustedHarnessKeys {
		if harnessID == "" || len(key) != ed25519.PublicKeySize {
			return nil, ErrInvalidPrivacyMaskRelease
		}
		keys[harnessID] = append(ed25519.PublicKey(nil), key...)
	}
	return &ControlledPrivacyMaskRelease{trustedHarnessKeys: keys, applier: applier, reporter: reporter}, nil
}

// Accept verifies and atomically records one release manifest. A failed
// verification or apply leaves LastAccepted unchanged and reports only a safe
// failure category. Reporting failure never rolls a successful local acceptance
// back because doing so could desynchronize a physical release slot.
func (r *ControlledPrivacyMaskRelease) Accept(ctx context.Context, manifest PrivacyMaskReleaseManifest) error {
	status, err := r.verifiedStatus(manifest)
	if err != nil {
		return r.reportFailure(ctx, status)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastAccepted != nil && manifest.Version <= r.lastAccepted.Version {
		failed := status
		failed.State, failed.ErrorCode = PrivacyMaskReleaseFailed, "stale_release"
		_ = r.reporter.ReportPrivacyMaskRelease(ctx, failed)
		return ErrInvalidPrivacyMaskRelease
	}
	if err := r.applier.ApplyVerifiedRelease(ctx, manifest); err != nil {
		failed := status
		failed.State, failed.ErrorCode = PrivacyMaskReleaseFailed, "apply_failed"
		_ = r.reporter.ReportPrivacyMaskRelease(ctx, failed)
		return err
	}
	accepted := status
	accepted.State = PrivacyMaskReleaseAccepted
	r.lastAccepted = cloneReleaseStatus(accepted)
	if err := r.reporter.ReportPrivacyMaskRelease(ctx, accepted); err != nil {
		return err
	}
	return nil
}

// LastAccepted returns a copy of the active metadata release. It does not
// expose geometry, signatures, frame content, or any encoder controls.
func (r *ControlledPrivacyMaskRelease) LastAccepted() *PrivacyMaskReleaseStatus {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastAccepted == nil {
		return nil
	}
	return cloneReleaseStatus(*r.lastAccepted)
}

func (r *ControlledPrivacyMaskRelease) verifiedStatus(manifest PrivacyMaskReleaseManifest) (PrivacyMaskReleaseStatus, error) {
	if r == nil || r.applier == nil || r.reporter == nil {
		return PrivacyMaskReleaseStatus{State: PrivacyMaskReleaseFailed, ErrorCode: "verification_failed"}, ErrInvalidPrivacyMaskRelease
	}
	if parsed, err := uuid.Parse(manifest.ReleaseID); err != nil || parsed.Version() != 4 || manifest.Version < 1 {
		return PrivacyMaskReleaseStatus{State: PrivacyMaskReleaseFailed, ErrorCode: "verification_failed"}, ErrInvalidPrivacyMaskRelease
	}
	if parsed, err := uuid.Parse(manifest.DeviceID); err != nil || parsed.Version() != 4 {
		return PrivacyMaskReleaseStatus{ReleaseID: manifest.ReleaseID, State: PrivacyMaskReleaseFailed, ErrorCode: "verification_failed"}, ErrInvalidPrivacyMaskRelease
	}
	verification, err := VerifyPreEncodePrivacyMask(manifest.Candidate, manifest.Pipeline)
	if err != nil || manifest.HILEvidence.DeviceID != manifest.DeviceID || manifest.HILEvidence.CandidateHash != verification.CandidateHash {
		return PrivacyMaskReleaseStatus{ReleaseID: manifest.ReleaseID, DeviceID: manifest.DeviceID, State: PrivacyMaskReleaseFailed, ErrorCode: "verification_failed"}, ErrInvalidPrivacyMaskRelease
	}
	hil, err := VerifyPrivacyMaskHILEvidence(manifest.HILEvidence, r.trustedHarnessKeys)
	if err != nil || hil.CandidateHash != verification.CandidateHash {
		return PrivacyMaskReleaseStatus{ReleaseID: manifest.ReleaseID, DeviceID: manifest.DeviceID, CandidateHash: verification.CandidateHash, State: PrivacyMaskReleaseFailed, ErrorCode: "verification_failed"}, ErrInvalidPrivacyMaskRelease
	}
	return PrivacyMaskReleaseStatus{ReleaseID: manifest.ReleaseID, DeviceID: manifest.DeviceID, Version: manifest.Version, CandidateHash: verification.CandidateHash, EvidenceHash: hil.EvidenceHash}, nil
}

func (r *ControlledPrivacyMaskRelease) reportFailure(ctx context.Context, status PrivacyMaskReleaseStatus) error {
	if r == nil || r.reporter == nil {
		return ErrInvalidPrivacyMaskRelease
	}
	status.State, status.ErrorCode = PrivacyMaskReleaseFailed, "verification_failed"
	_ = r.reporter.ReportPrivacyMaskRelease(ctx, status)
	return ErrInvalidPrivacyMaskRelease
}

func cloneReleaseStatus(status PrivacyMaskReleaseStatus) *PrivacyMaskReleaseStatus {
	copy := status
	return &copy
}
