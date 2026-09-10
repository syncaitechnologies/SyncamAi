package agent

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	MinAnalyticsFrameFPS = 5
	MaxAnalyticsFrameFPS = 10
)

var ErrInvalidAnalyticsFrame = errors.New("analytics frame is invalid")

type AnalyticsFrameConsumer interface {
	ConsumeAnalyticsFrame(context.Context, RGB24Frame) error
}

type AnalyticsFrameConsumerFunc func(context.Context, RGB24Frame) error

func (f AnalyticsFrameConsumerFunc) ConsumeAnalyticsFrame(ctx context.Context, frame RGB24Frame) error {
	return f(ctx, frame)
}

type FrameSamplingMetrics struct {
	AcceptedFrames       uint64
	ForwardedFrames      uint64
	RateSuppressedFrames uint64
	DuplicateFrames      uint64
	DownstreamFailures   uint64
}

// AnalyticsFrameSampler is a camera-local, post-mask boundary. It enforces the
// permanent 5-10 FPS cost invariant and suppresses identical consecutive
// sampled frames while retaining only a SHA-256 digest and ordering metadata.
type AnalyticsFrameSampler struct {
	cameraID string
	interval time.Duration
	consumer AnalyticsFrameConsumer

	mu           sync.Mutex
	initialized  bool
	lastSequence uint64
	lastObserved time.Time
	lastSampled  time.Time
	lastDigest   [sha256.Size]byte
	haveDigest   bool
	metrics      FrameSamplingMetrics
}

func NewAnalyticsFrameSampler(cameraID string, framesPerSecond int, consumer AnalyticsFrameConsumer) (*AnalyticsFrameSampler, error) {
	parsed, err := uuid.Parse(cameraID)
	if err != nil || parsed.Version() != 4 || framesPerSecond < MinAnalyticsFrameFPS || framesPerSecond > MaxAnalyticsFrameFPS || consumer == nil {
		return nil, ErrInvalidAnalyticsFrame
	}
	return &AnalyticsFrameSampler{cameraID: parsed.String(), interval: time.Second / time.Duration(framesPerSecond), consumer: consumer}, nil
}

func (s *AnalyticsFrameSampler) ConsumeMaskedFrame(ctx context.Context, frame RGB24Frame) error {
	if s == nil || s.consumer == nil || ctx == nil || validateRGB24Frame(frame) != nil || frame.CameraID != s.cameraID || frame.Sequence == 0 || frame.ObservedAt.IsZero() {
		return ErrInvalidAnalyticsFrame
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := analyticsFrameDigest(frame)
	observedAt := frame.ObservedAt.UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized && (frame.Sequence <= s.lastSequence || observedAt.Before(s.lastObserved)) {
		return ErrInvalidAnalyticsFrame
	}
	s.initialized = true
	s.lastSequence, s.lastObserved = frame.Sequence, observedAt
	s.metrics.AcceptedFrames++
	if !s.lastSampled.IsZero() && observedAt.Sub(s.lastSampled) < s.interval {
		s.metrics.RateSuppressedFrames++
		return nil
	}
	s.lastSampled = observedAt
	if s.haveDigest && digest == s.lastDigest {
		s.metrics.DuplicateFrames++
		return nil
	}
	s.lastDigest, s.haveDigest = digest, true
	frame.ObservedAt = observedAt
	if err := s.consumer.ConsumeAnalyticsFrame(ctx, frame); err != nil {
		s.metrics.DownstreamFailures++
		return err
	}
	s.metrics.ForwardedFrames++
	return nil
}

func (s *AnalyticsFrameSampler) Metrics() FrameSamplingMetrics {
	if s == nil {
		return FrameSamplingMetrics{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metrics
}

func analyticsFrameDigest(frame RGB24Frame) [sha256.Size]byte {
	hash := sha256.New()
	var dimensions [16]byte
	binary.BigEndian.PutUint64(dimensions[0:8], uint64(frame.Width))
	binary.BigEndian.PutUint64(dimensions[8:16], uint64(frame.Height))
	_, _ = hash.Write(dimensions[:])
	_, _ = hash.Write(frame.Pixels)
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}
