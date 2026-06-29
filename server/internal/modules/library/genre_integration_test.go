package library_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	_ "modernc.org/sqlite"
)

func TestScanStoresTrackAndAlbumGenresFromFlac(t *testing.T) {
	musicDir := filepath.Join("..", "..", "..", "music")
	weekndFlac := filepath.Join(musicDir, "The Weeknd-2025-Hurry Up Tomorrow - 01 - Wake Me Up-24bit-88.2Khz.flac")
	if _, err := os.Stat(weekndFlac); err != nil {
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

	store := library.NewStore(db)
	files, err := library.WalkMusicPaths([]string{musicDir})
	if err != nil {
		t.Fatal(err)
	}

	var meta library.FileMetadata
	for _, file := range files {
		if filepath.Base(file.Metadata.Path) == filepath.Base(weekndFlac) {
			meta = file.Metadata
			break
		}
	}
	if meta.Path == "" || meta.Genre == "" {
		t.Fatal("expected weeknd track metadata with genre")
	}

	_, _, err = store.UpsertFromScan(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}

	var trackGenre string
	err = db.QueryRow(`SELECT genre FROM tracks`).Scan(&trackGenre)
	if err != nil {
		t.Fatal(err)
	}
	if trackGenre != meta.Genre {
		t.Fatalf("track genre = %q, want %q", trackGenre, meta.Genre)
	}

	var genresJSON string
	err = db.QueryRow(`SELECT genres FROM albums`).Scan(&genresJSON)
	if err != nil {
		t.Fatal(err)
	}
	var genres []string
	if err := json.Unmarshal([]byte(genresJSON), &genres); err != nil {
		t.Fatal(err)
	}
	if len(genres) != 1 || genres[0] != meta.Genre {
		t.Fatalf("album genres = %#v, want [%q]", genres, meta.Genre)
	}

	_, _ = db.Exec(`UPDATE tracks SET genre = NULL`)
	_, _ = db.Exec(`UPDATE albums SET genres = '[]'`)
	_, _, err = store.UpsertFromScan(context.Background(), meta)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(`SELECT genre FROM tracks`).Scan(&trackGenre)
	if err != nil {
		t.Fatal(err)
	}
	if trackGenre != meta.Genre {
		t.Fatalf("rescan track genre = %q, want %q", trackGenre, meta.Genre)
	}
}
