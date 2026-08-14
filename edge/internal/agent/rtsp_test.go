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
