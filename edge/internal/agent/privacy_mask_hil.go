package agent

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxHILHarnessIDLength = 128

var ErrInvalidPrivacyMaskHILEvidence = errors.New("privacy mask hardware-in-loop evidence is invalid")

// PrivacyMaskHILAttestation is signed evidence generated outside the product
// data path by a registered physical-camera/encoder harness. It carries only
// opaque hashes and measured booleans: never frames, pixels, credentials, or
// encoder output.
type PrivacyMaskHILAttestation struct {
	TestRunID           string    `json:"test_run_id"`
	DeviceID            string    `json:"device_id"`
	CandidateHash       string    `json:"candidate_hash"`
	HarnessID           string    `json:"harness_id"`
	ExecutionKind       string    `json:"execution_kind"`
	AttestedAt          time.Time `json:"attested_at"`
	EncoderBuildHash    string    `json:"encoder_build_hash"`
	EncodedProbeHash    string    `json:"encoded_probe_hash"`
	MaskBeforeEncode    bool      `json:"mask_before_encode"`
	EncoderBypassDenied bool      `json:"encoder_bypass_denied"`
	RawFramesRetained   bool      `json:"raw_frames_retained"`
	Signature           []byte    `json:"signature"`
}

// PrivacyMaskHILVerification is deterministic, non-sensitive evidence that a
// trusted physical HIL attestation met the release-gate shape. It is not a
// release permit and does not activate or deliver a privacy mask.
type PrivacyMaskHILVerification struct {
	TestRunID     string `json:"test_run_id"`
	CandidateHash string `json:"candidate_hash"`
	EvidenceHash  string `json:"evidence_hash"`
}

// VerifyPrivacyMaskHILEvidence validates a signed physical HIL result against
// the supplied, locally managed public-key allowlist. Callers must provide the
// candidate hash produced by VerifyPreEncodePrivacyMask; neither raw media nor
// an encoder handle is accepted here.
func VerifyPrivacyMaskHILEvidence(attestation PrivacyMaskHILAttestation, trustedHarnessKeys map[string]ed25519.PublicKey) (PrivacyMaskHILVerification, error) {
	if err := validateHILAttestation(attestation); err != nil {
		return PrivacyMaskHILVerification{}, err
	}
	key, ok := trustedHarnessKeys[strings.TrimSpace(attestation.HarnessID)]
	if !ok || len(key) != ed25519.PublicKeySize {
		return PrivacyMaskHILVerification{}, ErrInvalidPrivacyMaskHILEvidence
	}
	payload, err := canonicalHILPayload(attestation)
	if err != nil || !ed25519.Verify(key, payload, attestation.Signature) {
		return PrivacyMaskHILVerification{}, ErrInvalidPrivacyMaskHILEvidence
	}
	sum := sha256.Sum256(payload)
	return PrivacyMaskHILVerification{
		TestRunID: attestation.TestRunID, CandidateHash: attestation.CandidateHash,
		EvidenceHash: hex.EncodeToString(sum[:]),
	}, nil
}

func validateHILAttestation(attestation PrivacyMaskHILAttestation) error {
	for _, identifier := range []string{attestation.TestRunID, attestation.DeviceID} {
		parsed, err := uuid.Parse(identifier)
		if err != nil || parsed.Version() != 4 {
			return ErrInvalidPrivacyMaskHILEvidence
		}
	}
	if !validSHA256Hex(attestation.CandidateHash) || !validSHA256Hex(attestation.EncoderBuildHash) || !validSHA256Hex(attestation.EncodedProbeHash) {
		return ErrInvalidPrivacyMaskHILEvidence
	}
	if strings.TrimSpace(attestation.HarnessID) == "" || len(strings.TrimSpace(attestation.HarnessID)) > maxHILHarnessIDLength || attestation.ExecutionKind != "physical" || attestation.AttestedAt.IsZero() || !attestation.MaskBeforeEncode || !attestation.EncoderBypassDenied || attestation.RawFramesRetained || len(attestation.Signature) != ed25519.SignatureSize {
		return ErrInvalidPrivacyMaskHILEvidence
	}
	return nil
}

func canonicalHILPayload(attestation PrivacyMaskHILAttestation) ([]byte, error) {
	payload := struct {
		Version             int    `json:"version"`
		TestRunID           string `json:"test_run_id"`
		DeviceID            string `json:"device_id"`
		CandidateHash       string `json:"candidate_hash"`
		HarnessID           string `json:"harness_id"`
		ExecutionKind       string `json:"execution_kind"`
		AttestedAt          string `json:"attested_at"`
		EncoderBuildHash    string `json:"encoder_build_hash"`
		EncodedProbeHash    string `json:"encoded_probe_hash"`
		MaskBeforeEncode    bool   `json:"mask_before_encode"`
		EncoderBypassDenied bool   `json:"encoder_bypass_denied"`
		RawFramesRetained   bool   `json:"raw_frames_retained"`
	}{
		Version: 1, TestRunID: attestation.TestRunID, DeviceID: attestation.DeviceID,
		CandidateHash: attestation.CandidateHash, HarnessID: strings.TrimSpace(attestation.HarnessID),
		ExecutionKind: attestation.ExecutionKind, AttestedAt: attestation.AttestedAt.UTC().Format(time.RFC3339Nano),
		EncoderBuildHash: attestation.EncoderBuildHash, EncodedProbeHash: attestation.EncodedProbeHash,
		MaskBeforeEncode: attestation.MaskBeforeEncode, EncoderBypassDenied: attestation.EncoderBypassDenied,
		RawFramesRetained: attestation.RawFramesRetained,
	}
	return json.Marshal(payload)
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
