package privacymasks

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const releaseDeviceID = "44444444-4444-4444-8444-444444444444"

func approvedReleaseRequest() Request {
	return Request{ID: "55555555-5555-4555-8555-555555555555", TenantID: tenantID, SiteID: siteID, CameraID: cameraID, Name: "Entry privacy", Geometry: command("requester").Geometry, Status: StatusApproved, RequestedBy: "requester"}
}

func approvedReleaseApprovals() []Approval {
	return []Approval{{ApproverID: "approver-a"}, {ApproverID: "approver-b"}}
}

func signedReleaseEvidence(t *testing.T, request Request, approvals []Approval, private ed25519.PrivateKey) ReleaseEvidence {
	t.Helper()
	candidate := releaseCandidate{RequestID: request.ID, TenantID: request.TenantID, SiteID: request.SiteID, CameraID: request.CameraID, Status: request.Status, RequestedBy: request.RequestedBy, ApproverIDs: []string{approvals[0].ApproverID, approvals[1].ApproverID}, Geometry: request.Geometry}
	pipeline := releasePipeline{Stages: []string{"decode", "mask", "encode"}}
	candidateHash, err := releaseCandidateHash(candidate, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	hil := releaseHIL{TestRunID: "66666666-6666-4666-8666-666666666666", DeviceID: releaseDeviceID, CandidateHash: candidateHash, HarnessID: "physical-harness-a", ExecutionKind: "physical", AttestedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC), EncoderBuildHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EncodedProbeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", MaskBeforeEncode: true, EncoderBypassDenied: true}
	payload, err := releaseHILPayload(hil)
	if err != nil {
		t.Fatal(err)
	}
	hil.Signature = ed25519.Sign(private, payload)
	candidateJSON, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	pipelineJSON, err := json.Marshal(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	hilJSON, err := json.Marshal(hil)
	if err != nil {
		t.Fatal(err)
	}
	return ReleaseEvidence{Candidate: candidateJSON, Pipeline: pipelineJSON, HILEvidence: hilJSON, CandidateHash: candidateHash, EvidenceHash: releaseEvidenceHash(t, hil)}
}

func releaseEvidenceHash(t *testing.T, hil releaseHIL) string {
	t.Helper()
	payload, err := releaseHILPayload(hil)
	if err != nil {
		t.Fatal(err)
	}
	return releaseHash(payload)
}

func releaseHash(payload []byte) string {
	// The verifier's hash is intentionally derived from the signed canonical payload.
	return func() string { sum := sha256.Sum256(payload); return hex.EncodeToString(sum[:]) }()
}

func TestTrustedReleaseAuthorizerRequiresMatchingApprovedMetadataAndSignedPhysicalHIL(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request, approvals := approvedReleaseRequest(), approvedReleaseApprovals()
	authorizer, err := NewTrustedReleaseAuthorizer(map[string]ed25519.PublicKey{"physical-harness-a": public})
	if err != nil {
		t.Fatal(err)
	}
	evidence := signedReleaseEvidence(t, request, approvals, private)
	if err := authorizer.Authorize(request, approvals, evidence); err != nil {
		t.Fatalf("authorized evidence rejected: %v", err)
	}
	for _, altered := range []ReleaseEvidence{
		func() ReleaseEvidence {
			copy := evidence
			copy.CandidateHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			return copy
		}(),
		func() ReleaseEvidence {
			copy := evidence
			copy.EvidenceHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			return copy
		}(),
		func() ReleaseEvidence {
			copy := evidence
			copy.Pipeline = json.RawMessage(`{"stages":["decode","encode","mask"]}`)
			return copy
		}(),
		func() ReleaseEvidence {
			copy := evidence
			copy.HILEvidence = json.RawMessage(`{"device_id":"` + releaseDeviceID + `"}`)
			return copy
		}(),
	} {
		if err := authorizer.Authorize(request, approvals, altered); !errors.Is(err, ErrReleaseNotAuthorized) {
			t.Fatalf("invalid release evidence must fail closed: %v", err)
		}
	}
	request.Status = StatusPending
	if err := authorizer.Authorize(request, approvals, evidence); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("pending request must fail: %v", err)
	}
	if _, err := NewTrustedReleaseAuthorizer(nil); !errors.Is(err, ErrReleaseNotAuthorized) {
		t.Fatalf("empty trust store must fail: %v", err)
	}
}
