package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingPrivacyMaskReleaseSyncer struct {
	calls chan struct{}
	err   error
}

func (s *recordingPrivacyMaskReleaseSyncer) Sync(context.Context) error {
	if s.calls != nil {
		s.calls <- struct{}{}
	}
	return s.err
}

func TestPrivacyMaskReleaseWorkerRejectsInvalidConfiguration(t *testing.T) {
	syncer := &recordingPrivacyMaskReleaseSyncer{}
	for _, test := range []struct {
		name     string
		syncer   PrivacyMaskReleaseSyncer
		interval time.Duration
	}{
		{name: "missing synchronizer", interval: time.Second},
		{name: "zero interval", syncer: syncer},
		{name: "negative interval", syncer: syncer, interval: -time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPrivacyMaskReleaseWorker(test.syncer, test.interval); !errors.Is(err, ErrInvalidPrivacyMaskReleaseWorker) {
				t.Fatalf("invalid worker configuration must fail closed: %v", err)
			}
		})
	}
	worker, err := NewPrivacyMaskReleaseWorker(syncer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(nil); !errors.Is(err, ErrInvalidPrivacyMaskReleaseWorker) {
		t.Fatalf("nil context must fail closed: %v", err)
	}
}

func TestPrivacyMaskReleaseWorkerReturnsSynchronizationFailure(t *testing.T) {
	want := errors.New("dedicated privacy transport unavailable")
	syncer := &recordingPrivacyMaskReleaseSyncer{calls: make(chan struct{}, 1), err: want}
	worker, err := NewPrivacyMaskReleaseWorker(syncer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("sync failure must reach the supervised caller: %v", err)
	}
	if len(syncer.calls) != 1 {
		t.Fatalf("expected one initial dedicated sync, got %d", len(syncer.calls))
	}
}

func TestPrivacyMaskReleaseWorkerRunsInitialSyncAndStopsOnCancellation(t *testing.T) {
	syncer := &recordingPrivacyMaskReleaseSyncer{calls: make(chan struct{}, 1)}
	worker, err := NewPrivacyMaskReleaseWorker(syncer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-syncer.calls:
	case <-time.After(time.Second):
		t.Fatal("worker did not run its initial dedicated sync")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("worker must stop on cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestPrivacyMaskReleaseWorkerRepeatsOnlyAfterItsConfiguredInterval(t *testing.T) {
	syncer := &recordingPrivacyMaskReleaseSyncer{calls: make(chan struct{}, 2)}
	worker, err := NewPrivacyMaskReleaseWorker(syncer, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	for calls := 0; calls < 2; calls++ {
		select {
		case <-syncer.calls:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("worker did not perform its next serialized dedicated sync")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("worker must stop on cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

