package agent

import (
	"context"
)

// PrivacyMaskReleaseSynchronizer connects the dedicated mTLS client to the
// controlled local release gate. It is deliberately separate from generic
// configuration synchronization and never handles media or encoder controls.
type PrivacyMaskReleaseSynchronizer struct {
	client *PrivacyMaskReleaseClient
	gate   *ControlledPrivacyMaskRelease
}

func NewPrivacyMaskReleaseSynchronizer(client *PrivacyMaskReleaseClient, gate *ControlledPrivacyMaskRelease) (*PrivacyMaskReleaseSynchronizer, error) {
	if client == nil || gate == nil {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	return &PrivacyMaskReleaseSynchronizer{client: client, gate: gate}, nil
}

// Sync pulls at most one newer manifest. The gate preserves the last accepted
// release on verification/apply failure and reports only its fixed safe state.
func (s *PrivacyMaskReleaseSynchronizer) Sync(ctx context.Context) error {
	if s == nil || s.client == nil || s.gate == nil {
		return ErrInvalidPrivacyMaskTransport
	}
	after := int64(0)
	if accepted := s.gate.LastAccepted(); accepted != nil {
		after = accepted.Version
	}
	manifest, err := s.client.Pull(ctx, after)
	if err != nil {
		return err
	}
	if manifest == nil {
		return nil
	}
	if err := s.gate.Accept(ctx, *manifest); err != nil {
		return err
	}
	return nil
}
