package identification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
)

// Outcome says how far Recording Identification got for one file.
type Outcome string

const (
	// OUTCOME_MATCHED means a confident AcoustID match resolved to a MusicBrainz Recording.
	OUTCOME_MATCHED Outcome = "matched"
	// OUTCOME_NO_MATCH means AcoustID or MusicBrainz knows nothing usable about the audio.
	OUTCOME_NO_MATCH Outcome = "no_match"
	// OUTCOME_BELOW_THRESHOLD means the best AcoustID score is under the configured minimum.
	OUTCOME_BELOW_THRESHOLD Outcome = "below_threshold"
	// OUTCOME_UNAVAILABLE means fpcalc or a service failed, so nothing can be concluded.
	OUTCOME_UNAVAILABLE Outcome = "unavailable"
)

// Identification is the result for one file. Recording is set only for
// OUTCOME_MATCHED; Reason explains every other outcome for the Import Preview.
type Identification struct {
	Outcome     Outcome
	Reason      string
	Fingerprint Fingerprint
	AcoustID    string
	Score       float64
	Recording   *Recording
}

// FingerprintSource, AcoustIDLookup and RecordingSource are the three seams
// the Identifier orchestrates; the real implementations live in this package.
type FingerprintSource interface {
	Fingerprint(ctx context.Context, path string) (Fingerprint, error)
}

type AcoustIDLookup interface {
	Lookup(ctx context.Context, fingerprint Fingerprint) ([]AcoustIDResult, error)
}

type RecordingSource interface {
	Recording(ctx context.Context, mbid string) (Recording, error)
}

// Identifier runs fingerprint, AcoustID lookup and MusicBrainz lookup in order.
type Identifier struct {
	fingerprinter FingerprintSource
	acoustID      AcoustIDLookup
	musicBrainz   RecordingSource
	minScore      float64
}

const (
	requestTimeout       = 10 * time.Second
	musicBrainzInterval  = time.Second
	acoustIDInterval     = time.Second / 3
	userAgentProjectLink = "https://github.com/ardamayk/test-project-with-ai"
)

// Package-level limiters make the rate bound server-wide even if more than
// one Identifier is ever constructed.
var (
	acoustIDLimiter    = NewLimiter(acoustIDInterval)
	musicBrainzLimiter = NewLimiter(musicBrainzInterval)
)

// NewIdentifier wires the real fpcalc program and HTTP clients from
// configuration. Whether identification should run at all (cfg.Enabled, the
// Import Batch switch, fpcalc presence) is the caller's decision.
func NewIdentifier(cfg config.RecordingIdentificationConfig, version, fpcalcProgram string) *Identifier {
	userAgent := fmt.Sprintf("EarthlyAudio/%s (%s)", version, userAgentProjectLink)
	httpClient := &http.Client{Timeout: requestTimeout}
	return NewIdentifierWithSources(
		NewFingerprinter(fpcalcProgram),
		limitedAcoustID{client: NewAcoustIDClient(httpClient, cfg.AcoustIDBaseURL, cfg.AcoustIDAPIKey, userAgent), limiter: acoustIDLimiter},
		limitedMusicBrainz{client: NewMusicBrainzClient(httpClient, cfg.MusicBrainzBaseURL, userAgent), limiter: musicBrainzLimiter},
		cfg.MinScore,
	)
}

// NewIdentifierWithSources accepts the three seams directly for tests.
func NewIdentifierWithSources(fingerprinter FingerprintSource, acoustID AcoustIDLookup, musicBrainz RecordingSource, minScore float64) *Identifier {
	return &Identifier{fingerprinter: fingerprinter, acoustID: acoustID, musicBrainz: musicBrainz, minScore: minScore}
}

// Identify never returns an error: every failure becomes an Outcome with a
// Reason so Managed Import can fall back to the file's tags and tell the
// user why.
func (identifier *Identifier) Identify(ctx context.Context, path string) Identification {
	fingerprint, err := identifier.fingerprinter.Fingerprint(ctx, path)
	if err != nil {
		return Identification{Outcome: OUTCOME_UNAVAILABLE, Reason: err.Error()}
	}
	result := Identification{Fingerprint: fingerprint}

	results, err := identifier.acoustID.Lookup(ctx, fingerprint)
	if err != nil {
		result.Outcome, result.Reason = OUTCOME_UNAVAILABLE, err.Error()
		return result
	}
	best, recording, found := bestRecording(results)
	if !found {
		result.Outcome, result.Reason = OUTCOME_NO_MATCH, "AcoustID has no recording for this fingerprint"
		return result
	}
	result.AcoustID, result.Score = best.ID, best.Score
	if best.Score < identifier.minScore {
		result.Outcome = OUTCOME_BELOW_THRESHOLD
		result.Reason = fmt.Sprintf("best AcoustID score %.2f is below the minimum %.2f", best.Score, identifier.minScore)
		return result
	}

	fetched, err := identifier.musicBrainz.Recording(ctx, recording.ID)
	switch {
	case errors.Is(err, ErrRecordingNotFound):
		result.Outcome, result.Reason = OUTCOME_NO_MATCH, err.Error()
	case err != nil:
		result.Outcome, result.Reason = OUTCOME_UNAVAILABLE, err.Error()
	default:
		result.Outcome, result.Recording = OUTCOME_MATCHED, &fetched
	}
	return result
}

// bestRecording picks the highest-scoring AcoustID result that links at
// least one recording, then the recording with the most sources within it.
func bestRecording(results []AcoustIDResult) (AcoustIDResult, AcoustIDRecording, bool) {
	var best AcoustIDResult
	var found bool
	for _, result := range results {
		if len(result.Recordings) == 0 {
			continue
		}
		if !found || result.Score > best.Score {
			best, found = result, true
		}
	}
	if !found {
		return AcoustIDResult{}, AcoustIDRecording{}, false
	}
	chosen := best.Recordings[0]
	for _, recording := range best.Recordings[1:] {
		if recording.Sources > chosen.Sources {
			chosen = recording
		}
	}
	return best, chosen, true
}

type limitedAcoustID struct {
	client  *AcoustIDClient
	limiter *Limiter
}

func (limited limitedAcoustID) Lookup(ctx context.Context, fingerprint Fingerprint) ([]AcoustIDResult, error) {
	if err := limited.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServiceUnavailable, err)
	}
	return limited.client.Lookup(ctx, fingerprint)
}

type limitedMusicBrainz struct {
	client  *MusicBrainzClient
	limiter *Limiter
}

func (limited limitedMusicBrainz) Recording(ctx context.Context, mbid string) (Recording, error) {
	if err := limited.limiter.Wait(ctx); err != nil {
		return Recording{}, fmt.Errorf("%w: %w", ErrServiceUnavailable, err)
	}
	return limited.client.Recording(ctx, mbid)
}
