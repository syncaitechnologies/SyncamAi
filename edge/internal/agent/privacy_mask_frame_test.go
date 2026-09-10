package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
)

const maskFrameCameraID = "99999999-9999-4999-8999-999999999999"

func TestPreAnalyticsPrivacyMaskBlacksConfiguredPixelsBeforeForwarding(t *testing.T) {
	var consumed RGB24Frame
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[0.5,0],[0.5,0.5],[0,0.5],[0,0]]]}`)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), MaskedFrameConsumerFunc(func(_ context.Context, frame RGB24Frame) error {
		consumed = frame
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, 4*4*3)
	for index := range pixels {
		pixels[index] = 255
	}
	if err := mask.Forward(context.Background(), RGB24Frame{CameraID: maskFrameCameraID, Width: 4, Height: 4, Pixels: pixels}); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			offset := (y*4 + x) * 3
			want := byte(255)
			if x < 2 && y < 2 {
				want = 0
			}
			if consumed.Pixels[offset] != want || consumed.Pixels[offset+1] != want || consumed.Pixels[offset+2] != want {
				t.Fatalf("pixel (%d,%d) was %v, want %d", x, y, consumed.Pixels[offset:offset+3], want)
			}
		}
	}
	if len(mask.CandidateHash()) != 64 {
		t.Fatalf("candidate hash was not preserved: %q", mask.CandidateHash())
	}
}

func TestPreAnalyticsPrivacyMaskKeepsHoleInteriorButMasksItsBoundary(t *testing.T) {
	var consumed RGB24Frame
	geometry := `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]],[[0.25,0.25],[0.75,0.25],[0.75,0.75],[0.25,0.75],[0.25,0.25]]]}`
	candidate := maskFrameCandidate(t, geometry)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), MaskedFrameConsumerFunc(func(_ context.Context, frame RGB24Frame) error {
		consumed = frame
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, 5*5*3)
	for index := range pixels {
		pixels[index] = 99
	}
	if err := mask.Forward(context.Background(), RGB24Frame{CameraID: maskFrameCameraID, Width: 5, Height: 5, Pixels: pixels}); err != nil {
		t.Fatal(err)
	}
	center := (2*5 + 2) * 3
	if consumed.Pixels[center] != 99 {
		t.Fatalf("hole interior must remain available to the local pipeline: %v", consumed.Pixels[center:center+3])
	}
	corner := 0
	if consumed.Pixels[corner] != 0 {
		t.Fatalf("outer mask was not applied: %v", consumed.Pixels[corner:corner+3])
	}
}

func TestPreAnalyticsPrivacyMaskRejectsInvalidFramesBeforeMutation(t *testing.T) {
	called := false
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), MaskedFrameConsumerFunc(func(context.Context, RGB24Frame) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range []RGB24Frame{
		{CameraID: "bad", Width: 1, Height: 1, Pixels: []byte{1, 2, 3}},
		{CameraID: maskFrameCameraID, Width: 2, Height: 2, Pixels: []byte{1, 2, 3}},
		{CameraID: "88888888-8888-4888-8888-888888888888", Width: 1, Height: 1, Pixels: []byte{1, 2, 3}},
	} {
		before := append([]byte(nil), frame.Pixels...)
		if err := mask.Forward(context.Background(), frame); !errors.Is(err, ErrInvalidPrivacyMaskFrame) {
			t.Fatalf("invalid frame must fail: %+v err=%v", frame, err)
		}
		if string(before) != string(frame.Pixels) {
			t.Fatalf("rejected frame was modified: before=%v after=%v", before, frame.Pixels)
		}
	}
	if called {
		t.Fatal("consumer must not receive a rejected frame")
	}
}

func TestPreAnalyticsPrivacyMaskDoesNotRestorePixelsAfterConsumerFailure(t *testing.T) {
	consumerError := errors.New("consumer unavailable")
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), MaskedFrameConsumerFunc(func(context.Context, RGB24Frame) error {
		return consumerError
	}))
	if err != nil {
		t.Fatal(err)
	}
	pixels := []byte{7, 8, 9}
	if err := mask.Forward(context.Background(), RGB24Frame{CameraID: maskFrameCameraID, Width: 1, Height: 1, Pixels: pixels}); !errors.Is(err, consumerError) {
		t.Fatalf("consumer failure must reach caller: %v", err)
	}
	if pixels[0] != 0 || pixels[1] != 0 || pixels[2] != 0 {
		t.Fatalf("failure restored unmasked pixels: %v", pixels)
	}
}

func TestPreAnalyticsPrivacyMaskRejectsMismatchedHardwareActivation(t *testing.T) {
	approved := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	different := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[0.5,0],[0.5,1],[0,1],[0,0]]]}`)
	if _, err := NewPreAnalyticsPrivacyMask(different, maskFrameAdapter(t, approved), MaskedFrameConsumerFunc(func(context.Context, RGB24Frame) error { return nil })); !errors.Is(err, ErrInvalidPrivacyMaskFrame) {
		t.Fatalf("mismatched activation must fail: %v", err)
	}
}

