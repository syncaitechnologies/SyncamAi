package agent

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidPrivacyMaskReleaseSupervisor = errors.New("privacy mask release supervisor is invalid")

// PrivacyMaskReleaseWorkerRunner is intentionally limited to the dedicated
// privacy-release worker. It must not substitute generic configuration
// delivery or carry frames, stream credentials, encoder handles, or media.
type PrivacyMaskReleaseWorkerRunner interface {
	Run(context.Context) error
}

// PrivacyMaskReleaseRetryPolicy bounds the wait between failed worker runs.
// It starts at InitialDelay and doubles up to, but never beyond, MaxDelay.
type PrivacyMaskReleaseRetryPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// PrivacyMaskReleaseSupervisor restarts only a dedicated privacy-release
// worker after a failure. It does not interpret release data or change the
// worker's controlled privacy-release gates.
type PrivacyMaskReleaseSupervisor struct {
	worker PrivacyMaskReleaseWorkerRunner
	policy PrivacyMaskReleaseRetryPolicy
	wait   func(context.Context, time.Duration) error
}

func NewPrivacyMaskReleaseSupervisor(worker PrivacyMaskReleaseWorkerRunner, policy PrivacyMaskReleaseRetryPolicy) (*PrivacyMaskReleaseSupervisor, error) {
	if worker == nil || policy.InitialDelay <= 0 || policy.MaxDelay < policy.InitialDelay {
		return nil, ErrInvalidPrivacyMaskReleaseSupervisor
	}
	return &PrivacyMaskReleaseSupervisor{worker: worker, policy: policy, wait: waitForPrivacyMaskReleaseRetry}, nil
}

// Run restarts the worker after failures using the bounded retry policy. A
// caller cancellation takes precedence over the last worker failure so the
// supervisor always stops promptly and predictably.
func (s *PrivacyMaskReleaseSupervisor) Run(ctx context.Context) error {
	if s == nil || s.worker == nil || s.policy.InitialDelay <= 0 || s.policy.MaxDelay < s.policy.InitialDelay || s.wait == nil || ctx == nil {
		return ErrInvalidPrivacyMaskReleaseSupervisor
	}
	delay := s.policy.InitialDelay
	for {
		if err := s.worker.Run(ctx); err == nil {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.wait(ctx, delay); err != nil {
			return err
		}
		delay = nextPrivacyMaskReleaseRetryDelay(delay, s.policy.MaxDelay)
	}
}

func waitForPrivacyMaskReleaseRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextPrivacyMaskReleaseRetryDelay(delay, maximum time.Duration) time.Duration {
	if delay >= maximum || delay > maximum/2 {
		return maximum
	}
	return delay * 2
}

