package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type runtimeHeartbeatFake struct {
	desired  int64
	snapshot Telemetry
}

func (f *runtimeHeartbeatFake) Run(ctx context.Context, _ time.Duration, snapshot func() Telemetry, report func(HeartbeatResult, error)) error {
	f.snapshot = snapshot()
	report(HeartbeatResult{Device: DeviceStatus{DesiredConfigRevision: f.desired}}, nil)
	<-ctx.Done()
	return ctx.Err()
}

type runtimeConfigurationFake struct {
	mu      sync.Mutex
	applied int64
	desired int64
	started chan struct{}
}

func (f *runtimeConfigurationFake) AppliedRevision() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applied
}

func (f *runtimeConfigurationFake) Run(ctx context.Context, _ time.Duration) error {
	close(f.started)
	<-ctx.Done()
	return ctx.Err()
}

func (f *runtimeConfigurationFake) SyncIfDesired(_ context.Context, desired int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.desired = desired
	f.applied = desired
	return nil
}

type runtimeIngestFake struct{ started chan struct{} }

func (f *runtimeIngestFake) Run(ctx context.Context, report func(RTSPStatus)) error {
	close(f.started)
	report(RTSPStatus{SourceID: "camera-1", State: RTSPStreaming, Attempt: 1})
	<-ctx.Done()
	return ctx.Err()
}

type runtimeSpoolFake struct{ depth int64 }

func (f runtimeSpoolFake) Metrics() SpoolMetrics { return SpoolMetrics{Depth: f.depth} }

func TestEdgeRuntimeComposesHeartbeatConfigurationSpoolAndRTSP(t *testing.T) {
	heartbeat := &runtimeHeartbeatFake{desired: 7}
	configuration := &runtimeConfigurationFake{applied: 3, started: make(chan struct{})}
	ingest := &runtimeIngestFake{started: make(chan struct{})}
	runtime, err := NewEdgeRuntime(EdgeRuntimeConfig{FirmwareVersion: "1.0.0", HeartbeatInterval: time.Millisecond, ConfigurationInterval: time.Millisecond}, heartbeat, configuration, runtimeSpoolFake{depth: 4}, ingest)
	if err != nil {
		t.Fatal(err)
	}
	runtime.now = func() time.Time { return time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC) }
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan RuntimeEvent, 10)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, func(event RuntimeEvent) { events <- event }) }()
	<-configuration.started
	<-ingest.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation: %v", err)
	}
	if heartbeat.snapshot.FirmwareVersion != "1.0.0" || heartbeat.snapshot.StoreForwardDepth != 4 || heartbeat.snapshot.UptimeSeconds != 0 {
		t.Fatalf("unexpected heartbeat snapshot: %+v", heartbeat.snapshot)
	}
	if configuration.desired != 7 || configuration.AppliedRevision() != 7 {
		t.Fatalf("desired configuration was not synchronized: desired=%d applied=%d", configuration.desired, configuration.AppliedRevision())
	}

	states := map[string]bool{}
	close(events)
	for event := range events {
		states[event.Component+":"+event.State] = true
		if event.Component == "rtsp" && event.SourceID != "camera-1" {
			t.Fatalf("runtime event leaked or changed source identity: %+v", event)
		}
	}
	for _, expected := range []string{"runtime:started", "heartbeat:online", "configuration:synchronized", "rtsp:streaming", "runtime:stopped"} {
		if !states[expected] {
			t.Fatalf("missing %s in events: %v", expected, states)
		}
	}
}

func TestEdgeRuntimeRejectsIncompleteComposition(t *testing.T) {
	configuration := &runtimeConfigurationFake{started: make(chan struct{})}
	valid := EdgeRuntimeConfig{FirmwareVersion: "1", HeartbeatInterval: time.Second, ConfigurationInterval: time.Second}
	if _, err := NewEdgeRuntime(valid, nil, configuration, runtimeSpoolFake{}); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("missing heartbeat must fail: %v", err)
	}
	valid.FirmwareVersion = ""
	if _, err := NewEdgeRuntime(valid, &runtimeHeartbeatFake{}, configuration, runtimeSpoolFake{}); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("missing firmware must fail: %v", err)
	}
}