func TestPreAnalyticsPrivacyMaskFailsClosedAfterActiveReleaseChanges(t *testing.T) {
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	adapter := maskFrameAdapter(t, candidate)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, adapter, MaskedFrameConsumerFunc(func(context.Context, RGB24Frame) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	adapter.active.Version++
	adapter.mu.Unlock()
	pixels := []byte{1, 2, 3}
	if err := mask.Forward(context.Background(), RGB24Frame{CameraID: maskFrameCameraID, Width: 1, Height: 1, Pixels: pixels}); !errors.Is(err, ErrInvalidPrivacyMaskFrame) {
		t.Fatalf("stale renderer must fail closed: %v", err)
	}
	if pixels[0] != 1 || pixels[1] != 2 || pixels[2] != 3 {
		t.Fatalf("stale renderer changed rejected frame: %v", pixels)
	}
}

func maskFrameCandidate(t *testing.T, geometry string) PrivacyMaskCandidate {
	t.Helper()
	if !json.Valid([]byte(geometry)) {
		t.Fatalf("invalid test geometry: %s", geometry)
	}
	return PrivacyMaskCandidate{
		RequestID:   "11111111-1111-4111-8111-111111111111",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		SiteID:      "33333333-3333-4333-8333-333333333333",
		CameraID:    maskFrameCameraID,
		Status:      "approved",
		RequestedBy: "requester",
		ApproverIDs: []string{"approver-a", "approver-b"},
		Geometry:    json.RawMessage(geometry),
	}
}

type frameMaskExecutor struct{}

func (frameMaskExecutor) ActivatePreEncodePrivacyMask(context.Context, HardwarePrivacyMaskActivation) error {
	return nil
}

func maskFrameAdapter(t *testing.T, candidate PrivacyMaskCandidate) *HardwareBoundPrivacyMaskAdapter {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	profile := hardwarePrivacyMaskProfile()
	adapter, err := NewHardwareBoundPrivacyMaskAdapter(profile, publicKey, frameMaskExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyPreEncodePrivacyMask(candidate, PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineMask, PipelineEncode}})
	if err != nil {
		t.Fatal(err)
	}
	attestation := testHILAttestation(t, privateKey)
	attestation.DeviceID = profile.DeviceID
	attestation.CandidateHash = verification.CandidateHash
	payload, err := canonicalHILPayload(attestation)
	if err != nil {
		t.Fatal(err)
	}
	attestation.Signature = ed25519.Sign(privateKey, payload)
	manifest := PrivacyMaskReleaseManifest{
		ReleaseID:   "44444444-4444-4444-8444-444444444444",
		DeviceID:    profile.DeviceID,
		Version:     1,
		Candidate:   candidate,
		Pipeline:    PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineMask, PipelineEncode}},
		HILEvidence: attestation,
	}
	if err := adapter.ApplyVerifiedRelease(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	return adapter
}
