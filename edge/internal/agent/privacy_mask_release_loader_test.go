package agent

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func privacyMaskReleaseScope() PrivacyMaskReleaseScope {
	candidate := verifiedPrivacyMaskCandidate()
	return PrivacyMaskReleaseScope{
		TenantID: candidate.TenantID,
		SiteID:   candidate.SiteID,
		CameraID: candidate.CameraID,
		Profile:  hardwarePrivacyMaskProfile(),
	}
}

func encodedPrivacyMaskReleaseBundle(t *testing.T, manifest PrivacyMaskReleaseManifest) []byte {
	t.Helper()
	payload, err := json.Marshal(PrivacyMaskReleaseBundle{
		SchemaVersion: privacyMaskReleaseBundleSchemaVersion,
		ProfileID:     hardwarePrivacyMaskProfile().ProfileID,
		Release:       manifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestPrivacyMaskReleaseLoaderLoadsOnlyVerifiedScopedMetadata(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := NewPrivacyMaskReleaseLoader(privacyMaskReleaseScope(), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	loaded, err := loader.Load(bytes.NewReader(encodedPrivacyMaskReleaseBundle(t, manifest)))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.ReleaseID != manifest.ReleaseID || loaded.Manifest.Version != manifest.Version || loaded.ProfileID != hardwarePrivacyMaskProfile().ProfileID || len(loaded.CandidateHash) != 64 || len(loaded.EvidenceHash) != 64 {
		t.Fatalf("unexpected loaded release: %#v", loaded)
	}

	loaded.Manifest.Candidate.ApproverIDs[0] = "changed"
	loaded.Manifest.Candidate.Geometry[0] = 'x'
	loaded.Manifest.Pipeline.Stages[0] = PipelineEncode
	loaded.Manifest.HILEvidence.Signature[0] ^= 1
	reloaded, err := loader.Load(bytes.NewReader(encodedPrivacyMaskReleaseBundle(t, manifest)))
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Manifest.Candidate.ApproverIDs[0] != manifest.Candidate.ApproverIDs[0] || !bytes.Equal(reloaded.Manifest.Candidate.Geometry, manifest.Candidate.Geometry) || reloaded.Manifest.Pipeline.Stages[0] != PipelineDecode || !bytes.Equal(reloaded.Manifest.HILEvidence.Signature, manifest.HILEvidence.Signature) {
		t.Fatal("loaded release must not retain mutable state from a prior result")
	}
	if reloaded.CandidateHash != loaded.CandidateHash || reloaded.EvidenceHash != loaded.EvidenceHash {
		t.Fatal("repeated loading must produce deterministic verification hashes")
	}
}

func TestPrivacyMaskReleaseLoaderRejectsMalformedAndUnboundedInput(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := NewPrivacyMaskReleaseLoader(privacyMaskReleaseScope(), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	valid := encodedPrivacyMaskReleaseBundle(t, signedReleaseManifest(t, privateKey))
	duplicate := bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
	nestedDuplicate := bytes.Replace(valid, []byte(`"type":"Polygon"`), []byte(`"type":"Polygon","type":"Polygon"`), 1)
	unknown := bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":1,"unexpected":true`), 1)
	nestedUnknown := bytes.Replace(valid, []byte(`"type":"Polygon"`), []byte(`"type":"Polygon","unexpected":true`), 1)

	for _, test := range []struct {
		name    string
		payload []byte
	}{
		{name: "empty", payload: nil},
		{name: "malformed", payload: []byte(`{"schema_version":`)},
		{name: "duplicate field", payload: duplicate},
		{name: "nested duplicate field", payload: nestedDuplicate},
		{name: "unknown field", payload: unknown},
		{name: "nested unknown field", payload: nestedUnknown},
		{name: "trailing document", payload: append(append([]byte(nil), valid...), []byte(` {}`)...)},
		{name: "oversized", payload: bytes.Repeat([]byte{' '}, maxPrivacyMaskReleaseBundleBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loader.Load(bytes.NewReader(test.payload)); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
				t.Fatalf("invalid bundle must fail closed: %v", err)
			}
		})
	}
	if _, err := loader.Load(nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
		t.Fatalf("nil reader must fail closed: %v", err)
	}
}

func TestPrivacyMaskReleaseLoaderRejectsScopeAndTrustDrift(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := NewPrivacyMaskReleaseLoader(privacyMaskReleaseScope(), publicKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*PrivacyMaskReleaseBundle)
	}{
		{name: "schema", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.SchemaVersion++ }},
		{name: "profile", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.ProfileID = "other-profile" }},
		{name: "tenant", mutate: func(bundle *PrivacyMaskReleaseBundle) {
			bundle.Release.Candidate.TenantID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{name: "site", mutate: func(bundle *PrivacyMaskReleaseBundle) {
			bundle.Release.Candidate.SiteID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{name: "camera", mutate: func(bundle *PrivacyMaskReleaseBundle) {
			bundle.Release.Candidate.CameraID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{name: "device", mutate: func(bundle *PrivacyMaskReleaseBundle) {
			bundle.Release.DeviceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{name: "HIL device", mutate: func(bundle *PrivacyMaskReleaseBundle) {
			bundle.Release.HILEvidence.DeviceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}},
		{name: "harness", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.Release.HILEvidence.HarnessID = "other-harness" }},
		{name: "nonphysical evidence", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.Release.HILEvidence.ExecutionKind = "simulation" }},
		{name: "tampered signature", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.Release.HILEvidence.Signature[0] ^= 1 }},
		{name: "invalid release", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.Release.ReleaseID = "not-a-uuid" }},
		{name: "invalid version", mutate: func(bundle *PrivacyMaskReleaseBundle) { bundle.Release.Version = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle := PrivacyMaskReleaseBundle{
				SchemaVersion: privacyMaskReleaseBundleSchemaVersion,
				ProfileID:     hardwarePrivacyMaskProfile().ProfileID,
				Release:       signedReleaseManifest(t, privateKey),
			}
			test.mutate(&bundle)
			payload, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loader.Load(bytes.NewReader(payload)); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
				t.Fatalf("scope or trust drift must fail closed: %v", err)
			}
		})
	}
}

func TestPrivacyMaskReleaseLoaderRejectsInvalidConfiguration(t *testing.T) {
	trustedKey := make(ed25519.PublicKey, ed25519.PublicKeySize)
	valid := privacyMaskReleaseScope()
	for _, scope := range []PrivacyMaskReleaseScope{
		{},
		{TenantID: "not-a-uuid", SiteID: valid.SiteID, CameraID: valid.CameraID, Profile: valid.Profile},
		{TenantID: valid.TenantID, SiteID: valid.SiteID, CameraID: valid.CameraID, Profile: PrivacyMaskHardwareProfile{ProfileID: strings.Repeat("p", maxPrivacyMaskHardwareProfileIDLength+1), DeviceID: valid.Profile.DeviceID, HarnessID: valid.Profile.HarnessID}},
	} {
		if _, err := NewPrivacyMaskReleaseLoader(scope, trustedKey); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
			t.Fatalf("invalid loader scope must fail closed: %#v, %v", scope, err)
		}
	}
	if _, err := NewPrivacyMaskReleaseLoader(valid, nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
		t.Fatalf("missing trusted key must fail closed: %v", err)
	}
}
