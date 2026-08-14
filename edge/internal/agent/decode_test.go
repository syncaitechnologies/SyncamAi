package agent

import (
	"errors"
	"testing"
)

func TestSelectDecoderSupportsH264AndH265Software(t *testing.T) {
	for _, test := range []struct {
		codec VideoCodec
		name  string
	}{
		{codec: CodecH264, name: "h264"},
		{codec: CodecH265, name: "hevc"},
		{codec: "hevc", name: "hevc"},
	} {
		selection, err := SelectDecoder(DecodeProfile{Codec: test.codec, Preference: DecodeSoftware})
		if err != nil {
			t.Fatalf("select %s: %v", test.codec, err)
		}
		if selection.Name != test.name || selection.HardwareAccelerated || selection.Backend != DecodeSoftware {
			t.Fatalf("unexpected selection for %s: %+v", test.codec, selection)
		}
	}
}

func TestSelectDecoderAutoPrefersHardwareAndFallsBack(t *testing.T) {
	jetson, err := SelectDecoder(DecodeProfile{Codec: CodecH264, Preference: DecodeAuto, AvailableDecoders: []string{"h264_cuvid", "h264_nvv4l2dec"}})
	if err != nil || jetson.Name != "h264_nvv4l2dec" || !jetson.HardwareAccelerated || jetson.Backend != DecodeNVIDIAJetson {
		t.Fatalf("unexpected Jetson selection: %+v err=%v", jetson, err)
	}
	cuda, err := SelectDecoder(DecodeProfile{Codec: CodecH265, AvailableDecoders: []string{"hevc_cuvid"}})
	if err != nil || cuda.Name != "hevc_cuvid" || !cuda.HardwareAccelerated || cuda.Backend != DecodeNVIDIACUDA {
		t.Fatalf("unexpected CUDA selection: %+v err=%v", cuda, err)
	}
	software, err := SelectDecoder(DecodeProfile{Codec: CodecH265})
	if err != nil || software.Name != "hevc" || software.HardwareAccelerated {
		t.Fatalf("unexpected software fallback: %+v err=%v", software, err)
	}
}

func TestSelectDecoderRejectsUnsupportedOrUnavailableProfiles(t *testing.T) {
	for _, profile := range []DecodeProfile{
		{},
		{Codec: "mpeg4"},
		{Codec: CodecH264, Preference: "shell"},
		{Codec: CodecH264, Preference: DecodeNVIDIAJetson, AvailableDecoders: []string{"h264"}},
		{Codec: CodecH265, Preference: DecodeNVIDIACUDA, AvailableDecoders: []string{"hevc"}},
	} {
		if _, err := SelectDecoder(profile); !errors.Is(err, ErrInvalidDecodeProfile) {
			t.Fatalf("profile %+v: expected validation error, got %v", profile, err)
		}
	}
}
