package library_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	_ "modernc.org/sqlite"
)

func TestScanBackfillsAlbumCoverFromFlac(t *testing.T) {
	flacPath := filepath.Join("..", "..", "..", "music", "Taylor Swift _The Life of a Showgirl _01_The Fate of Ophelia.flac")
	if _, err := os.Stat(flacPath); err != nil {
		t.Skip("sample flac not present")
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE artists (id TEXT PRIMARY KEY, name TEXT, name_sort TEXT, created_at TEXT, updated_at TEXT);
		CREATE TABLE albums (id TEXT PRIMARY KEY, artist_id TEXT, title TEXT, title_sort TEXT, year INTEGER, genres TEXT NOT NULL DEFAULT '[]', cover_mime TEXT, cover_data BLOB, created_at TEXT, updated_at TEXT);
		CREATE TABLE tracks (id TEXT PRIMARY KEY, album_id TEXT, title TEXT, title_sort TEXT, artist_name TEXT, track_no INTEGER, duration_ms INTEGER, format TEXT, size_bytes INTEGER, file_path TEXT UNIQUE, file_mtime INTEGER, missing_at TEXT, genre TEXT, created_at TEXT, updated_at TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(flacPath)
	store := library.NewStore(db)
	files, err := library.WalkMusicPaths([]string{filepath.Dir(absPath)})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected scanned files")
	}
	if len(files[0].Metadata.CoverData) == 0 {
		t.Fatal("expected embedded cover in flac metadata")
	}

	_, _, err = store.UpsertFromScan(context.Background(), files[0].Metadata)
	if err != nil {
		t.Fatal(err)
	}

	var coverLen int
	err = db.QueryRow(`SELECT length(cover_data) FROM albums`).Scan(&coverLen)
	if err != nil {
		t.Fatal(err)
	}
	if coverLen == 0 {
		t.Fatalf("expected cover stored in albums table")
	}
}
