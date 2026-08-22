package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

type recordingPrivacyMaskReleaseApplier struct {
	manifests []PrivacyMaskReleaseManifest
	err       error
}

func (a *recordingPrivacyMaskReleaseApplier) ApplyVerifiedRelease(_ context.Context, manifest PrivacyMaskReleaseManifest) error {
	if a.err != nil {
		return a.err
	}
	a.manifests = append(a.manifests, manifest)
	return nil
}

type recordingPrivacyMaskReleaseReporter struct {
	statuses []PrivacyMaskReleaseStatus
	err      error
}

func (r *recordingPrivacyMaskReleaseReporter) ReportPrivacyMaskRelease(_ context.Context, status PrivacyMaskReleaseStatus) error {
	r.statuses = append(r.statuses, status)
	return r.err
}

func signedReleaseManifest(t *testing.T, privateKey ed25519.PrivateKey) PrivacyMaskReleaseManifest {
	t.Helper()
	candidate := verifiedPrivacyMaskCandidate()
	pipeline := strictPreEncodePipeline()
	verification, err := VerifyPreEncodePrivacyMask(candidate, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	attestation := testHILAttestation(t, privateKey)
	attestation.DeviceID = "22222222-2222-4222-8222-222222222222"
	attestation.CandidateHash = verification.CandidateHash
	payload, err := canonicalHILPayload(attestation)
	if err != nil {
		t.Fatal(err)
	}
	attestation.Signature = ed25519.Sign(privateKey, payload)
	return PrivacyMaskReleaseManifest{
		ReleaseID: "33333333-3333-4333-8333-333333333333", DeviceID: attestation.DeviceID, Version: 1,
		Candidate: candidate, Pipeline: pipeline, HILEvidence: attestation,
	}
}

func TestControlledPrivacyMaskReleaseAcceptsOnlyVerifiedReleaseAndReportsSafeStatus(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{}
	reporter := &recordingPrivacyMaskReleaseReporter{}
	controller, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, reporter)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	if err := controller.Accept(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if len(applier.manifests) != 1 || len(reporter.statuses) != 1 || reporter.statuses[0].State != PrivacyMaskReleaseAccepted || reporter.statuses[0].ErrorCode != "" {
		t.Fatalf("accepted release state: applies=%d reports=%#v", len(applier.manifests), reporter.statuses)
	}
	accepted := controller.LastAccepted()
	if accepted == nil || accepted.ReleaseID != manifest.ReleaseID || accepted.Version != 1 || accepted.CandidateHash == "" || accepted.EvidenceHash == "" {
		t.Fatalf("last accepted status: %#v", accepted)
	}
	accepted.State = PrivacyMaskReleaseFailed
	if controller.LastAccepted().State != PrivacyMaskReleaseAccepted {
		t.Fatal("last accepted state must be returned as a copy")
	}
	next := signedReleaseManifest(t, privateKey)
	next.ReleaseID, next.Version = "44444444-4444-4444-8444-444444444444", 2
	if err := controller.Accept(context.Background(), next); err != nil || controller.LastAccepted().Version != 2 || len(applier.manifests) != 2 {
		t.Fatalf("strictly newer release must be accepted: last=%#v applies=%d err=%v", controller.LastAccepted(), len(applier.manifests), err)
	}
}

func TestControlledPrivacyMaskReleaseRejectsMismatchedEvidenceAndPreservesLastAccepted(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{}
	reporter := &recordingPrivacyMaskReleaseReporter{}
	controller, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, reporter)
	if err != nil {
		t.Fatal(err)
	}
	valid := signedReleaseManifest(t, privateKey)
	if err := controller.Accept(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	invalid := signedReleaseManifest(t, privateKey)
	invalid.ReleaseID = "44444444-4444-4444-8444-444444444444"
	invalid.HILEvidence.CandidateHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := controller.Accept(context.Background(), invalid); !errors.Is(err, ErrInvalidPrivacyMaskRelease) {
		t.Fatalf("mismatched HIL candidate hash must fail closed: %v", err)
	}
	if len(applier.manifests) != 1 || controller.LastAccepted().ReleaseID != valid.ReleaseID {
		t.Fatalf("invalid release must preserve last accepted: applies=%d last=%#v", len(applier.manifests), controller.LastAccepted())
	}
	if latest := reporter.statuses[len(reporter.statuses)-1]; latest.State != PrivacyMaskReleaseFailed || latest.ErrorCode != "verification_failed" {
		t.Fatalf("invalid release must emit only safe failure: %#v", latest)
	}
	if err := controller.Accept(context.Background(), valid); !errors.Is(err, ErrInvalidPrivacyMaskRelease) {
		t.Fatalf("replayed or stale release must fail closed: %v", err)
	}
	if latest := reporter.statuses[len(reporter.statuses)-1]; latest.ErrorCode != "stale_release" {
		t.Fatalf("stale release must emit a safe stale status: %#v", latest)
	}
}

func TestControlledPrivacyMaskReleaseReportsApplyAndReporterFailuresWithoutRollback(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{err: errors.New("metadata storage failed")}
	reporter := &recordingPrivacyMaskReleaseReporter{}
	controller, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Accept(context.Background(), signedReleaseManifest(t, privateKey)); err == nil {
		t.Fatal("apply failure must reach caller")
	}
	if controller.LastAccepted() != nil || len(reporter.statuses) != 1 || reporter.statuses[0].ErrorCode != "apply_failed" {
		t.Fatalf("failed apply must not change accepted release: last=%#v reports=%#v", controller.LastAccepted(), reporter.statuses)
	}

	applier.err = nil
	reporter.err = errors.New("status transport unavailable")
	if err := controller.Accept(context.Background(), signedReleaseManifest(t, privateKey)); err == nil {
		t.Fatal("reporting failure must reach caller")
	}
	if controller.LastAccepted() == nil || controller.LastAccepted().State != PrivacyMaskReleaseAccepted {
		t.Fatalf("successful local acceptance must not roll back for reporting failure: %#v", controller.LastAccepted())
	}
	if _, err := NewControlledPrivacyMaskRelease(nil, applier, reporter); !errors.Is(err, ErrInvalidPrivacyMaskRelease) {
		t.Fatalf("missing trusted keys must fail closed: %v", err)
	}
}
