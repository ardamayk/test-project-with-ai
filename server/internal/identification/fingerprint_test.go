package identification_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

func TestFingerprinterReadsDurationAndFingerprintFromFpcalc(t *testing.T) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		t.Skip("fpcalc not installed on this host")
	}
	fingerprinter := identification.NewFingerprinter("fpcalc")
	path := synthesizedTone(t)

	fingerprint, err := fingerprinter.Fingerprint(context.Background(), path)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if fingerprint.DurationSeconds <= 0 {
		t.Fatalf("DurationSeconds = %v, want positive", fingerprint.DurationSeconds)
	}
	if len(fingerprint.Value) < 16 {
		t.Fatalf("Value = %q, want a compressed chromaprint string", fingerprint.Value)
	}
}

func TestFingerprinterReportsMissingProgram(t *testing.T) {
	fingerprinter := identification.NewFingerprinter(filepath.Join(t.TempDir(), "no-such-fpcalc"))

	_, err := fingerprinter.Fingerprint(context.Background(), "irrelevant.flac")

	if !errors.Is(err, identification.ErrFingerprinterUnavailable) {
		t.Fatalf("error = %v, want ErrFingerprinterUnavailable", err)
	}
}

func TestFingerprinterRejectsUndecodableFile(t *testing.T) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		t.Skip("fpcalc not installed on this host")
	}
	path := filepath.Join(t.TempDir(), "noise.flac")
	if err := os.WriteFile(path, []byte("not audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := identification.NewFingerprinter("fpcalc").Fingerprint(context.Background(), path)

	if !errors.Is(err, identification.ErrFingerprintFailed) {
		t.Fatalf("error = %v, want ErrFingerprintFailed", err)
	}
}

// synthesizedTone renders six seconds of audio with ffmpeg; Chromaprint needs
// several seconds of signal, so the repository's short strict-import fixture
// cannot be fingerprinted.
func synthesizedTone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed on this host")
	}
	path := filepath.Join(t.TempDir(), "tone.flac")
	command := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-f", "lavfi", "-i", "sine=frequency=660:duration=6", "-filter_complex", "amix", "-ar", "44100", "-y", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("synthesize tone: %v: %s", err, output)
	}
	return path
}
