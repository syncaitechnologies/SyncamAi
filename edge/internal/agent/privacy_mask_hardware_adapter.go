package agent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const maxPrivacyMaskHardwareProfileIDLength = 128

var ErrInvalidPrivacyMaskHardwareAdapter = errors.New("privacy mask hardware adapter is invalid")

// PrivacyMaskHardwareProfile binds this adapter to one allowlisted physical
// camera/encoder profile. It contains no stream URL, credential, frame, or
// encoder handle. The configured device and HIL harness must both match a
// verified release before the executor is called.
type PrivacyMaskHardwareProfile struct {
	ProfileID string
	DeviceID  string
	HarnessID string
}

// HardwarePrivacyMaskActivation is the bounded metadata supplied to a
// hardware-specific executor. Candidate geometry, media, stream credentials,
// signatures, and HIL probe data are deliberately excluded.
type HardwarePrivacyMaskActivation struct {
	ProfileID     string
	ReleaseID     string
	DeviceID      string
	Version       int64
	CandidateHash string
	Pipeline      []PipelineStage
}

// PrivacyMaskHardwareExecutor is implemented only by an allowlisted,
// hardware-specific integration. The adapter calls it after enforcing the
// metadata gates; an executor must not add an encoder-bypass path.
type PrivacyMaskHardwareExecutor interface {
	ActivatePreEncodePrivacyMask(context.Context, HardwarePrivacyMaskActivation) error
}

// HardwareBoundPrivacyMaskAdapter is a dedicated PrivacyMaskReleaseApplier.
// It cannot be used for generic configuration delivery and has no API for
// frames, pixels, streams, or credentials. ActiveRelease is updated only after
// the hardware executor succeeds, preserving the prior safe release on error.
type HardwareBoundPrivacyMaskAdapter struct {
	profile           PrivacyMaskHardwareProfile
	trustedHarnessKey ed25519.PublicKey
	executor          PrivacyMaskHardwareExecutor
	mu                sync.Mutex
	active            *HardwarePrivacyMaskActivation
}

func NewHardwareBoundPrivacyMaskAdapter(profile PrivacyMaskHardwareProfile, trustedHarnessKey ed25519.PublicKey, executor PrivacyMaskHardwareExecutor) (*HardwareBoundPrivacyMaskAdapter, error) {
	if err := validatePrivacyMaskHardwareProfile(profile); err != nil || len(trustedHarnessKey) != ed25519.PublicKeySize || executor == nil {
		return nil, ErrInvalidPrivacyMaskHardwareAdapter
	}
	return &HardwareBoundPrivacyMaskAdapter{
		profile: normalizedPrivacyMaskHardwareProfile(profile), trustedHarnessKey: append(ed25519.PublicKey(nil), trustedHarnessKey...), executor: executor,
	}, nil
}

// ApplyVerifiedRelease accepts a manifest only when it matches the adapter's
// configured physical profile, is bound to its device and HIL harness, and
// preserves the strict decode -> mask -> encode ordering. The controlled
// release gate verifies the attestation signature before it invokes this
// method. The adapter repeats that verification so direct callers fail closed.
func (a *HardwareBoundPrivacyMaskAdapter) ApplyVerifiedRelease(ctx context.Context, manifest PrivacyMaskReleaseManifest) error {
	if a == nil || a.executor == nil {
		return ErrInvalidPrivacyMaskHardwareAdapter
	}
	activation, err := a.activationFor(manifest)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil && activation.Version <= a.active.Version {
		return ErrInvalidPrivacyMaskHardwareAdapter
	}
	if err := a.executor.ActivatePreEncodePrivacyMask(ctx, activation); err != nil {
		return err
	}
	a.active = cloneHardwarePrivacyMaskActivation(activation)
	return nil
}

