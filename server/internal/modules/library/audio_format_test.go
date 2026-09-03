package library

import (
	"testing"
)

func TestEstimateBitrateKbps(t *testing.T) {
	kbps := estimateBitrateKbps(10_000_000, 240_000)
	if kbps < 300 || kbps > 350 {
		t.Fatalf("bitrate = %d, want roughly 333", kbps)
	}
}

func TestEnrichTrackBitrate(t *testing.T) {
	track := Track{
		DurationMs: 240_000,
		SizeBytes:  10_000_000,
	}
	enrichTrackBitrate(&track)
	if track.BitrateKbps < 300 || track.BitrateKbps > 350 {
		t.Fatalf("bitrate = %d, want roughly 333", track.BitrateKbps)
	}

	withFlac := Track{BitDepth: 24, SampleRateHz: 96000, DurationMs: 1000, SizeBytes: 1_000_000}
	enrichTrackBitrate(&withFlac)
	if withFlac.BitrateKbps != 0 {
		t.Fatalf("lossless track should not get bitrate fallback, got %d", withFlac.BitrateKbps)
	}
}
