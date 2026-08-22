package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func verifiedPrivacyMaskCandidate() PrivacyMaskCandidate {
	return PrivacyMaskCandidate{
		RequestID: "11111111-1111-4111-8111-111111111111", TenantID: "22222222-2222-4222-8222-222222222222",
		SiteID: "33333333-3333-4333-8333-333333333333", CameraID: "44444444-4444-4444-8444-444444444444",
		Status: "approved", RequestedBy: "requester", ApproverIDs: []string{"approver-a", "approver-b"},
		Geometry: json.RawMessage(`{"coordinates":[[[0,0],[1,0],[1,1],[0,0]]],"type":"Polygon"}`),
	}
}

func strictPreEncodePipeline() PreEncodePipeline {
	return PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineMask, PipelineEncode}}
}

func TestVerifyPreEncodePrivacyMaskProducesDeterministicMetadataOnlyEvidence(t *testing.T) {
	candidate := verifiedPrivacyMaskCandidate()
	first, err := VerifyPreEncodePrivacyMask(candidate, strictPreEncodePipeline())
	if err != nil || first.VerifierVersion != 1 || len(first.CandidateHash) != 64 {
		t.Fatalf("verify privacy mask: %#v %v", first, err)
	}
	candidate.Geometry = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`)
	second, err := VerifyPreEncodePrivacyMask(candidate, strictPreEncodePipeline())
	if err != nil || second.CandidateHash != first.CandidateHash {
		t.Fatalf("equivalent canonical geometry must verify deterministically: %#v %v", second, err)
	}
	first.Stages[0] = PipelineEncode
	if second.Stages[0] != PipelineDecode {
		t.Fatal("verification evidence must not share mutable pipeline state")
	}
}

func TestVerifyPreEncodePrivacyMaskFailsClosedOnApprovalPipelineAndGeometryViolations(t *testing.T) {
	for _, test := range []struct {
		name      string
		candidate PrivacyMaskCandidate
		pipeline  PreEncodePipeline
	}{
		{name: "requester approval", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.ApproverIDs[1] = "requester"
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "duplicate approval", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.ApproverIDs[1] = "approver-a"
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "too few approvals", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.ApproverIDs = candidate.ApproverIDs[:1]
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "pending request", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.Status = "pending"
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "invalid identifier", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.CameraID = "invalid"
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "encode before mask", candidate: verifiedPrivacyMaskCandidate(), pipeline: PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineEncode, PipelineMask}}},
		{name: "extra pipeline stage", candidate: verifiedPrivacyMaskCandidate(), pipeline: PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineMask, PipelineEncode, "network"}}},
		{name: "unclosed geometry", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.Geometry = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1]]]}`)
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "out of bounds geometry", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.Geometry = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[2,0],[1,1],[0,0]]]}`)
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
		{name: "oversized identity", candidate: func() PrivacyMaskCandidate {
			candidate := verifiedPrivacyMaskCandidate()
			candidate.RequestedBy = strings.Repeat("a", 129)
			return candidate
		}(), pipeline: strictPreEncodePipeline()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := VerifyPreEncodePrivacyMask(test.candidate, test.pipeline); !errors.Is(err, ErrInvalidPrivacyMaskVerification) {
				t.Fatalf("invalid privacy mask candidate must fail closed: %v", err)
			}
		})
	}
}

func TestPrivacyMaskGeometryValidationIsBounded(t *testing.T) {
	candidate := verifiedPrivacyMaskCandidate()
	candidate.Geometry = json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}` + strings.Repeat(" ", maxMaskGeometryBytes))
	if _, err := VerifyPreEncodePrivacyMask(candidate, strictPreEncodePipeline()); !errors.Is(err, ErrInvalidPrivacyMaskVerification) {
		t.Fatalf("oversized geometry must fail closed: %v", err)
	}
	if normalizedCoordinate(-0.01) || normalizedCoordinate(1.01) || !normalizedCoordinate(0.5) {
		t.Fatal("normalized coordinate bounds must remain strict")
	}
}
