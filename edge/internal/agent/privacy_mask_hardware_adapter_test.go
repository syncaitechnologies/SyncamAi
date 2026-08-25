package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

type recordingPrivacyMaskHardwareExecutor struct {
	activations []HardwarePrivacyMaskActivation
	err         error
}

func (e *recordingPrivacyMaskHardwareExecutor) ActivatePreEncodePrivacyMask(_ context.Context, activation HardwarePrivacyMaskActivation) error {
	if e.err != nil {
		return e.err
	}
	e.activations = append(e.activations, activation)
	return nil
}

func hardwarePrivacyMaskProfile() PrivacyMaskHardwareProfile {
	return PrivacyMaskHardwareProfile{
		ProfileID: "approved-camera-encoder-profile-01",
		DeviceID:  "22222222-2222-4222-8222-222222222222",
		HarnessID: "physical-rig-01",
	}
}

func TestHardwareBoundPrivacyMaskAdapterActivatesOnlyMatchingPhysicalProfile(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingPrivacyMaskHardwareExecutor{}
	adapter, err := NewHardwareBoundPrivacyMaskAdapter(hardwarePrivacyMaskProfile(), publicKey, executor)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	if err := adapter.ApplyVerifiedRelease(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if len(executor.activations) != 1 {
		t.Fatalf("expected one hardware activation, got %d", len(executor.activations))
	}
	activation := executor.activations[0]
	if activation.ProfileID != hardwarePrivacyMaskProfile().ProfileID || activation.ReleaseID != manifest.ReleaseID || activation.DeviceID != manifest.DeviceID || activation.CandidateHash == "" || len(activation.Pipeline) != 3 {
		t.Fatalf("unexpected bounded activation: %#v", activation)
	}
	active := adapter.ActiveRelease()
	active.Pipeline[0] = PipelineEncode
	if adapter.ActiveRelease().Pipeline[0] != PipelineDecode {
		t.Fatal("active release must be returned as a copy")
	}
}

func TestHardwareBoundPrivacyMaskAdapterFailsClosedAndPreservesPriorRelease(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingPrivacyMaskHardwareExecutor{}
	adapter, err := NewHardwareBoundPrivacyMaskAdapter(hardwarePrivacyMaskProfile(), publicKey, executor)
	if err != nil {
		t.Fatal(err)
	}
	valid := signedReleaseManifest(t, privateKey)
	if err := adapter.ApplyVerifiedRelease(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*PrivacyMaskReleaseManifest)
	}{
		{name: "other device", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.DeviceID = "33333333-3333-4333-8333-333333333333" }},
		{name: "other harness", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.HILEvidence.HarnessID = "physical-rig-02" }},
		{name: "nonphysical evidence", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.HILEvidence.ExecutionKind = "simulation" }},
		{name: "encoder bypass", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.HILEvidence.EncoderBypassDenied = false }},
		{name: "raw frames retained", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.HILEvidence.RawFramesRetained = true }},
		{name: "wrong pipeline", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.Pipeline.Stages = []PipelineStage{PipelineDecode, PipelineEncode, PipelineMask} }},
		{name: "tampered HIL signature", mutate: func(manifest *PrivacyMaskReleaseManifest) { manifest.HILEvidence.Signature[0] ^= 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := signedReleaseManifest(t, privateKey)
			candidate.ReleaseID, candidate.Version = "44444444-4444-4444-8444-444444444444", 2
			test.mutate(&candidate)
			if err := adapter.ApplyVerifiedRelease(context.Background(), candidate); !errors.Is(err, ErrInvalidPrivacyMaskHardwareAdapter) {
				t.Fatalf("invalid adapter candidate must fail closed: %v", err)
			}
			if adapter.ActiveRelease().ReleaseID != valid.ReleaseID || len(executor.activations) != 1 {
				t.Fatalf("invalid candidate changed active release: active=%#v activations=%d", adapter.ActiveRelease(), len(executor.activations))
			}
		})
	}
	next := signedReleaseManifest(t, privateKey)
	next.ReleaseID, next.Version = "44444444-4444-4444-8444-444444444444", 2
	executor.err = errors.New("hardware adapter unavailable")
	if err := adapter.ApplyVerifiedRelease(context.Background(), next); err == nil {
		t.Fatal("hardware error must reach caller")
	}
	if adapter.ActiveRelease().ReleaseID != valid.ReleaseID || len(executor.activations) != 1 {
		t.Fatalf("hardware failure changed active release: active=%#v activations=%d", adapter.ActiveRelease(), len(executor.activations))
	}
}

func TestHardwareBoundPrivacyMaskAdapterReconcilesOnlyExactReleaseReplay(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingPrivacyMaskHardwareExecutor{}
	adapter, err := NewHardwareBoundPrivacyMaskAdapter(hardwarePrivacyMaskProfile(), publicKey, executor)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	if err := adapter.ApplyVerifiedRelease(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ApplyVerifiedRelease(context.Background(), manifest); err != nil {
		t.Fatalf("exact verified replay must be a safe no-op: %v", err)
	}
	if len(executor.activations) != 1 {
		t.Fatalf("exact replay must not activate hardware again, got %d calls", len(executor.activations))
	}

	conflicting := signedReleaseManifest(t, privateKey)
	conflicting.Version = manifest.Version
	if err := adapter.ApplyVerifiedRelease(context.Background(), conflicting); !errors.Is(err, ErrInvalidPrivacyMaskHardwareAdapter) {
		t.Fatalf("different release at active version must fail closed: %v", err)
	}
	if len(executor.activations) != 1 || adapter.ActiveRelease().ReleaseID != manifest.ReleaseID {
		t.Fatal("conflicting replay changed the active hardware release")
	}
}

func TestHardwareBoundPrivacyMaskAdapterRejectsInvalidProfiles(t *testing.T) {
	executor := &recordingPrivacyMaskHardwareExecutor{}
	trustedKey := make(ed25519.PublicKey, ed25519.PublicKeySize)
	for _, profile := range []PrivacyMaskHardwareProfile{
		{},
		{ProfileID: " profile", DeviceID: hardwarePrivacyMaskProfile().DeviceID, HarnessID: "physical-rig-01"},
		{ProfileID: "profile", DeviceID: "not-a-uuid", HarnessID: "physical-rig-01"},
		{ProfileID: "profile", DeviceID: hardwarePrivacyMaskProfile().DeviceID, HarnessID: " "},
	} {
		if _, err := NewHardwareBoundPrivacyMaskAdapter(profile, trustedKey, executor); !errors.Is(err, ErrInvalidPrivacyMaskHardwareAdapter) {
			t.Fatalf("invalid profile must fail closed: %#v, %v", profile, err)
		}
	}
	if _, err := NewHardwareBoundPrivacyMaskAdapter(hardwarePrivacyMaskProfile(), nil, executor); !errors.Is(err, ErrInvalidPrivacyMaskHardwareAdapter) {
		t.Fatalf("missing trusted HIL key must fail closed: %v", err)
	}
	if _, err := NewHardwareBoundPrivacyMaskAdapter(hardwarePrivacyMaskProfile(), trustedKey, nil); !errors.Is(err, ErrInvalidPrivacyMaskHardwareAdapter) {
		t.Fatalf("missing executor must fail closed: %v", err)
	}
}
