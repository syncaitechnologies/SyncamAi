package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const maxMaskFrameDimension = 8192

var ErrInvalidPrivacyMaskFrame = errors.New("privacy mask frame is invalid")

// RGB24Frame transfers ownership of one decoded frame to a synchronous local
// consumer. Pixels must contain exactly Width*Height tightly packed RGB bytes;
// callers must not reuse or inspect the buffer after forwarding it.
type RGB24Frame struct {
	CameraID string
	Width    int
	Height   int
	Pixels   []byte
}

// MaskedFrameConsumer receives only a frame after the approved mask has been
// applied in place. Implementations must remain inside the trusted edge
// boundary and must not retain raw pixels outside an approved evidence path.
type MaskedFrameConsumer interface {
	ConsumeMaskedFrame(context.Context, RGB24Frame) error
}

type MaskedFrameConsumerFunc func(context.Context, RGB24Frame) error

func (f MaskedFrameConsumerFunc) ConsumeMaskedFrame(ctx context.Context, frame RGB24Frame) error {
	return f(ctx, frame)
}

// PreAnalyticsPrivacyMask is the only frame-forwarding boundary for this
// slice. Construction requires the HIL-gated activation produced by the
// existing hardware adapter for the same two-approver candidate.
type PreAnalyticsPrivacyMask struct {
	cameraID       string
	releaseID      string
	releaseVersion int64
	candidateHash  string
	rings          [][][]float64
	adapter        *HardwareBoundPrivacyMaskAdapter
	consumer       MaskedFrameConsumer
	maskMu         sync.Mutex
	maskWidth      int
	maskHeight     int
	maskPixels     []bool
}

