package agent

import (
	"errors"
	"strings"
)

var ErrInvalidDecodeProfile = errors.New("video decode profile is invalid")

type VideoCodec string

const (
	CodecH264 VideoCodec = "h264"
	CodecH265 VideoCodec = "h265"
)

type DecodePreference string

const (
	DecodeAuto         DecodePreference = "auto"
	DecodeSoftware     DecodePreference = "software"
	DecodeNVIDIAJetson DecodePreference = "nvidia-jetson"
	DecodeNVIDIACUDA   DecodePreference = "nvidia-cuda"
)

// DecodeProfile declares the codec discovered during camera onboarding and
// the acceleration policy for the edge tier. AvailableDecoders must come from
// the trusted local FFmpeg capability probe, never from tenant input.
type DecodeProfile struct {
	Codec             VideoCodec
	Preference        DecodePreference
	AvailableDecoders []string
}

type DecoderSelection struct {
	Codec               VideoCodec
	Name                string
	HardwareAccelerated bool
	Backend             DecodePreference
}

// SelectDecoder chooses a known decoder without passing capability strings to
// the command line. Auto prefers the Jetson path, then CUDA, and falls back to
// FFmpeg's software decoder. A forced hardware backend fails closed when the
// matching decoder is unavailable.
func SelectDecoder(profile DecodeProfile) (DecoderSelection, error) {
	codec, err := normalizeVideoCodec(profile.Codec)
	if err != nil {
		return DecoderSelection{}, err
	}
	preference := DecodePreference(strings.ToLower(strings.TrimSpace(string(profile.Preference))))
	if preference == "" {
		preference = DecodeAuto
	}
	available := make(map[string]struct{}, len(profile.AvailableDecoders))
	for _, decoder := range profile.AvailableDecoders {
		available[strings.ToLower(strings.TrimSpace(decoder))] = struct{}{}
	}

	software, jetson, cuda := decoderNames(codec)
	selection := func(name string, backend DecodePreference, hardware bool) DecoderSelection {
		return DecoderSelection{Codec: codec, Name: name, HardwareAccelerated: hardware, Backend: backend}
	}
	has := func(name string) bool {
		_, ok := available[name]
		return ok
	}

	switch preference {
	case DecodeAuto:
		if has(jetson) {
			return selection(jetson, DecodeNVIDIAJetson, true), nil
		}
		if has(cuda) {
			return selection(cuda, DecodeNVIDIACUDA, true), nil
		}
		return selection(software, DecodeSoftware, false), nil
	case DecodeSoftware:
		return selection(software, DecodeSoftware, false), nil
	case DecodeNVIDIAJetson:
		if !has(jetson) {
			return DecoderSelection{}, ErrInvalidDecodeProfile
		}
		return selection(jetson, DecodeNVIDIAJetson, true), nil
	case DecodeNVIDIACUDA:
		if !has(cuda) {
			return DecoderSelection{}, ErrInvalidDecodeProfile
		}
		return selection(cuda, DecodeNVIDIACUDA, true), nil
	default:
		return DecoderSelection{}, ErrInvalidDecodeProfile
	}
}

func normalizeVideoCodec(codec VideoCodec) (VideoCodec, error) {
	switch strings.ToLower(strings.TrimSpace(string(codec))) {
	case "h264", "avc", "avc1":
		return CodecH264, nil
	case "h265", "hevc", "hev1":
		return CodecH265, nil
	default:
		return "", ErrInvalidDecodeProfile
	}
}

func decoderNames(codec VideoCodec) (software, jetson, cuda string) {
	if codec == CodecH265 {
		return "hevc", "hevc_nvv4l2dec", "hevc_cuvid"
	}
	return "h264", "h264_nvv4l2dec", "h264_cuvid"
}
