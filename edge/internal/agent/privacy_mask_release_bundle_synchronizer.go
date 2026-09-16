package agent

import (
	"context"
	"errors"
	"io"
)

var ErrInvalidPrivacyMaskReleaseBundleSynchronizer = errors.New("privacy mask release bundle synchronizer is invalid")

// PrivacyMaskReleaseBundleSource supplies the exact bounded release artifact
// bytes to the local loader. It is deliberately separate from the legacy
// manifest transport so a caller cannot reconstruct or weaken the signed
// artifact boundary after parsing.
type PrivacyMaskReleaseBundleSource interface {
	PullPrivacyMaskReleaseBundle(context.Context, int64) (io.Reader, error)
}

// PrivacyMaskReleaseBundleSynchronizer composes a source, strict bundle loader,
// and controlled release gate. It carries only metadata and never supplies a
// filesystem path, trust store, network credential, frame, or hardware handle.
type PrivacyMaskReleaseBundleSynchronizer struct {
	source PrivacyMaskReleaseBundleSource
	loader *PrivacyMaskReleaseLoader
	gate   *ControlledPrivacyMaskRelease
}

func NewPrivacyMaskReleaseBundleSynchronizer(source PrivacyMaskReleaseBundleSource, loader *PrivacyMaskReleaseLoader, gate *ControlledPrivacyMaskRelease) (*PrivacyMaskReleaseBundleSynchronizer, error) {
	if source == nil || loader == nil || gate == nil {
		return nil, ErrInvalidPrivacyMaskReleaseBundleSynchronizer
	}
	return &PrivacyMaskReleaseBundleSynchronizer{source: source, loader: loader, gate: gate}, nil
}

// Sync asks for at most one newer signed bundle, verifies it before release
// acceptance, and preserves the gate's previously accepted release on every
// source, parse, verification, or apply failure. A nil reader means there is no
// newer artifact; it is not an activation signal.
func (s *PrivacyMaskReleaseBundleSynchronizer) Sync(ctx context.Context) error {
	if s == nil || s.source == nil || s.loader == nil || s.gate == nil || ctx == nil {
		return ErrInvalidPrivacyMaskReleaseBundleSynchronizer
	}
	afterVersion := int64(0)
	if accepted := s.gate.LastAccepted(); accepted != nil {
		afterVersion = accepted.Version
	}
	payload, err := s.source.PullPrivacyMaskReleaseBundle(ctx, afterVersion)
	if err != nil {
		return err
	}
	if payload == nil {
		return nil
	}
	loaded, err := s.loader.Load(payload)
	if err != nil {
		return err
	}
	return s.gate.Accept(ctx, loaded.Manifest)
}
