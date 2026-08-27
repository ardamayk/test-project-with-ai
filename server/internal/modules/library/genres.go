package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

var genreDelimiterPattern = regexp.MustCompile(`[;/|,]+`)

func splitGenres(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := genreDelimiterPattern.Split(raw, -1)
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return out
}

func mergeGenres(genres ...[]string) []string {
	seen := make(map[string]string)
	for _, list := range genres {
		for _, genre := range list {
			genre = strings.TrimSpace(genre)
			if genre == "" {
				continue
			}
			key := strings.ToLower(genre)
			if _, ok := seen[key]; !ok {
				seen[key] = genre
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, genre := range seen {
		out = append(out, genre)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func encodeGenres(genres []string) string {
	if len(genres) == 0 {
		return "[]"
	}
	data, err := json.Marshal(genres)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeGenres(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var genres []string
	if err := json.Unmarshal([]byte(raw), &genres); err != nil {
		return splitGenres(raw)
	}
	return mergeGenres(genres)
}

func (s *Store) recomputeAlbumGenres(ctx context.Context, albumID string) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT genre FROM tracks
		WHERE album_id = ? AND missing_at IS NULL AND genre IS NOT NULL AND genre != ''`, albumID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	collected := []string{}
	for rows.Next() {
		var genre string
		if scanErr := rows.Scan(&genre); scanErr != nil {
			return scanErr
		}
		collected = append(collected, splitGenres(genre)...)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return rowsErr
	}

	genres := mergeGenres(collected)
	_, err = s.db.ExecContext(ctx, `
		UPDATE albums SET genres = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, encodeGenres(genres), albumID)
	return err
}

func (s *Store) RecomputeAllAlbumGenres(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM albums`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var albumID string
		if err := rows.Scan(&albumID); err != nil {
			return err
		}
		if err := s.recomputeAlbumGenres(ctx, albumID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) setTrackGenre(ctx context.Context, trackID, genre string) error {
	if genre == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tracks SET genre = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, genre, trackID)
	return err
}

func scanAlbumGenres(nullable sql.NullString) []string {
	if !nullable.Valid {
		return nil
	}
	return decodeGenres(nullable.String)
}
