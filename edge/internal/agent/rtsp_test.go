package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordedRTSPRun struct {
	binary string
	args   []string
}

type fakeRTSPRunner struct {
	runs   []recordedRTSPRun
	errors []error
}

type fakeRTSPFrameRunner struct {
	fakeRTSPRunner
	frames     [][]byte
	frameBytes []int
}

func (r *fakeRTSPFrameRunner) RunFrames(_ context.Context, binary string, args []string, frameBytes int, started func(), consume func([]byte) error) error {
	r.runs = append(r.runs, recordedRTSPRun{binary: binary, args: append([]string(nil), args...)})
	r.frameBytes = append(r.frameBytes, frameBytes)
	started()
	for _, pixels := range r.frames {
		if err := consume(append([]byte(nil), pixels...)); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeRTSPRunner) Run(_ context.Context, binary string, args []string, started func()) error {
	r.runs = append(r.runs, recordedRTSPRun{binary: binary, args: append([]string(nil), args...)})
	started()
	if len(r.errors) == 0 {
		return nil
	}
	err := r.errors[0]
	r.errors = r.errors[1:]
	return err
}

func TestNewRTSPIngestRejectsInvalidSourcesAndConfig(t *testing.T) {
	for _, source := range []RTSPSource{
		{},
		{ID: "camera-1", URL: "http://camera/live"},
		{ID: "camera-1", URL: "rtsp:///live"},
		{ID: "camera-1\nforged", URL: "rtsp://camera/live"},
		{ID: "camera-1", URL: "rtsp://camera/live#secret"},
		{ID: "camera-1", URL: "rtsp://camera/live", Transport: "http"},
	} {
		if _, err := NewRTSPIngest(source, RTSPIngestConfig{}, nil); !errors.Is(err, ErrInvalidRTSPSource) {
			t.Fatalf("source %+v: expected validation error, got %v", source, err)
		}
	}
	if _, err := NewRTSPIngest(RTSPSource{ID: "camera-1", URL: "rtsp://camera/live"}, RTSPIngestConfig{RetryMinimum: time.Second, RetryMaximum: time.Millisecond}, nil); !errors.Is(err, ErrInvalidRTSPConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
}

func TestRTSPIngestBuildsBoundedFFmpegCommand(t *testing.T) {
	runner := &fakeRTSPRunner{}
	ingest, err := NewRTSPIngest(
		RTSPSource{ID: "camera-1", URL: "rtsps://operator:private@camera.local/live?profile=main", Transport: "TCP"},
		RTSPIngestConfig{Binary: "ffmpeg-safe", ConnectTimeout: 7 * time.Second},
		runner,
	)
	if err != nil {
		t.Fatalf("new ingest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ingest.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	var statuses []RTSPStatus
	if err := ingest.Run(ctx, func(status RTSPStatus) { statuses = append(statuses, status) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if len(runner.runs) != 1 || runner.runs[0].binary != "ffmpeg-safe" {
		t.Fatalf("unexpected runs: %+v", runner.runs)
	}
	wantArgs := []string{"-nostdin", "-hide_banner", "-loglevel", "warning", "-rtsp_transport", "tcp", "-rw_timeout", "7000000", "-i", "rtsps://operator:private@camera.local/live?profile=main", "-map", "0:v:0", "-an", "-f", "null", "-"}
	if !reflect.DeepEqual(runner.runs[0].args, wantArgs) {
		t.Fatalf("args mismatch\n got: %q\nwant: %q", runner.runs[0].args, wantArgs)
	}
	for _, status := range statuses {
		if strings.Contains(status.SourceID+errorText(status.Err), "private") {
			t.Fatalf("credential leaked through status: %+v", status)
		}
	}
	if got := ingest.Status(); got.State != RTSPStopped || got.SourceID != "camera-1" {
		t.Fatalf("unexpected final status: %+v", got)
	}
}

func TestRTSPIngestRetriesWithBoundedBackoff(t *testing.T) {
	runner := &fakeRTSPRunner{errors: []error{errors.New("rtsp://user:secret@camera/live failed"), nil, nil}}
	ingest, err := NewRTSPIngest(
		RTSPSource{ID: "camera-2", URL: "rtsp://user:secret@camera/live"},
		RTSPIngestConfig{RetryMinimum: time.Second, RetryMaximum: 2 * time.Second},
		runner,
	)
	if err != nil {
		t.Fatalf("new ingest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var delays []time.Duration
	ingest.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if len(delays) == 3 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	var statuses []RTSPStatus
	if err := ingest.Run(ctx, func(status RTSPStatus) { statuses = append(statuses, status) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if !reflect.DeepEqual(delays, []time.Duration{time.Second, 2 * time.Second, 2 * time.Second}) {
		t.Fatalf("unexpected retry delays: %v", delays)
	}
	for _, status := range statuses {
		if strings.Contains(errorText(status.Err), "secret") {
			t.Fatalf("runner error leaked credentials: %+v", status)
		}
	}
}

func TestRTSPIngestAppliesCodecDecoderAndReportsSafeCapability(t *testing.T) {
	runner := &fakeRTSPRunner{}
	ingest, err := NewRTSPIngest(
		RTSPSource{ID: "camera-h265", URL: "rtsp://operator:private@camera.local/live"},
		RTSPIngestConfig{Decode: &DecodeProfile{Codec: CodecH265, Preference: DecodeAuto, AvailableDecoders: []string{"hevc_nvv4l2dec"}}},
		runner,
	)
	if err != nil {
		t.Fatalf("new ingest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ingest.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	var statuses []RTSPStatus
	if err := ingest.Run(ctx, func(status RTSPStatus) { statuses = append(statuses, status) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if len(runner.runs) != 1 || !containsArgumentPair(runner.runs[0].args, "-c:v", "hevc_nvv4l2dec") {
		t.Fatalf("hardware decoder missing from args: %q", runner.runs)
	}
	for _, status := range statuses {
		if status.Codec != CodecH265 || status.Decoder != "hevc_nvv4l2dec" || !status.HardwareAccelerated {
			t.Fatalf("decoder capability missing from status: %+v", status)
		}
		if strings.Contains(status.SourceID+errorText(status.Err), "private") {
			t.Fatalf("credential leaked through status: %+v", status)
		}
	}
}

func TestRTSPIngestDeliversOnlyMaskedSampledRGB24Frames(t *testing.T) {
	var received []RGB24Frame
	sampler, err := NewAnalyticsFrameSampler(maskFrameCameraID, 10, AnalyticsFrameConsumerFunc(func(_ context.Context, frame RGB24Frame) error {
		received = append(received, frame)
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
	runner := &fakeRTSPFrameRunner{frames: [][]byte{{11, 22, 33}, {44, 55, 66}}}
	ingest, err := NewRTSPIngest(
		RTSPSource{ID: maskFrameCameraID, URL: "rtsp://operator:private@camera.local/live"},
		RTSPIngestConfig{FrameWidth: 1, FrameHeight: 1, FramePipeline: mask},
		runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	ingest.now = func() time.Time { return start }
	ctx, cancel := context.WithCancel(context.Background())
	retries := 0
	ingest.sleep = func(context.Context, time.Duration) error {
		retries++
		if retries == 2 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if err := ingest.Run(ctx, func(RTSPStatus) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if len(received) != 1 || !reflect.DeepEqual(received[0].Pixels, []byte{0, 0, 0}) {
		t.Fatalf("analytics must receive only a masked sampled frame: %+v", received)
	}
	if received[0].Sequence != 1 || !received[0].ObservedAt.Equal(start) {
		t.Fatalf("unexpected frame metadata: %+v", received[0])
	}
	if !reflect.DeepEqual(runner.frameBytes, []int{3, 3}) {
		t.Fatalf("unexpected frame bounds: %v", runner.frameBytes)
	}
	wantSuffix := []string{"-s:v", "1x1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"}
	if args := runner.runs[0].args; len(args) < len(wantSuffix) || !reflect.DeepEqual(args[len(args)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("rawvideo output is not bounded: %q", args)
	}
	if metrics := sampler.Metrics(); metrics.AcceptedFrames != 4 || metrics.ForwardedFrames != 1 || metrics.RateSuppressedFrames != 3 {
		t.Fatalf("unexpected sampler metrics: %+v", metrics)
	}
	if ingest.sequence != 4 || !ingest.lastFrameObserved.Equal(start.Add(3*time.Nanosecond)) {
		t.Fatalf("frame ordering did not survive restart: sequence=%d observed=%s", ingest.sequence, ingest.lastFrameObserved)
	}
}

func TestRTSPIngestRejectsIncompleteOrBypassFrameConfiguration(t *testing.T) {
	validSource := RTSPSource{ID: maskFrameCameraID, URL: "rtsp://camera.local/live"}
	sampler, err := NewAnalyticsFrameSampler(maskFrameCameraID, 5, AnalyticsFrameConsumerFunc(func(context.Context, RGB24Frame) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	candidate := maskFrameCandidate(t, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`)
	pipeline, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), sampler)
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []RTSPIngestConfig{
		{FrameWidth: 1, FrameHeight: 1},
		{FrameWidth: 1, FramePipeline: pipeline},
		{FrameHeight: 1, FramePipeline: pipeline},
		{FrameWidth: maxMaskFrameDimension, FrameHeight: maxMaskFrameDimension, FramePipeline: pipeline},
	} {
		if _, err := NewRTSPIngest(validSource, config, &fakeRTSPFrameRunner{}); !errors.Is(err, ErrInvalidRTSPConfig) {
			t.Fatalf("unsafe frame config must fail: %+v err=%v", config, err)
		}
	}
	if _, err := NewRTSPIngest(RTSPSource{ID: "camera-not-uuid", URL: "rtsp://camera.local/live"}, RTSPIngestConfig{FrameWidth: 1, FrameHeight: 1, FramePipeline: pipeline}, &fakeRTSPFrameRunner{}); !errors.Is(err, ErrInvalidRTSPConfig) {
		t.Fatalf("non-UUID framed source must fail: %v", err)
	}
	if _, err := NewRTSPIngest(validSource, RTSPIngestConfig{FrameWidth: 1, FrameHeight: 1, FramePipeline: pipeline}, &fakeRTSPRunner{}); !errors.Is(err, ErrInvalidRTSPConfig) {
		t.Fatalf("runner without framed output must fail: %v", err)
	}
	bypass, err := NewPreAnalyticsPrivacyMask(candidate, maskFrameAdapter(t, candidate), MaskedFrameConsumerFunc(func(context.Context, RGB24Frame) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRTSPIngest(validSource, RTSPIngestConfig{FrameWidth: 1, FrameHeight: 1, FramePipeline: bypass}, &fakeRTSPFrameRunner{}); !errors.Is(err, ErrInvalidRTSPConfig) {
		t.Fatalf("mask without the bounded sampler must fail: %v", err)
	}
}

func TestConsumeFixedRGB24FramesRejectsPartialAndConsumerFailure(t *testing.T) {
	var frames [][]byte
	if err := consumeFixedRGB24Frames(strings.NewReader("abcdef"), 3, func(pixels []byte) error {
		frames = append(frames, pixels)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(frames, [][]byte{{'a', 'b', 'c'}, {'d', 'e', 'f'}}) {
		t.Fatalf("unexpected fixed frames: %v", frames)
	}
	if err := consumeFixedRGB24Frames(strings.NewReader("abcd"), 3, func([]byte) error { return nil }); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial frame must fail safely: %v", err)
	}
	if err := consumeFixedRGB24Frames(strings.NewReader("abc"), 3, func([]byte) error { return errors.New("private downstream details") }); err == nil || err.Error() != "decoded frame consumer failed" {
		t.Fatalf("consumer details must be sanitized: %v", err)
	}
}

func TestRetryDelay(t *testing.T) {
	for attempt, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
		if got := retryDelay(time.Second, 5*time.Second, attempt+1); got != want {
			t.Fatalf("attempt %d: got %s want %s", attempt+1, got, want)
		}
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func containsArgumentPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}
