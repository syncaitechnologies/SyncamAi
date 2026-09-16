package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

type runtimePrivacyMaskReleaseApplier struct {
	applied chan PrivacyMaskReleaseManifest
}

func (a *runtimePrivacyMaskReleaseApplier) ApplyVerifiedRelease(_ context.Context, manifest PrivacyMaskReleaseManifest) error {
	select {
	case a.applied <- manifest:
	default:
	}
	return nil
}

func TestEdgeRuntimeRunsVerifiedPrivacyMaskBundleComposition(t *testing.T) {
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
	applier := &runtimePrivacyMaskReleaseApplier{applied: make(chan PrivacyMaskReleaseManifest, 1)}
	gate, err := NewControlledPrivacyMaskRelease(map[string]ed25519.PublicKey{"physical-rig-01": publicKey}, applier, &recordingPrivacyMaskReleaseReporter{})
	if err != nil {
		t.Fatal(err)
	}
	synchronizer, err := NewPrivacyMaskReleaseBundleSynchronizer(source, loader, gate)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewPrivacyMaskReleaseWorker(synchronizer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewPrivacyMaskReleaseSupervisor(worker, PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Millisecond, MaxDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := &runtimeHeartbeatFake{}
	configuration := &runtimeConfigurationFake{started: make(chan struct{})}
	runtime, err := NewEdgeRuntimeWithPrivacyMaskRelease(EdgeRuntimeConfig{FirmwareVersion: "1.0.0", HeartbeatInterval: time.Millisecond, ConfigurationInterval: time.Millisecond}, heartbeat, configuration, runtimeSpoolFake{}, supervisor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, func(RuntimeEvent) {}) }()
	select {
	case applied := <-applier.applied:
		if applied.ReleaseID != manifest.ReleaseID || applied.Version != manifest.Version {
			t.Fatalf("runtime released unexpected metadata: %#v", applied)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not reach the verified release applier")
	}
	accepted := gate.LastAccepted()
	if accepted == nil || accepted.ReleaseID != manifest.ReleaseID || accepted.Version != manifest.Version {
		t.Fatalf("runtime did not retain the accepted release status: %#v", accepted)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation: %v", err)
	}
}
