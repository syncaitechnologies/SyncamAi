package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

type privacyMaskReleaseBundleSourceFake struct {
	payload []byte
	err     error
	after   []int64
}

func (f *privacyMaskReleaseBundleSourceFake) PullPrivacyMaskReleaseBundle(_ context.Context, afterVersion int64) (io.Reader, error) {
	f.after = append(f.after, afterVersion)
	if f.err != nil {
		return nil, f.err
	}
	if f.payload == nil {
		return nil, nil
	}
	return bytes.NewReader(f.payload), nil
}

func TestPrivacyMaskReleaseBundleSynchronizerLoadsBeforeControlledAcceptance(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	source := &privacyMaskReleaseBundleSourceFake{payload: encodedPrivacyMaskReleaseBundle(t, manifest)}
	loader, err := NewPrivacyMaskReleaseLoader(privacyMaskReleaseScope(), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{}
	reporter := &recordingPrivacyMaskReleaseReporter{}
	gate, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, reporter)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewPrivacyMaskReleaseBundleSynchronizer(source, loader, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(source.after) != 1 || source.after[0] != 0 || len(applier.manifests) != 1 || gate.LastAccepted() == nil {
		t.Fatalf("verified bundle was not accepted exactly once: after=%v manifests=%d accepted=%#v", source.after, len(applier.manifests), gate.LastAccepted())
	}
	source.payload = nil
	if err := synchronizer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(source.after) != 2 || source.after[1] != manifest.Version || len(applier.manifests) != 1 {
		t.Fatalf("empty poll must preserve the accepted release: after=%v manifests=%d", source.after, len(applier.manifests))
	}
}

func TestPrivacyMaskReleaseBundleSynchronizerRejectsUnverifiedArtifactWithoutBypass(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := signedReleaseManifest(t, privateKey)
	payload := encodedPrivacyMaskReleaseBundle(t, manifest)
	payload[len(payload)-2] ^= 1
	source := &privacyMaskReleaseBundleSourceFake{payload: payload}
	loader, err := NewPrivacyMaskReleaseLoader(privacyMaskReleaseScope(), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	applier := &recordingPrivacyMaskReleaseApplier{}
	reporter := &recordingPrivacyMaskReleaseReporter{}
	gate, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, reporter)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewPrivacyMaskReleaseBundleSynchronizer(source, loader, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(context.Background()); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundle) {
		t.Fatalf("tampered bundle must fail before release acceptance: %v", err)
	}
	if len(applier.manifests) != 0 || gate.LastAccepted() != nil {
		t.Fatalf("unverified bundle bypassed the release boundary: manifests=%d accepted=%#v", len(applier.manifests), gate.LastAccepted())
	}
}

func TestPrivacyMaskReleaseBundleSynchronizerRejectsInvalidComposition(t *testing.T) {
	validSource := &privacyMaskReleaseBundleSourceFake{}
	if _, err := NewPrivacyMaskReleaseBundleSynchronizer(nil, &PrivacyMaskReleaseLoader{}, &ControlledPrivacyMaskRelease{}); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundleSynchronizer) {
		t.Fatalf("missing source must fail closed: %v", err)
	}
	if _, err := NewPrivacyMaskReleaseBundleSynchronizer(validSource, nil, &ControlledPrivacyMaskRelease{}); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundleSynchronizer) {
		t.Fatalf("missing loader must fail closed: %v", err)
	}
	if _, err := NewPrivacyMaskReleaseBundleSynchronizer(validSource, &PrivacyMaskReleaseLoader{}, nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundleSynchronizer) {
		t.Fatalf("missing gate must fail closed: %v", err)
	}
	synchronizer := &PrivacyMaskReleaseBundleSynchronizer{}
	if err := synchronizer.Sync(nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseBundleSynchronizer) {
		t.Fatalf("invalid synchronizer must fail closed: %v", err)
	}
}
