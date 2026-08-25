package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedPrivacyMaskReleaseWorker struct {
	results []error
	calls   int
}

func (w *scriptedPrivacyMaskReleaseWorker) Run(context.Context) error {
	if w.calls >= len(w.results) {
		return nil
	}
	result := w.results[w.calls]
	w.calls++
	return result
}

func TestPrivacyMaskReleaseSupervisorRejectsInvalidConfiguration(t *testing.T) {
	worker := &scriptedPrivacyMaskReleaseWorker{}
	for _, test := range []struct {
		name   string
		worker PrivacyMaskReleaseWorkerRunner
		policy PrivacyMaskReleaseRetryPolicy
	}{
		{name: "missing worker", policy: PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Second, MaxDelay: time.Second}},
		{name: "zero initial delay", worker: worker, policy: PrivacyMaskReleaseRetryPolicy{MaxDelay: time.Second}},
		{name: "maximum below initial delay", worker: worker, policy: PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Second, MaxDelay: time.Millisecond}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPrivacyMaskReleaseSupervisor(test.worker, test.policy); !errors.Is(err, ErrInvalidPrivacyMaskReleaseSupervisor) {
				t.Fatalf("invalid supervisor configuration must fail closed: %v", err)
			}
		})
	}
	supervisor, err := NewPrivacyMaskReleaseSupervisor(worker, PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Second, MaxDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Run(nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseSupervisor) {
		t.Fatalf("nil context must fail closed: %v", err)
	}
}

func TestPrivacyMaskReleaseSupervisorRetriesWithBoundedBackoff(t *testing.T) {
	failure := errors.New("dedicated transport unavailable")
	worker := &scriptedPrivacyMaskReleaseWorker{results: []error{failure, failure, nil}}
	supervisor, err := NewPrivacyMaskReleaseSupervisor(worker, PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Second, MaxDelay: 1500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	supervisor.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	if err := supervisor.Run(context.Background()); err != nil {
		t.Fatalf("supervisor must retry until the worker succeeds: %v", err)
	}
	if worker.calls != 3 {
		t.Fatalf("expected three dedicated worker runs, got %d", worker.calls)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 1500*time.Millisecond {
		t.Fatalf("unexpected bounded retry delays: %#v", waits)
	}
}

func TestPrivacyMaskReleaseSupervisorStopsWhenCancelled(t *testing.T) {
	worker := &scriptedPrivacyMaskReleaseWorker{results: []error{errors.New("dedicated transport unavailable")}}
	supervisor, err := NewPrivacyMaskReleaseSupervisor(worker, PrivacyMaskReleaseRetryPolicy{InitialDelay: time.Second, MaxDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := supervisor.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must stop the supervisor: %v", err)
	}
	if worker.calls != 1 {
		t.Fatalf("expected one final worker result before cancellation handling, got %d", worker.calls)
	}
}

func TestNextPrivacyMaskReleaseRetryDelayNeverExceedsMaximum(t *testing.T) {
	for _, test := range []struct {
		delay, maximum, want time.Duration
	}{
		{time.Second, 5 * time.Second, 2 * time.Second},
		{3 * time.Second, 5 * time.Second, 5 * time.Second},
		{5 * time.Second, 5 * time.Second, 5 * time.Second},
	} {
		if got := nextPrivacyMaskReleaseRetryDelay(test.delay, test.maximum); got != test.want {
			t.Fatalf("next delay(%s, %s) = %s, want %s", test.delay, test.maximum, got, test.want)
		}
	}
}

