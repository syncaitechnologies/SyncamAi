package privacymasks

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxReleaseCandidateBytes = 64 << 10
	maxReleasePipelineBytes  = 1024
	maxReleaseEvidenceBytes  = 8 << 10
	maxReleaseVertices       = 128
)

var ErrReleaseNotAuthorized = errors.New("privacy mask release is not authorized")

// ReleaseEvidence contains bounded, metadata-only evidence. It intentionally
// has no fields for media, credentials, encoder handles, or mask output.
type ReleaseEvidence struct {
	Candidate     json.RawMessage
	Pipeline      json.RawMessage
	HILEvidence   json.RawMessage
	CandidateHash string
	EvidenceHash  string
}

// ReleaseAuthorizer verifies the locally declared pre-encode boundary and the
// signed physical HIL attestation before durable release metadata is written.
type ReleaseAuthorizer interface {
	Authorize(Request, []Approval, ReleaseEvidence) error
}

// TrustedReleaseAuthorizer verifies evidence against an injected, externally
// registered harness-key allowlist. Key registration itself remains a separate
// security-reviewed capability.
type TrustedReleaseAuthorizer struct{ keys map[string]ed25519.PublicKey }

func NewTrustedReleaseAuthorizer(keys map[string]ed25519.PublicKey) (*TrustedReleaseAuthorizer, error) {
	if len(keys) == 0 {
		return nil, ErrReleaseNotAuthorized
	}
	copy := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if strings.TrimSpace(id) == "" || len(key) != ed25519.PublicKeySize {
			return nil, ErrReleaseNotAuthorized
		}
		copy[id] = append(ed25519.PublicKey(nil), key...)
	}
	return &TrustedReleaseAuthorizer{keys: copy}, nil
}

func (a *TrustedReleaseAuthorizer) Authorize(request Request, approvals []Approval, evidence ReleaseEvidence) error {
	if a == nil || len(a.keys) == 0 || request.Status != StatusApproved || len(approvals) != 2 {
		return ErrReleaseNotAuthorized
	}
	candidate, err := decodeReleaseCandidate(evidence.Candidate)
	if err != nil || !sameReleaseCandidate(request, approvals, candidate) {
		return ErrReleaseNotAuthorized
	}
	pipeline, err := decodeReleasePipeline(evidence.Pipeline)
	if err != nil || !strictReleasePipeline(pipeline.Stages) {
		return ErrReleaseNotAuthorized
	}
	candidateHash, err := releaseCandidateHash(candidate, pipeline)
	if err != nil || candidateHash != evidence.CandidateHash || !validReleaseHash(evidence.CandidateHash) {
		return ErrReleaseNotAuthorized
	}
	hil, err := decodeReleaseHIL(evidence.HILEvidence)
	if err != nil || hil.DeviceID == "" || hil.CandidateHash != candidateHash {
		return ErrReleaseNotAuthorized
	}
	key, ok := a.keys[hil.HarnessID]
	payload, err := releaseHILPayload(hil)
	if !ok || err != nil || !ed25519.Verify(key, payload, hil.Signature) {
		return ErrReleaseNotAuthorized
	}
	sum := sha256.Sum256(payload)
	if evidence.EvidenceHash != hex.EncodeToString(sum[:]) || !validReleaseHash(evidence.EvidenceHash) {
		return ErrReleaseNotAuthorized
	}
	return nil
}

type releaseCandidate struct {
	RequestID   string          `json:"request_id"`
	TenantID    string          `json:"tenant_id"`
	SiteID      string          `json:"site_id"`
	CameraID    string          `json:"camera_id"`
	Status      string          `json:"status"`
	RequestedBy string          `json:"requested_by"`
	ApproverIDs []string        `json:"approver_ids"`
	Geometry    json.RawMessage `json:"geometry"`
}

type releasePipeline struct {
	Stages []string `json:"stages"`
}

