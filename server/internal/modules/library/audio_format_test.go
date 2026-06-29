package library

import (
	"path/filepath"
	"testing"
)

func TestEstimateBitrateKbps(t *testing.T) {
	kbps := estimateBitrateKbps(10_000_000, 240_000)
	if kbps < 300 || kbps > 350 {
		t.Fatalf("bitrate = %d, want roughly 333", kbps)
	}
}

func TestReadFlacStreamInfo(t *testing.T) {
	info, ok := readFlacStreamInfo(filepath.Join("..", "..", "..", "music", "The Weeknd-2025-Hurry Up Tomorrow - 01 - Wake Me Up-24bit-88.2Khz.flac"))
	if !ok {
		t.Skip("sample flac not present")
	}
	if info.SampleRateHz != 88200 {
		t.Fatalf("sample rate = %d, want 88200", info.SampleRateHz)
	}
	if info.BitsPerSample != 24 {
		t.Fatalf("bit depth = %d, want 24", info.BitsPerSample)
	}
	if info.DurationMs <= 0 {
		t.Fatalf("duration = %d, want > 0", info.DurationMs)
	}
}
