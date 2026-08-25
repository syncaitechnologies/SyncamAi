package agent

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidPrivacyMaskReleaseWorker = errors.New("privacy mask release worker is invalid")

// PrivacyMaskReleaseSyncer is deliberately limited to the dedicated
// privacy-release channel. Implementations must not use generic configuration
// delivery or carry media, frames, stream credentials, or encoder handles.
type PrivacyMaskReleaseSyncer interface {
	Sync(context.Context) error
}

// PrivacyMaskReleaseWorker serializes periodic reconciliation of the dedicated
// privacy-release channel. It runs one initial sync, then waits the configured
// interval between completed syncs; overlapping pulls or hardware activations
// are therefore impossible through this worker.
type PrivacyMaskReleaseWorker struct {
	syncer   PrivacyMaskReleaseSyncer
	interval time.Duration
}

func NewPrivacyMaskReleaseWorker(syncer PrivacyMaskReleaseSyncer, interval time.Duration) (*PrivacyMaskReleaseWorker, error) {
	if syncer == nil || interval <= 0 {
		return nil, ErrInvalidPrivacyMaskReleaseWorker
	}
	return &PrivacyMaskReleaseWorker{syncer: syncer, interval: interval}, nil
}

// Run reconciles only through the supplied dedicated synchronizer until the
// context is cancelled. A synchronization failure is returned immediately so
// the caller can apply its supervised retry policy; the worker does not fall
// back to any broader configuration path.
func (w *PrivacyMaskReleaseWorker) Run(ctx context.Context) error {
	if w == nil || w.syncer == nil || w.interval <= 0 {
		return ErrInvalidPrivacyMaskReleaseWorker
	}
	if ctx == nil {
		return ErrInvalidPrivacyMaskReleaseWorker
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if err := w.syncer.Sync(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