type releaseHIL struct {
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

func decodeReleaseCandidate(raw json.RawMessage) (releaseCandidate, error) {
	var candidate releaseCandidate
	if len(raw) == 0 || len(raw) > maxReleaseCandidateBytes || decodeReleaseJSON(raw, &candidate) != nil {
		return releaseCandidate{}, ErrReleaseNotAuthorized
	}
	for _, id := range []string{candidate.RequestID, candidate.TenantID, candidate.SiteID, candidate.CameraID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.Version() != 4 {
			return releaseCandidate{}, ErrReleaseNotAuthorized
		}
	}
	if candidate.Status != StatusApproved || strings.TrimSpace(candidate.RequestedBy) == "" || len(candidate.RequestedBy) > 128 || len(candidate.ApproverIDs) != 2 {
		return releaseCandidate{}, ErrReleaseNotAuthorized
	}
	seen := map[string]struct{}{}
	for _, approver := range candidate.ApproverIDs {
		if strings.TrimSpace(approver) == "" || len(approver) > 128 || approver == candidate.RequestedBy {
			return releaseCandidate{}, ErrReleaseNotAuthorized
		}
		if _, exists := seen[approver]; exists {
			return releaseCandidate{}, ErrReleaseNotAuthorized
		}
		seen[approver] = struct{}{}
	}
	if _, err := canonicalReleaseGeometry(candidate.Geometry); err != nil {
		return releaseCandidate{}, ErrReleaseNotAuthorized
	}
	return candidate, nil
}

func decodeReleasePipeline(raw json.RawMessage) (releasePipeline, error) {
	var pipeline releasePipeline
	if len(raw) == 0 || len(raw) > maxReleasePipelineBytes || decodeReleaseJSON(raw, &pipeline) != nil {
		return releasePipeline{}, ErrReleaseNotAuthorized
	}
	return pipeline, nil
}

func decodeReleaseHIL(raw json.RawMessage) (releaseHIL, error) {
	var hil releaseHIL
	if len(raw) == 0 || len(raw) > maxReleaseEvidenceBytes || decodeReleaseJSON(raw, &hil) != nil {
		return releaseHIL{}, ErrReleaseNotAuthorized
	}
	for _, id := range []string{hil.TestRunID, hil.DeviceID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.Version() != 4 {
			return releaseHIL{}, ErrReleaseNotAuthorized
		}
	}
	if !validReleaseHash(hil.CandidateHash) || !validReleaseHash(hil.EncoderBuildHash) || !validReleaseHash(hil.EncodedProbeHash) || strings.TrimSpace(hil.HarnessID) == "" || len(hil.HarnessID) > 128 || hil.ExecutionKind != "physical" || hil.AttestedAt.IsZero() || !hil.MaskBeforeEncode || !hil.EncoderBypassDenied || hil.RawFramesRetained || len(hil.Signature) != ed25519.SignatureSize {
		return releaseHIL{}, ErrReleaseNotAuthorized
	}
	return hil, nil
}

func decodeReleaseJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func sameReleaseCandidate(request Request, approvals []Approval, candidate releaseCandidate) bool {
	if candidate.RequestID != request.ID || candidate.TenantID != request.TenantID || candidate.SiteID != request.SiteID || candidate.CameraID != request.CameraID || candidate.RequestedBy != request.RequestedBy {
		return false
	}
	wantGeometry, err := canonicalReleaseGeometry(request.Geometry)
	gotGeometry, candidateErr := canonicalReleaseGeometry(candidate.Geometry)
	if err != nil || candidateErr != nil || string(wantGeometry) != string(gotGeometry) {
		return false
	}
	want := make([]string, len(approvals))
	for index, approval := range approvals {
		want[index] = approval.ApproverID
	}
	sort.Strings(want)
	got := append([]string(nil), candidate.ApproverIDs...)
	sort.Strings(got)
	return strings.Join(want, "\x00") == strings.Join(got, "\x00")
}

func strictReleasePipeline(stages []string) bool {
	return len(stages) == 3 && stages[0] == "decode" && stages[1] == "mask" && stages[2] == "encode"
}

func releaseCandidateHash(candidate releaseCandidate, pipeline releasePipeline) (string, error) {
	geometry, err := canonicalReleaseGeometry(candidate.Geometry)
	if err != nil {
		return "", err
	}
	payload := struct {
		Version     int             `json:"version"`
		RequestID   string          `json:"request_id"`
		TenantID    string          `json:"tenant_id"`
		SiteID      string          `json:"site_id"`
		CameraID    string          `json:"camera_id"`
		Status      string          `json:"status"`
		RequestedBy string          `json:"requested_by"`
		ApproverIDs []string        `json:"approver_ids"`
		Geometry    json.RawMessage `json:"geometry"`
		Stages      []string        `json:"stages"`
	}{1, candidate.RequestID, candidate.TenantID, candidate.SiteID, candidate.CameraID, candidate.Status, candidate.RequestedBy, candidate.ApproverIDs, geometry, pipeline.Stages}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func releaseHILPayload(hil releaseHIL) ([]byte, error) {
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
	}{1, hil.TestRunID, hil.DeviceID, hil.CandidateHash, strings.TrimSpace(hil.HarnessID), hil.ExecutionKind, hil.AttestedAt.UTC().Format(time.RFC3339Nano), hil.EncoderBuildHash, hil.EncodedProbeHash, hil.MaskBeforeEncode, hil.EncoderBypassDenied, hil.RawFramesRetained}
	return json.Marshal(payload)
}

func canonicalReleaseGeometry(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxReleaseCandidateBytes || !json.Valid(raw) {
		return nil, ErrReleaseNotAuthorized
	}
	var geometry struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &geometry); err != nil || geometry.Type != "Polygon" || len(geometry.Coordinates) == 0 {
		return nil, ErrReleaseNotAuthorized
	}
	vertices := 0
	for _, ring := range geometry.Coordinates {
		if len(ring) < 4 {
			return nil, ErrReleaseNotAuthorized
		}
		vertices += len(ring)
		if vertices > maxReleaseVertices || !sameReleasePoint(ring[0], ring[len(ring)-1]) {
			return nil, ErrReleaseNotAuthorized
		}
		for _, point := range ring {
			if len(point) != 2 || math.IsNaN(point[0]) || math.IsNaN(point[1]) || math.IsInf(point[0], 0) || math.IsInf(point[1], 0) || point[0] < 0 || point[0] > 1 || point[1] < 0 || point[1] > 1 {
				return nil, ErrReleaseNotAuthorized
			}
		}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, ErrReleaseNotAuthorized
	}
	return json.Marshal(decoded)
}

func sameReleasePoint(left, right []float64) bool {
	return len(left) == 2 && len(right) == 2 && left[0] == right[0] && left[1] == right[1]
}

func validReleaseHash(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
