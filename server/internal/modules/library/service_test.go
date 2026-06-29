package library

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	_ "modernc.org/sqlite"
)

func setupServiceDB(t *testing.T) (*Service, *Store, *sql.DB, string) {
	t.Helper()
	musicRoot := t.TempDir()
	db := openMemoryDB(t)
	_, err := db.Exec(`
		CREATE TABLE artists (id TEXT PRIMARY KEY, name TEXT, name_sort TEXT, created_at TEXT, updated_at TEXT);
		CREATE TABLE albums (id TEXT PRIMARY KEY, artist_id TEXT, title TEXT, title_sort TEXT, year INTEGER, genres TEXT NOT NULL DEFAULT '[]', cover_mime TEXT, cover_data BLOB, created_at TEXT, updated_at TEXT);
		CREATE TABLE tracks (id TEXT PRIMARY KEY, album_id TEXT, title TEXT, title_sort TEXT, artist_name TEXT, track_no INTEGER, duration_ms INTEGER, format TEXT, size_bytes INTEGER, file_path TEXT UNIQUE, file_mtime INTEGER, missing_at TEXT, genre TEXT, sample_rate_hz INTEGER, bit_depth INTEGER, created_at TEXT, updated_at TEXT);
		CREATE TABLE playback_queue (id TEXT PRIMARY KEY, user_id TEXT, position INTEGER, track_id TEXT, UNIQUE(user_id, position));
		CREATE TABLE scan_jobs (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'idle', started_at TEXT, finished_at TEXT, scanned INTEGER NOT NULL DEFAULT 0, added INTEGER NOT NULL DEFAULT 0, updated INTEGER NOT NULL DEFAULT 0, removed INTEGER NOT NULL DEFAULT 0, error_message TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	svc := NewService(store, config.Config{MusicPaths: []string{musicRoot}})
	return svc, store, db, musicRoot
}

func TestServiceMusicPathsConfigured(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	withPaths := NewService(NewStore(db), config.Config{MusicPaths: []string{"/music"}})
	if !withPaths.MusicPathsConfigured() {
		t.Fatal("expected music paths configured")
	}

	withoutPaths := NewService(NewStore(db), config.Config{})
	if withoutPaths.MusicPathsConfigured() {
		t.Fatal("expected no music paths")
	}
}

func TestServiceDeleteTrackRespectsMusicRoots(t *testing.T) {
	svc, store, db, musicRoot := setupServiceDB(t)
	trackPath := filepath.Join(musicRoot, "keep.flac")
	if err := os.WriteFile(trackPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Artist",
		AlbumArtist: "Artist",
		Album:       "Album",
		TrackNo:     1,
		DurationMs:  1000,
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}
	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}

	result, err := svc.DeleteTrack(context.Background(), trackID)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != 1 {
		t.Fatalf("deletedFiles = %d, want 1", result.DeletedFiles)
	}
	if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
		t.Fatal("track file should be removed")
	}
}