// ActiveRelease returns a copy of the active bounded metadata. It does not
// expose candidate geometry, frames, stream credentials, or encoder controls.
func (a *HardwareBoundPrivacyMaskAdapter) ActiveRelease() *HardwarePrivacyMaskActivation {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneHardwarePrivacyMaskActivationPointer(a.active)
}

func (a *HardwareBoundPrivacyMaskAdapter) activationFor(manifest PrivacyMaskReleaseManifest) (HardwarePrivacyMaskActivation, error) {
	if err := validatePrivacyMaskHardwareProfile(a.profile); err != nil {
		return HardwarePrivacyMaskActivation{}, ErrInvalidPrivacyMaskHardwareAdapter
	}
	if manifest.DeviceID != a.profile.DeviceID || manifest.HILEvidence.DeviceID != a.profile.DeviceID || strings.TrimSpace(manifest.HILEvidence.HarnessID) != a.profile.HarnessID || manifest.HILEvidence.ExecutionKind != "physical" || !manifest.HILEvidence.MaskBeforeEncode || !manifest.HILEvidence.EncoderBypassDenied || manifest.HILEvidence.RawFramesRetained {
		return HardwarePrivacyMaskActivation{}, ErrInvalidPrivacyMaskHardwareAdapter
	}
	verification, err := VerifyPreEncodePrivacyMask(manifest.Candidate, manifest.Pipeline)
	if err != nil || manifest.HILEvidence.CandidateHash != verification.CandidateHash {
		return HardwarePrivacyMaskActivation{}, ErrInvalidPrivacyMaskHardwareAdapter
	}
	if _, err := VerifyPrivacyMaskHILEvidence(manifest.HILEvidence, map[string]ed25519.PublicKey{a.profile.HarnessID: a.trustedHarnessKey}); err != nil {
		return HardwarePrivacyMaskActivation{}, ErrInvalidPrivacyMaskHardwareAdapter
	}
	parsedRelease, err := uuid.Parse(manifest.ReleaseID)
	if err != nil || parsedRelease.Version() != 4 || manifest.Version < 1 {
		return HardwarePrivacyMaskActivation{}, ErrInvalidPrivacyMaskHardwareAdapter
	}
	return HardwarePrivacyMaskActivation{
		ProfileID: a.profile.ProfileID, ReleaseID: manifest.ReleaseID, DeviceID: manifest.DeviceID,
		Version: manifest.Version, CandidateHash: verification.CandidateHash,
		Pipeline: append([]PipelineStage(nil), manifest.Pipeline.Stages...),
	}, nil
}

func validatePrivacyMaskHardwareProfile(profile PrivacyMaskHardwareProfile) error {
	if profile.ProfileID != strings.TrimSpace(profile.ProfileID) || profile.HarnessID != strings.TrimSpace(profile.HarnessID) || profile.ProfileID == "" || profile.HarnessID == "" || len(profile.ProfileID) > maxPrivacyMaskHardwareProfileIDLength || len(profile.HarnessID) > maxHILHarnessIDLength {
		return ErrInvalidPrivacyMaskHardwareAdapter
	}
	parsedDevice, err := uuid.Parse(profile.DeviceID)
	if err != nil || parsedDevice.Version() != 4 {
		return ErrInvalidPrivacyMaskHardwareAdapter
	}
	return nil
}

func normalizedPrivacyMaskHardwareProfile(profile PrivacyMaskHardwareProfile) PrivacyMaskHardwareProfile {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.HarnessID = strings.TrimSpace(profile.HarnessID)
	return profile
}

func cloneHardwarePrivacyMaskActivation(value HardwarePrivacyMaskActivation) *HardwarePrivacyMaskActivation {
	copy := value
	copy.Pipeline = append([]PipelineStage(nil), value.Pipeline...)
	return &copy
}

func cloneHardwarePrivacyMaskActivationPointer(value *HardwarePrivacyMaskActivation) *HardwarePrivacyMaskActivation {
	if value == nil {
		return nil
	}
	return cloneHardwarePrivacyMaskActivation(*value)
}
