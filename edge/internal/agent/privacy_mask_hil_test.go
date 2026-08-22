package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func testHILAttestation(t *testing.T, privateKey ed25519.PrivateKey) PrivacyMaskHILAttestation {
	t.Helper()
	attestation := PrivacyMaskHILAttestation{
		TestRunID: "11111111-1111-4111-8111-111111111111", DeviceID: "22222222-2222-4222-8222-222222222222",
		CandidateHash: strings.Repeat("a", 64), HarnessID: "physical-rig-01", ExecutionKind: "physical",
		AttestedAt:       time.Date(2026, 8, 22, 15, 0, 0, 0, time.FixedZone("IST", 19800)),
		EncoderBuildHash: strings.Repeat("b", 64), EncodedProbeHash: strings.Repeat("c", 64),
		MaskBeforeEncode: true, EncoderBypassDenied: true, RawFramesRetained: false,
	}
	payload, err := canonicalHILPayload(attestation)
	if err != nil {
		t.Fatal(err)
	}
	attestation.Signature = ed25519.Sign(privateKey, payload)
	return attestation
}

func TestVerifyPrivacyMaskHILEvidenceAcceptsTrustedPhysicalAttestation(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attestation := testHILAttestation(t, privateKey)
	result, err := VerifyPrivacyMaskHILEvidence(attestation, map[string]ed25519.PublicKey{"physical-rig-01": publicKey})
	if err != nil || result.TestRunID != attestation.TestRunID || result.CandidateHash != attestation.CandidateHash || len(result.EvidenceHash) != 64 {
		t.Fatalf("verify HIL attestation: %#v %v", result, err)
	}
	second, err := VerifyPrivacyMaskHILEvidence(attestation, map[string]ed25519.PublicKey{"physical-rig-01": publicKey})
	if err != nil || second.EvidenceHash != result.EvidenceHash {
		t.Fatalf("HIL verification must be deterministic: %#v %v", second, err)
	}
}

func TestVerifyPrivacyMaskHILEvidenceFailsClosed(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted := map[string]ed25519.PublicKey{"physical-rig-01": publicKey}
	for _, test := range []struct {
		name   string
		mutate func(*PrivacyMaskHILAttestation)
	}{
		{name: "simulation", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.ExecutionKind = "simulation" }},
		{name: "bypass not denied", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.EncoderBypassDenied = false }},
		{name: "mask not before encoding", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.MaskBeforeEncode = false }},
		{name: "raw frames retained", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.RawFramesRetained = true }},
		{name: "malformed candidate hash", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.CandidateHash = "not-a-hash" }},
		{name: "tampered signed field", mutate: func(attestation *PrivacyMaskHILAttestation) {
			attestation.DeviceID = "33333333-3333-4333-8333-333333333333"
		}},
		{name: "invalid signature", mutate: func(attestation *PrivacyMaskHILAttestation) { attestation.Signature[0] ^= 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			attestation := testHILAttestation(t, privateKey)
			test.mutate(&attestation)
			if _, err := VerifyPrivacyMaskHILEvidence(attestation, trusted); err == nil {
				t.Fatal("invalid HIL evidence must fail closed")
			}
		})
	}
	attestation := testHILAttestation(t, privateKey)
	if _, err := VerifyPrivacyMaskHILEvidence(attestation, nil); err == nil {
		t.Fatal("untrusted harness must fail closed")
	}
}

func TestHILAttestationCanonicalizationUsesUTCAndHashesAreStrict(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attestation := testHILAttestation(t, privateKey)
	payload, err := canonicalHILPayload(attestation)
	if err != nil || !strings.Contains(string(payload), "2026-08-22T09:30:00Z") {
		t.Fatalf("canonical HIL payload must normalize time to UTC: %s %v", payload, err)
	}
	if validSHA256Hex("bad") || !validSHA256Hex(strings.Repeat("d", 64)) {
		t.Fatal("HIL evidence hashes must be strict SHA-256 hex")
	}
	if _, err := VerifyPrivacyMaskHILEvidence(attestation, map[string]ed25519.PublicKey{"physical-rig-01": append(ed25519.PublicKey(nil), publicKey[:31]...)}); err == nil {
		t.Fatal("wrong-sized trusted key must fail closed")
	}
}
