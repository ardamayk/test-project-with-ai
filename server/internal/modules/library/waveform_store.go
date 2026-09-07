package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) GetTrackWaveformSource(ctx context.Context, trackID string) (WaveformSource, error) {
	var source WaveformSource
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(track_sources.file_path, tracks.file_path), tracks.duration_ms
		FROM visible_tracks tracks LEFT JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ?`, trackID,
	).Scan(&source.FilePath, &source.DurationMs)
	if errors.Is(err, sql.ErrNoRows) {
		return WaveformSource{}, ErrNotFound
	}
	if err != nil {
		return WaveformSource{}, fmt.Errorf("get waveform source %q: %w", trackID, err)
	}
	return source, nil
}

func (s *Store) GetTrackWaveform(ctx context.Context, trackID string) (WaveformRecord, bool, error) {
	var record WaveformRecord
	err := s.db.QueryRowContext(ctx,
		`SELECT peaks, source_size_bytes, source_modified_at FROM track_waveforms WHERE track_id = ?`,
		trackID,
	).Scan(&record.Peaks, &record.SourceSizeBytes, &record.SourceModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WaveformRecord{}, false, nil
	}
	if err != nil {
		return WaveformRecord{}, false, fmt.Errorf("get waveform %q: %w", trackID, err)
	}
	return record, true, nil
}

func (s *Store) PutTrackWaveform(ctx context.Context, trackID string, record WaveformRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO track_waveforms (track_id, peak_count, peaks, source_size_bytes, source_modified_at, generated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			peak_count = excluded.peak_count,
			peaks = excluded.peaks,
			source_size_bytes = excluded.source_size_bytes,
			source_modified_at = excluded.source_modified_at,
			generated_at = CURRENT_TIMESTAMP`,
		trackID, len(record.Peaks), record.Peaks, record.SourceSizeBytes, record.SourceModifiedAt,
	)
	if err != nil {
		return fmt.Errorf("put waveform %q: %w", trackID, err)
	}
	return nil
}
