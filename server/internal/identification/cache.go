package identification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// CachedAcoustID stores AcoustID answers in the database keyed by the
// fingerprint, indefinitely (ADR 0017): the same audio re-uploaded, imported
// separately or replaced never costs a second request. Failures are never
// cached; an empty answer is, because AcoustID's silence for a fingerprint is
// itself an answer.
type CachedAcoustID struct {
	database *sql.DB
	upstream AcoustIDLookup
}

func NewCachedAcoustID(database *sql.DB, upstream AcoustIDLookup) *CachedAcoustID {
	return &CachedAcoustID{database: database, upstream: upstream}
}

func (cached *CachedAcoustID) Lookup(ctx context.Context, fingerprint Fingerprint) ([]AcoustIDResult, error) {
	key := fingerprintKey(fingerprint)
	var stored string
	err := cached.database.QueryRowContext(ctx, `SELECT results_json FROM acoustid_lookup_cache WHERE fingerprint_sha256 = ?`, key).Scan(&stored)
	switch {
	case err == nil:
		var results []AcoustIDResult
		if decodeErr := json.Unmarshal([]byte(stored), &results); decodeErr == nil {
			return results, nil
		}
		slog.Warn("ignoring undecodable AcoustID cache row", "fingerprint", key)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("%w: read AcoustID cache: %w", ErrServiceUnavailable, err)
	}

	results, err := cached.upstream.Lookup(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("encode AcoustID results: %w", err)
	}
	duration := max(int(fingerprint.DurationSeconds), 1)
	if _, err := cached.database.ExecContext(ctx, `
		INSERT INTO acoustid_lookup_cache (fingerprint_sha256, duration_seconds, results_json)
		VALUES (?, ?, ?)
		ON CONFLICT(fingerprint_sha256) DO UPDATE SET results_json = excluded.results_json, fetched_at = CURRENT_TIMESTAMP`,
		key, duration, string(encoded)); err != nil {
		return nil, fmt.Errorf("store AcoustID cache: %w", err)
	}
	return results, nil
}

func fingerprintKey(fingerprint Fingerprint) string {
	sum := sha256.Sum256([]byte(fingerprint.Value))
	return hex.EncodeToString(sum[:])
}

// CachedMusicBrainz stores fetched Recordings by MBID. A not-found answer is
// not cached so a Recording created or merged later is picked up.
type CachedMusicBrainz struct {
	database *sql.DB
	upstream RecordingSource
}

func NewCachedMusicBrainz(database *sql.DB, upstream RecordingSource) *CachedMusicBrainz {
	return &CachedMusicBrainz{database: database, upstream: upstream}
}

// RecordingIDsByISRC is not cached: it is a small lookup that only runs for
// files tagged with an ISRC but no recording MBID.
func (cached *CachedMusicBrainz) RecordingIDsByISRC(ctx context.Context, isrc string) ([]string, error) {
	return cached.upstream.RecordingIDsByISRC(ctx, isrc)
}

func (cached *CachedMusicBrainz) Recording(ctx context.Context, mbid string) (Recording, error) {
	var stored string
	err := cached.database.QueryRowContext(ctx, `SELECT recording_json FROM musicbrainz_recording_cache WHERE recording_id = ?`, mbid).Scan(&stored)
	switch {
	case err == nil:
		var recording Recording
		if decodeErr := json.Unmarshal([]byte(stored), &recording); decodeErr == nil {
			return recording, nil
		}
		slog.Warn("ignoring undecodable MusicBrainz cache row", "recording", mbid)
	case !errors.Is(err, sql.ErrNoRows):
		return Recording{}, fmt.Errorf("%w: read MusicBrainz cache: %w", ErrServiceUnavailable, err)
	}

	recording, err := cached.upstream.Recording(ctx, mbid)
	if err != nil {
		return Recording{}, err
	}
	encoded, err := json.Marshal(recording)
	if err != nil {
		return Recording{}, fmt.Errorf("encode MusicBrainz recording: %w", err)
	}
	if _, err := cached.database.ExecContext(ctx, `
		INSERT INTO musicbrainz_recording_cache (recording_id, recording_json)
		VALUES (?, ?)
		ON CONFLICT(recording_id) DO UPDATE SET recording_json = excluded.recording_json, fetched_at = CURRENT_TIMESTAMP`,
		mbid, string(encoded)); err != nil {
		return Recording{}, fmt.Errorf("store MusicBrainz cache: %w", err)
	}
	return recording, nil
}
