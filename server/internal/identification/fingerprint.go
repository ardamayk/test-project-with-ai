// Package identification resolves an audio file to a MusicBrainz Recording:
// fpcalc fingerprints the file, AcoustID maps the fingerprint to recording
// MBIDs, and the MusicBrainz web service supplies the Recording's metadata
// (ADR 0017). Nothing here writes to the audio file.
package identification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var (
	// ErrFingerprinterUnavailable means fpcalc could not be started at all.
	ErrFingerprinterUnavailable = errors.New("fpcalc is not available")
	// ErrFingerprintFailed means fpcalc ran but could not fingerprint the file.
	ErrFingerprintFailed = errors.New("fpcalc could not fingerprint the file")
)

const fingerprintTimeout = 60 * time.Second

// Fingerprint is fpcalc's compressed Chromaprint output for one file.
type Fingerprint struct {
	DurationSeconds float64
	Value           string
}

// Fingerprinter runs fpcalc for one file at a time.
type Fingerprinter struct {
	program string
}

// NewFingerprinter uses the given fpcalc program name or path.
func NewFingerprinter(program string) *Fingerprinter {
	return &Fingerprinter{program: program}
}

type fpcalcOutput struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

// Fingerprint runs `fpcalc -json` and parses its duration and fingerprint.
func (fingerprinter *Fingerprinter) Fingerprint(ctx context.Context, path string) (Fingerprint, error) {
	ctx, cancel := context.WithTimeout(ctx, fingerprintTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, fingerprinter.program, "-json", path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Fingerprint{}, fmt.Errorf("%w: %s", ErrFingerprintFailed, firstLine(stderr.String()))
		}
		return Fingerprint{}, fmt.Errorf("%w: %w", ErrFingerprinterUnavailable, err)
	}
	var parsed fpcalcOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return Fingerprint{}, fmt.Errorf("%w: parse fpcalc output: %w", ErrFingerprintFailed, err)
	}
	if parsed.Fingerprint == "" || parsed.Duration <= 0 {
		return Fingerprint{}, fmt.Errorf("%w: fpcalc returned no fingerprint", ErrFingerprintFailed)
	}
	return Fingerprint{DurationSeconds: parsed.Duration, Value: parsed.Fingerprint}, nil
}

func firstLine(text string) string {
	for index, char := range text {
		if char == '\n' {
			return text[:index]
		}
	}
	return text
}
