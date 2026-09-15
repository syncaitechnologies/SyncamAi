package agent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
)

const (
	privacyMaskReleaseBundleSchemaVersion = 1
	maxPrivacyMaskReleaseBundleBytes      = 128 << 10
)

var ErrInvalidPrivacyMaskReleaseBundle = errors.New("privacy mask release bundle is invalid")

// PrivacyMaskReleaseBundle is the bounded, metadata-only artifact accepted by
// the local release loader. The profile identifier is supplied separately from
// the signed HIL evidence so a valid release cannot be moved between configured
// hardware profiles on the same device.
type PrivacyMaskReleaseBundle struct {
	SchemaVersion int                        `json:"schema_version"`
	ProfileID     string                     `json:"profile_id"`
	Release       PrivacyMaskReleaseManifest `json:"release"`
}

// PrivacyMaskReleaseScope is configured outside the release artifact. It binds
// loading to one tenant, site, camera, device, hardware profile, and HIL harness.
// It contains no stream URL, credential, private key, frame, or encoder handle.
type PrivacyMaskReleaseScope struct {
	TenantID string
	SiteID   string
	CameraID string
	Profile  PrivacyMaskHardwareProfile
}

// LoadedPrivacyMaskRelease is verified metadata ready for the existing
// controlled release gate. Loading does not accept, apply, or activate it.
type LoadedPrivacyMaskRelease struct {
	Manifest      PrivacyMaskReleaseManifest
	ProfileID     string
	CandidateHash string
	EvidenceHash  string
}

// PrivacyMaskReleaseLoader strictly decodes one bounded local artifact and
// authenticates its signed physical-HIL metadata using an injected public key.
// Trust provisioning and file permissions remain the caller's responsibility.
type PrivacyMaskReleaseLoader struct {
	scope             PrivacyMaskReleaseScope
	trustedHarnessKey ed25519.PublicKey
}

func NewPrivacyMaskReleaseLoader(scope PrivacyMaskReleaseScope, trustedHarnessKey ed25519.PublicKey) (*PrivacyMaskReleaseLoader, error) {
	if err := validatePrivacyMaskReleaseScope(scope); err != nil || len(trustedHarnessKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPrivacyMaskReleaseBundle
	}
	return &PrivacyMaskReleaseLoader{
		scope:             scope,
		trustedHarnessKey: append(ed25519.PublicKey(nil), trustedHarnessKey...),
	}, nil
}

// Load consumes exactly one JSON bundle. Oversized input, duplicate or unknown
// fields, trailing data, scope drift, an invalid candidate, and untrusted or
// non-physical HIL evidence all fail before any metadata is returned.
func (l *PrivacyMaskReleaseLoader) Load(reader io.Reader) (LoadedPrivacyMaskRelease, error) {
	if l == nil || reader == nil || validatePrivacyMaskReleaseScope(l.scope) != nil || len(l.trustedHarnessKey) != ed25519.PublicKeySize {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxPrivacyMaskReleaseBundleBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > maxPrivacyMaskReleaseBundleBytes || rejectDuplicateJSONFields(payload) != nil {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}

	var bundle PrivacyMaskReleaseBundle
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	if err := requireJSONEOF(decoder); err != nil {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	if bundle.SchemaVersion != privacyMaskReleaseBundleSchemaVersion || bundle.ProfileID != l.scope.Profile.ProfileID {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}

	manifest := bundle.Release
	if parsed, err := uuid.Parse(manifest.ReleaseID); err != nil || parsed.Version() != 4 || manifest.Version < 1 {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	if err := rejectUnknownPrivacyMaskGeometryFields(manifest.Candidate.Geometry); err != nil {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	if manifest.Candidate.TenantID != l.scope.TenantID || manifest.Candidate.SiteID != l.scope.SiteID || manifest.Candidate.CameraID != l.scope.CameraID ||
		manifest.DeviceID != l.scope.Profile.DeviceID || manifest.HILEvidence.DeviceID != l.scope.Profile.DeviceID || manifest.HILEvidence.HarnessID != l.scope.Profile.HarnessID {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}

	verification, err := VerifyPreEncodePrivacyMask(manifest.Candidate, manifest.Pipeline)
	if err != nil || manifest.HILEvidence.CandidateHash != verification.CandidateHash {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}
	hil, err := VerifyPrivacyMaskHILEvidence(manifest.HILEvidence, map[string]ed25519.PublicKey{l.scope.Profile.HarnessID: l.trustedHarnessKey})
	if err != nil || hil.CandidateHash != verification.CandidateHash {
		return LoadedPrivacyMaskRelease{}, ErrInvalidPrivacyMaskReleaseBundle
	}

	return LoadedPrivacyMaskRelease{
		Manifest:      clonePrivacyMaskReleaseManifest(manifest),
		ProfileID:     l.scope.Profile.ProfileID,
		CandidateHash: verification.CandidateHash,
		EvidenceHash:  hil.EvidenceHash,
	}, nil
}

func validatePrivacyMaskReleaseScope(scope PrivacyMaskReleaseScope) error {
	for _, identifier := range []string{scope.TenantID, scope.SiteID, scope.CameraID} {
		parsed, err := uuid.Parse(identifier)
		if err != nil || parsed.Version() != 4 {
			return ErrInvalidPrivacyMaskReleaseBundle
		}
	}
	if err := validatePrivacyMaskHardwareProfile(scope.Profile); err != nil {
		return ErrInvalidPrivacyMaskReleaseBundle
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidPrivacyMaskReleaseBundle
	}
	return nil
}

func rejectUnknownPrivacyMaskGeometryFields(raw json.RawMessage) error {
	var geometry struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&geometry); err != nil {
		return ErrInvalidPrivacyMaskReleaseBundle
	}
	return requireJSONEOF(decoder)
}

func rejectDuplicateJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := scanUniqueJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidPrivacyMaskReleaseBundle
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidPrivacyMaskReleaseBundle
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalidPrivacyMaskReleaseBundle
			}
			if _, duplicate := seen[key]; duplicate {
				return ErrInvalidPrivacyMaskReleaseBundle
			}
			seen[key] = struct{}{}
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
			return ErrInvalidPrivacyMaskReleaseBundle
		}
	case '[':
		for decoder.More() {
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
			return ErrInvalidPrivacyMaskReleaseBundle
		}
	default:
		return ErrInvalidPrivacyMaskReleaseBundle
	}
	return nil
}

func clonePrivacyMaskReleaseManifest(manifest PrivacyMaskReleaseManifest) PrivacyMaskReleaseManifest {
	copy := manifest
	copy.Candidate.ApproverIDs = append([]string(nil), manifest.Candidate.ApproverIDs...)
	copy.Candidate.Geometry = append(json.RawMessage(nil), manifest.Candidate.Geometry...)
	copy.Pipeline.Stages = append([]PipelineStage(nil), manifest.Pipeline.Stages...)
	copy.HILEvidence.Signature = append([]byte(nil), manifest.HILEvidence.Signature...)
	return copy
}
