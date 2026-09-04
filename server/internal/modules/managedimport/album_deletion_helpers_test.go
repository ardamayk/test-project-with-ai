package managedimport

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// seedManagedAlbumTrack seeds one Managed Track with distinct bytes so several
// tracks can share an album under the unique content-hash constraint.
func SeedManagedAlbumTrackForTest(t *testing.T, database *sql.DB, root, albumID, artistID, trackID string, trackNo int, title string) string {
	t.Helper()
	trackPath := filepath.Join(root, "library", artistID, albumID, trackID+".flac")
	contents := map[string]string{"track-1": "first-bytes", "track-2": "second-bytes", "track-3": "third-bytes"}[trackID]
	writeAlbumDeletionFile(t, trackPath, contents)
	contentSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT OR IGNORE INTO artists (id, name, name_sort, name_normalized) VALUES (?, 'Artist', 'artist', 'artist')`, []any{artistID}},
		{`INSERT OR IGNORE INTO albums (id, artist_id, title, title_sort, identity_key) VALUES (?, ?, 'Album', 'album', ?)`, []any{albumID, artistID, albumID}},
		{`INSERT OR IGNORE INTO album_artists (album_id, artist_id, position) VALUES (?, ?, 0)`, []any{albumID, artistID}},
		{`INSERT INTO tracks (id, album_id, title, title_sort, artist_name, track_no, disc_no, duration_ms, format, size_bytes, file_path, identity_key) VALUES (?, ?, ?, ?, 'Artist', ?, 1, 1000, 'flac', ?, ?, ?)`, []any{trackID, albumID, title, title, trackNo, len(contents), trackPath, trackID}},
		{`INSERT INTO track_sources (id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes) VALUES (?, ?, 'managed', ?, ?, 'flac', ?)`, []any{"source-" + trackID, trackID, trackPath, contentSHA256, len(contents)}},
		{`INSERT INTO track_artists (track_id, artist_id, position) VALUES (?, ?, 0)`, []any{trackID, artistID}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed Managed Track %s: %v", trackID, err)
		}
	}
	return trackPath
}

func writeAlbumDeletionFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
