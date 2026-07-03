package testutil

import (
	"database/sql"
	"testing"
)

func InsertTrack(t *testing.T, db *sql.DB, trackID string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO artists (id, name, name_sort) VALUES ('artist-1', 'Artist', 'artist')
		ON CONFLICT(id) DO NOTHING;
		INSERT INTO albums (id, artist_id, title, title_sort) VALUES ('album-1', 'artist-1', 'Album', 'album')
		ON CONFLICT(id) DO NOTHING;
		INSERT INTO tracks (id, album_id, title, title_sort, artist_name, duration_ms, format, size_bytes, file_path, file_mtime)
		VALUES (?, 'album-1', 'Song', 'song', 'Artist', 1000, 'flac', 10, ?, 1)
		ON CONFLICT(id) DO NOTHING;
	`, trackID, trackID+".flac")
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
}