func NewPreAnalyticsPrivacyMask(candidate PrivacyMaskCandidate, adapter *HardwareBoundPrivacyMaskAdapter, consumer MaskedFrameConsumer) (*PreAnalyticsPrivacyMask, error) {
	if adapter == nil || consumer == nil {
		return nil, ErrInvalidPrivacyMaskFrame
	}
	activation := adapter.ActiveRelease()
	if activation == nil || !validFrameMaskActivation(*activation) {
		return nil, ErrInvalidPrivacyMaskFrame
	}
	verification, err := VerifyPreEncodePrivacyMask(candidate, PreEncodePipeline{Stages: []PipelineStage{PipelineDecode, PipelineMask, PipelineEncode}})
	if err != nil || activation.CandidateHash != verification.CandidateHash {
		return nil, ErrInvalidPrivacyMaskFrame
	}
	var geometry struct {
		Coordinates [][][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(candidate.Geometry, &geometry); err != nil || len(geometry.Coordinates) == 0 {
		return nil, ErrInvalidPrivacyMaskFrame
	}
	return &PreAnalyticsPrivacyMask{
		cameraID:       candidate.CameraID,
		releaseID:      activation.ReleaseID,
		releaseVersion: activation.Version,
		candidateHash:  verification.CandidateHash,
		rings:          cloneMaskRings(geometry.Coordinates),
		adapter:        adapter,
		consumer:       consumer,
	}, nil
}

// Forward validates the complete frame before changing any pixel, applies an
// opaque black mask, and synchronously transfers the masked buffer. Consumer
// failures never restore the original pixels.
func (m *PreAnalyticsPrivacyMask) Forward(ctx context.Context, frame RGB24Frame) error {
	if m == nil || m.consumer == nil || m.adapter == nil || ctx == nil || validateRGB24Frame(frame) != nil || frame.CameraID != m.cameraID {
		return ErrInvalidPrivacyMaskFrame
	}
	active := m.adapter.ActiveRelease()
	if active == nil || active.ReleaseID != m.releaseID || active.Version != m.releaseVersion || active.CandidateHash != m.candidateHash {
		return ErrInvalidPrivacyMaskFrame
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mask := m.maskFor(frame.Width, frame.Height)
	for pixel, masked := range mask {
		if !masked {
			continue
		}
		offset := pixel * 3
		frame.Pixels[offset], frame.Pixels[offset+1], frame.Pixels[offset+2] = 0, 0, 0
	}
	return m.consumer.ConsumeMaskedFrame(ctx, frame)
}

func validFrameMaskActivation(activation HardwarePrivacyMaskActivation) bool {
	profileID := strings.TrimSpace(activation.ProfileID)
	if profileID == "" || profileID != activation.ProfileID || len(profileID) > maxPrivacyMaskHardwareProfileIDLength || activation.Version < 1 || !isStrictPreEncodePipeline(activation.Pipeline) {
		return false
	}
	releaseID, releaseErr := uuid.Parse(activation.ReleaseID)
	deviceID, deviceErr := uuid.Parse(activation.DeviceID)
	return releaseErr == nil && releaseID.Version() == 4 && deviceErr == nil && deviceID.Version() == 4
}

func (m *PreAnalyticsPrivacyMask) maskFor(width, height int) []bool {
	m.maskMu.Lock()
	defer m.maskMu.Unlock()
	if m.maskWidth == width && m.maskHeight == height && len(m.maskPixels) == width*height {
		return m.maskPixels
	}
	mask := make([]bool, width*height)
	for y := 0; y < height; y++ {
		py := (float64(y) + 0.5) / float64(height)
		for x := 0; x < width; x++ {
			px := (float64(x) + 0.5) / float64(width)
			mask[y*width+x] = maskedPoint(px, py, m.rings)
		}
	}
	m.maskWidth, m.maskHeight, m.maskPixels = width, height, mask
	return m.maskPixels
}

func (m *PreAnalyticsPrivacyMask) CandidateHash() string {
	if m == nil {
		return ""
	}
	return m.candidateHash
}

func validateRGB24Frame(frame RGB24Frame) error {
	parsed, err := uuid.Parse(frame.CameraID)
	if err != nil || parsed.Version() != 4 || frame.Width <= 0 || frame.Width > maxMaskFrameDimension || frame.Height <= 0 || frame.Height > maxMaskFrameDimension {
		return ErrInvalidPrivacyMaskFrame
	}
	want := int64(frame.Width) * int64(frame.Height) * 3
	if want <= 0 || want > int64(maxMaskFrameDimension)*int64(maxMaskFrameDimension)*3 || int64(len(frame.Pixels)) != want {
		return ErrInvalidPrivacyMaskFrame
	}
	return nil
}

func maskedPoint(x, y float64, rings [][][]float64) bool {
	insideOuter, boundary := pointInMaskRing(x, y, rings[0])
	if boundary {
		return true
	}
	if !insideOuter {
		return false
	}
	for _, hole := range rings[1:] {
		insideHole, holeBoundary := pointInMaskRing(x, y, hole)
		if holeBoundary {
			return true
		}
		if insideHole {
			return false
		}
	}
	return true
}

func pointInMaskRing(x, y float64, ring [][]float64) (inside, boundary bool) {
	for index, point := range ring[:len(ring)-1] {
		next := ring[index+1]
		if pointOnSegment(x, y, point[0], point[1], next[0], next[1]) {
			return true, true
		}
		if (point[1] > y) == (next[1] > y) {
			continue
		}
		intersection := (next[0]-point[0])*(y-point[1])/(next[1]-point[1]) + point[0]
		if x < intersection {
			inside = !inside
		}
	}
	return inside, false
}

func pointOnSegment(px, py, ax, ay, bx, by float64) bool {
	cross := (px-ax)*(by-ay) - (py-ay)*(bx-ax)
	if math.Abs(cross) > 1e-12 {
		return false
	}
	return px >= math.Min(ax, bx)-1e-12 && px <= math.Max(ax, bx)+1e-12 && py >= math.Min(ay, by)-1e-12 && py <= math.Max(ay, by)+1e-12
}

func cloneMaskRings(rings [][][]float64) [][][]float64 {
	cloned := make([][][]float64, len(rings))
	for ringIndex, ring := range rings {
		cloned[ringIndex] = make([][]float64, len(ring))
		for pointIndex, point := range ring {
			cloned[ringIndex][pointIndex] = append([]float64(nil), point...)
		}
	}
	return cloned
}
