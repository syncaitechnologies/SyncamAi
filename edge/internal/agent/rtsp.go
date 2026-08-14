package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	DefaultRTSPConnectTimeout = 10 * time.Second
	DefaultRTSPRetryMinimum   = time.Second
	DefaultRTSPRetryMaximum   = 30 * time.Second
	maxRTSPSourceIDLength     = 128
)

var (
	ErrInvalidRTSPSource = errors.New("rtsp source is invalid")
	ErrInvalidRTSPConfig = errors.New("rtsp ingest configuration is invalid")
)

// RTSPSource describes a camera stream. URL may contain camera credentials,
// but callers must obtain them from the runtime secret boundary and must not
// log the value. The supervisor reports only ID and lifecycle state.
type RTSPSource struct {
	ID        string
	URL       string
	Transport string
}

// RTSPIngestConfig controls one supervised FFmpeg process. Binary defaults to
// ffmpeg. Retry delays are bounded so a failed camera cannot create a tight
// restart loop or remain abandoned indefinitely.
type RTSPIngestConfig struct {
	Binary         string
	ConnectTimeout time.Duration
	RetryMinimum   time.Duration
	RetryMaximum   time.Duration
	Decode         *DecodeProfile
}

type RTSPState string

const (
	RTSPConnecting RTSPState = "connecting"
	RTSPStreaming  RTSPState = "streaming"
	RTSPRetrying   RTSPState = "retrying"
	RTSPStopped    RTSPState = "stopped"
)

type RTSPStatus struct {
	SourceID            string
	State               RTSPState
	Attempt             int
	Codec               VideoCodec
	Decoder             string
	HardwareAccelerated bool
	Err                 error
}

// CommandRunner is the process boundary used by RTSPIngest. Implementations
// must not include command arguments in returned errors because they can hold
// camera credentials.
type CommandRunner interface {
	Run(context.Context, string, []string, func()) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, binary string, args []string, started func()) error {
	command := exec.CommandContext(ctx, binary, args...)
	if err := command.Start(); err != nil {
		return errors.New("ffmpeg process failed to start")
	}
	started()
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("ffmpeg process exited unsuccessfully")
	}
	return nil
}

// RTSPIngest supervises a single camera pull. It intentionally emits decoded
// frames to FFmpeg's null muxer in this slice; later decode and ring-buffer
// tasks replace that sink without changing lifecycle and retry semantics.
type RTSPIngest struct {
	source  RTSPSource
	config  RTSPIngestConfig
	runner  CommandRunner
	sleep   func(context.Context, time.Duration) error
	decoder DecoderSelection

	mu     sync.RWMutex
	status RTSPStatus
}

func NewRTSPIngest(source RTSPSource, config RTSPIngestConfig, runner CommandRunner) (*RTSPIngest, error) {
	source.ID = strings.TrimSpace(source.ID)
	if source.ID == "" || len(source.ID) > maxRTSPSourceIDLength || strings.ContainsAny(source.ID, "\r\n\t") {
		return nil, ErrInvalidRTSPSource
	}
	parsed, err := url.Parse(strings.TrimSpace(source.URL))
	if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" || parsed.Fragment != "" {
		return nil, ErrInvalidRTSPSource
	}
	source.URL = parsed.String()
	source.Transport = strings.ToLower(strings.TrimSpace(source.Transport))
	if source.Transport == "" {
		source.Transport = "tcp"
	}
	if source.Transport != "tcp" && source.Transport != "udp" {
		return nil, ErrInvalidRTSPSource
	}
	if config.Binary == "" {
		config.Binary = "ffmpeg"
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = DefaultRTSPConnectTimeout
	}
	if config.RetryMinimum == 0 {
		config.RetryMinimum = DefaultRTSPRetryMinimum
	}
	if config.RetryMaximum == 0 {
		config.RetryMaximum = DefaultRTSPRetryMaximum
	}
	if config.ConnectTimeout <= 0 || config.RetryMinimum <= 0 || config.RetryMaximum < config.RetryMinimum || strings.TrimSpace(config.Binary) == "" {
		return nil, ErrInvalidRTSPConfig
	}
	if runner == nil {
		runner = execCommandRunner{}
	}
	var decoder DecoderSelection
	if config.Decode != nil {
		decoder, err = SelectDecoder(*config.Decode)
		if err != nil {
			return nil, err
		}
	}
	return &RTSPIngest{
		source:  source,
		config:  config,
		runner:  runner,
		sleep:   sleepContext,
		decoder: decoder,
		status:  RTSPStatus{SourceID: source.ID, State: RTSPStopped, Codec: decoder.Codec, Decoder: decoder.Name, HardwareAccelerated: decoder.HardwareAccelerated},
	}, nil
}

func (i *RTSPIngest) Status() RTSPStatus {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.status
}

// Run pulls the source until cancellation. Every unexpected exit is retried
// with bounded exponential backoff. Status callbacks never receive the URL or
// raw subprocess errors, preventing credential disclosure through logs.
func (i *RTSPIngest) Run(ctx context.Context, report func(RTSPStatus)) error {
	if i == nil || i.runner == nil || report == nil {
		return ErrInvalidRTSPConfig
	}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			i.publish(report, i.newStatus(RTSPStopped, attempt, nil))
			return err
		}
		attempt++
		i.publish(report, i.newStatus(RTSPConnecting, attempt, nil))
		err := i.runner.Run(ctx, i.config.Binary, i.arguments(), func() {
			i.publish(report, i.newStatus(RTSPStreaming, attempt, nil))
		})
		if ctx.Err() != nil {
			i.publish(report, i.newStatus(RTSPStopped, attempt, nil))
			return ctx.Err()
		}
		safeErr := errors.New("rtsp ingest process stopped")
		if err == nil {
			safeErr = errors.New("rtsp ingest process ended")
		}
		i.publish(report, i.newStatus(RTSPRetrying, attempt, safeErr))
		if err := i.sleep(ctx, retryDelay(i.config.RetryMinimum, i.config.RetryMaximum, attempt)); err != nil {
			i.publish(report, i.newStatus(RTSPStopped, attempt, nil))
			return err
		}
	}
}

func (i *RTSPIngest) arguments() []string {
	timeoutMicros := i.config.ConnectTimeout.Microseconds()
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtsp_transport", i.source.Transport,
		"-rw_timeout", fmt.Sprintf("%d", timeoutMicros),
	}
	if i.decoder.Name != "" {
		args = append(args, "-c:v", i.decoder.Name)
	}
	return append(args, "-i", i.source.URL, "-map", "0:v:0", "-an", "-f", "null", "-")
}

func (i *RTSPIngest) newStatus(state RTSPState, attempt int, err error) RTSPStatus {
	return RTSPStatus{
		SourceID:            i.source.ID,
		State:               state,
		Attempt:             attempt,
		Codec:               i.decoder.Codec,
		Decoder:             i.decoder.Name,
		HardwareAccelerated: i.decoder.HardwareAccelerated,
		Err:                 err,
	}
}

func (i *RTSPIngest) publish(report func(RTSPStatus), status RTSPStatus) {
	i.mu.Lock()
	i.status = status
	i.mu.Unlock()
	report(status)
}

func retryDelay(minimum, maximum time.Duration, attempt int) time.Duration {
	delay := minimum
	for n := 1; n < attempt && delay < maximum; n++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
