package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
)

const (
	privacyMaskVerifierVersion = 1
	maxMaskGeometryBytes       = 64 << 10
	maxMaskVertices            = 128
)

var ErrInvalidPrivacyMaskVerification = errors.New("privacy mask pre-encode verification is invalid")

// PrivacyMaskCandidate contains configuration metadata only. It deliberately
// cannot carry a frame, stream URL, encoder handle, credential, or mask pixels.
type PrivacyMaskCandidate struct {
	RequestID   string          `json:"request_id"`
	TenantID    string          `json:"tenant_id"`
	SiteID      string          `json:"site_id"`
	CameraID    string          `json:"camera_id"`
	Status      string          `json:"status"`
	RequestedBy string          `json:"requested_by"`
	ApproverIDs []string        `json:"approver_ids"`
	Geometry    json.RawMessage `json:"geometry"`
}

type PipelineStage string

const (
	PipelineDecode PipelineStage = "decode"
	PipelineMask   PipelineStage = "mask"
	PipelineEncode PipelineStage = "encode"
)

// PreEncodePipeline is the locally declared ordering boundary. This foundation
// accepts only the closed decode -> mask -> encode path; configuration cannot
// insert storage, network, or an encoder before the mask stage.
type PreEncodePipeline struct {
	Stages []PipelineStage `json:"stages"`
}

// PreEncodeVerification is deterministic evidence that a candidate passed the
// static edge boundary. It is not a claim that pixels have been processed or
// that a hardware encoder has been validated; that requires HIL evidence.
type PreEncodeVerification struct {
	VerifierVersion int             `json:"verifier_version"`
	CandidateHash   string          `json:"candidate_hash"`
	Stages          []PipelineStage `json:"stages"`
}

// VerifyPreEncodePrivacyMask validates an approved, metadata-only candidate
// and proves its declared local ordering has no encode-before-mask path.
func VerifyPreEncodePrivacyMask(candidate PrivacyMaskCandidate, pipeline PreEncodePipeline) (PreEncodeVerification, error) {
	if err := validatePrivacyMaskCandidate(candidate); err != nil {
		return PreEncodeVerification{}, err
	}
	if !isStrictPreEncodePipeline(pipeline.Stages) {
		return PreEncodeVerification{}, ErrInvalidPrivacyMaskVerification
	}
	canonicalGeometry, err := canonicalGeometry(candidate.Geometry)
	if err != nil {
		return PreEncodeVerification{}, ErrInvalidPrivacyMaskVerification
	}
	canonical := struct {
		Version     int             `json:"version"`
		RequestID   string          `json:"request_id"`
		TenantID    string          `json:"tenant_id"`
		SiteID      string          `json:"site_id"`
		CameraID    string          `json:"camera_id"`
		Status      string          `json:"status"`
		RequestedBy string          `json:"requested_by"`
		ApproverIDs []string        `json:"approver_ids"`
		Geometry    json.RawMessage `json:"geometry"`
		Stages      []PipelineStage `json:"stages"`
	}{
		Version: privacyMaskVerifierVersion, RequestID: candidate.RequestID,
		TenantID: candidate.TenantID, SiteID: candidate.SiteID, CameraID: candidate.CameraID, Status: candidate.Status,
		RequestedBy: candidate.RequestedBy, ApproverIDs: append([]string(nil), candidate.ApproverIDs...),
		Geometry: canonicalGeometry, Stages: append([]PipelineStage(nil), pipeline.Stages...),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return PreEncodeVerification{}, ErrInvalidPrivacyMaskVerification
	}
	sum := sha256.Sum256(payload)
	return PreEncodeVerification{
		VerifierVersion: privacyMaskVerifierVersion,
		CandidateHash:   hex.EncodeToString(sum[:]),
		Stages:          append([]PipelineStage(nil), pipeline.Stages...),
	}, nil
}

func validatePrivacyMaskCandidate(candidate PrivacyMaskCandidate) error {
	for _, identifier := range []string{candidate.RequestID, candidate.TenantID, candidate.SiteID, candidate.CameraID} {
		parsed, err := uuid.Parse(identifier)
		if err != nil || parsed.Version() != 4 {
			return ErrInvalidPrivacyMaskVerification
		}
	}
	requester := strings.TrimSpace(candidate.RequestedBy)
	if candidate.Status != "approved" || requester == "" || len(requester) > 128 || len(candidate.ApproverIDs) != 2 {
		return ErrInvalidPrivacyMaskVerification
	}
	seen := make(map[string]struct{}, 2)
	for _, approver := range candidate.ApproverIDs {
		approver = strings.TrimSpace(approver)
		if approver == "" || len(approver) > 128 || approver == requester {
			return ErrInvalidPrivacyMaskVerification
		}
		if _, duplicate := seen[approver]; duplicate {
			return ErrInvalidPrivacyMaskVerification
		}
		seen[approver] = struct{}{}
	}
	_, err := canonicalGeometry(candidate.Geometry)
	return err
}

func isStrictPreEncodePipeline(stages []PipelineStage) bool {
	return len(stages) == 3 && stages[0] == PipelineDecode && stages[1] == PipelineMask && stages[2] == PipelineEncode
}

func canonicalGeometry(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxMaskGeometryBytes || !json.Valid(raw) {
		return nil, ErrInvalidPrivacyMaskVerification
	}
	var geometry struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &geometry); err != nil || geometry.Type != "Polygon" || len(geometry.Coordinates) == 0 {
		return nil, ErrInvalidPrivacyMaskVerification
	}
	vertices := 0
	for _, ring := range geometry.Coordinates {
		if len(ring) < 4 {
			return nil, ErrInvalidPrivacyMaskVerification
		}
		vertices += len(ring)
		if vertices > maxMaskVertices || !samePoint(ring[0], ring[len(ring)-1]) {
			return nil, ErrInvalidPrivacyMaskVerification
		}
		for _, point := range ring {
			if len(point) != 2 || !normalizedCoordinate(point[0]) || !normalizedCoordinate(point[1]) {
				return nil, ErrInvalidPrivacyMaskVerification
			}
		}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, ErrInvalidPrivacyMaskVerification
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, ErrInvalidPrivacyMaskVerification
	}
	return canonical, nil
}

func normalizedCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func samePoint(left, right []float64) bool {
	return len(left) == 2 && len(right) == 2 && left[0] == right[0] && left[1] == right[1]
}
