package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

const samplerCameraID = "77777777-7777-4777-8777-777777777777"

func TestAnalyticsFrameSamplerEnforcesRateAndDuplicateSuppression(t *testing.T) {
	var forwarded []RGB24Frame
	sampler, err := NewAnalyticsFrameSampler(samplerCameraID, 10, AnalyticsFrameConsumerFunc(func(_ context.Context, frame RGB24Frame) error {
		forwarded = append(forwarded, frame)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	frames := []RGB24Frame{
		samplerFrame(1, start, 1),
		samplerFrame(2, start.Add(50*time.Millisecond), 2),
		samplerFrame(3, start.Add(100*time.Millisecond), 1),
		samplerFrame(4, start.Add(200*time.Millisecond), 3),
	}
	for _, frame := range frames {
		if err := sampler.ConsumeMaskedFrame(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
	}
	metrics := sampler.Metrics()
	if len(forwarded) != 2 || forwarded[0].Sequence != 1 || forwarded[1].Sequence != 4 {
		t.Fatalf("unexpected forwarded frames: %+v", forwarded)
	}
	if metrics.AcceptedFrames != 4 || metrics.ForwardedFrames != 2 || metrics.RateSuppressedFrames != 1 || metrics.DuplicateFrames != 1 || metrics.DownstreamFailures != 0 {
		t.Fatalf("unexpected sampling metrics: %+v", metrics)
	}
}

func TestAnalyticsFrameSamplerRejectsInvalidOrOutOfOrderFrames(t *testing.T) {
	called := false
	sampler, err := NewAnalyticsFrameSampler(samplerCameraID, 5, AnalyticsFrameConsumerFunc(func(context.Context, RGB24Frame) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if err := sampler.ConsumeMaskedFrame(context.Background(), samplerFrame(2, start, 1)); err != nil {
		t.Fatal(err)
	}
	called = false
	for _, frame := range []RGB24Frame{
		samplerFrame(2, start.Add(time.Second), 2),
		samplerFrame(3, start.Add(-time.Second), 2),
		{CameraID: samplerCameraID, Sequence: 4, ObservedAt: start, Width: 1, Height: 1, Pixels: []byte{1}},
		{CameraID: "88888888-8888-4888-8888-888888888888", Sequence: 4, ObservedAt: start, Width: 1, Height: 1, Pixels: []byte{1, 2, 3}},
	} {
		if err := sampler.ConsumeMaskedFrame(context.Background(), frame); !errors.Is(err, ErrInvalidAnalyticsFrame) {
			t.Fatalf("invalid frame must fail: %+v err=%v", frame, err)
		}
	}
	if called {
		t.Fatal("invalid frame reached analytics consumer")
	}
}

func TestAnalyticsFrameSamplerReportsDownstreamBackpressure(t *testing.T) {
	downstream := errors.New("inference queue full")
	sampler, err := NewAnalyticsFrameSampler(samplerCameraID, 5, AnalyticsFrameConsumerFunc(func(context.Context, RGB24Frame) error { return downstream }))
	if err != nil {
		t.Fatal(err)
	}
	if err := sampler.ConsumeMaskedFrame(context.Background(), samplerFrame(1, time.Now().UTC(), 1)); !errors.Is(err, downstream) {
		t.Fatalf("downstream failure must reach caller: %v", err)
	}
	if metrics := sampler.Metrics(); metrics.DownstreamFailures != 1 || metrics.ForwardedFrames != 0 {
		t.Fatalf("unexpected downstream metrics: %+v", metrics)
	}
}

func TestNewAnalyticsFrameSamplerRejectsUnsafeConfiguration(t *testing.T) {
	consumer := AnalyticsFrameConsumerFunc(func(context.Context, RGB24Frame) error { return nil })
	for _, test := range []struct {
		cameraID string
		fps      int
		consumer AnalyticsFrameConsumer
	}{
		{cameraID: "bad", fps: 5, consumer: consumer},
		{cameraID: samplerCameraID, fps: 4, consumer: consumer},
		{cameraID: samplerCameraID, fps: 11, consumer: consumer},
		{cameraID: samplerCameraID, fps: 5, consumer: nil},
	} {
		if _, err := NewAnalyticsFrameSampler(test.cameraID, test.fps, test.consumer); !errors.Is(err, ErrInvalidAnalyticsFrame) {
			t.Fatalf("unsafe sampler config must fail: %+v err=%v", test, err)
		}
	}
}

func TestApprovedMaskRunsBeforeAnalyticsSampler(t *testing.T) {
	var received RGB24Frame
	sampler, err := NewAnalyticsFrameSampler(maskFrameCameraID, 5, AnalyticsFrameConsumerFunc(func(_ context.Context, frame RGB24Frame) error {
		received = frame
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	mask, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), sampler)
	if err != nil {
		t.Fatal(err)
	}
	pixels := []byte{11, 22, 33}
	if err := mask.Forward(context.Background(), RGB24Frame{CameraID: maskFrameCameraID, Sequence: 1, ObservedAt: time.Now().UTC(), Width: 1, Height: 1, Pixels: pixels}); err != nil {
		t.Fatal(err)
	}
	if received.Pixels[0] != 0 || received.Pixels[1] != 0 || received.Pixels[2] != 0 {
		t.Fatalf("analytics received unmasked pixels: %v", received.Pixels)
	}
}

func samplerFrame(sequence uint64, observedAt time.Time, value byte) RGB24Frame {
	return RGB24Frame{CameraID: samplerCameraID, Sequence: sequence, ObservedAt: observedAt, Width: 1, Height: 1, Pixels: []byte{value, value, value}}
}
