package library_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	_ "modernc.org/sqlite"
)

func setupLibraryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE artists (id TEXT PRIMARY KEY, name TEXT, name_sort TEXT, created_at TEXT, updated_at TEXT);
		CREATE TABLE albums (id TEXT PRIMARY KEY, artist_id TEXT, title TEXT, title_sort TEXT, year INTEGER, genres TEXT NOT NULL DEFAULT '[]', cover_mime TEXT, cover_data BLOB, created_at TEXT, updated_at TEXT);
		CREATE TABLE tracks (id TEXT PRIMARY KEY, album_id TEXT, title TEXT, title_sort TEXT, artist_name TEXT, track_no INTEGER, duration_ms INTEGER, format TEXT, size_bytes INTEGER, file_path TEXT UNIQUE, file_mtime INTEGER, missing_at TEXT, genre TEXT, sample_rate_hz INTEGER, bit_depth INTEGER, created_at TEXT, updated_at TEXT);
		CREATE TABLE playback_queue (id TEXT PRIMARY KEY, user_id TEXT, position INTEGER, track_id TEXT, UNIQUE(user_id, position));
		CREATE TABLE scan_jobs (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'idle', started_at TEXT, finished_at TEXT, scanned INTEGER NOT NULL DEFAULT 0, added INTEGER NOT NULL DEFAULT 0, updated INTEGER NOT NULL DEFAULT 0, removed INTEGER NOT NULL DEFAULT 0, error_message TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAlbumArtistGroupsFeaturedTrackUnderAlbumArtist(t *testing.T) {
	db := setupLibraryDB(t)
	defer db.Close()

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        filepath.Join(t.TempDir(), "snow.flac"),
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Snow On The Beach (feat. Lana Del Rey)",
		Artist:      "Lana Del Rey",
		AlbumArtist: "Taylor Swift",
		Album:       "Midnights",
		TrackNo:     4,
		Year:        2022,
		DurationMs:  256000,
		Genre:       "Synthpop",
	}
	if err := os.WriteFile(meta.Path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var artistName, trackArtist string
	err := db.QueryRow(`
		SELECT ar.name, t.artist_name
		FROM albums al
		INNER JOIN artists ar ON ar.id = al.artist_id
		INNER JOIN tracks t ON t.album_id = al.id`,
	).Scan(&artistName, &trackArtist)
	if err != nil {
		t.Fatal(err)
	}
	if artistName != "Taylor Swift" {
		t.Fatalf("album artist = %q, want Taylor Swift", artistName)
	}
	if trackArtist != "Lana Del Rey" {
		t.Fatalf("track artist = %q, want Lana Del Rey", trackArtist)
	}

	var albumCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albumCount); err != nil {
		t.Fatal(err)
	}
	if albumCount != 1 {
		t.Fatalf("album count = %d, want 1", albumCount)
	}
}

func TestDeleteTrackRemovesDatabaseRowsAndFile(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)
	defer db.Close()

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Taylor Swift",
		AlbumArtist: "Taylor Swift",
		Album:       "Midnights",
		TrackNo:     1,
		Year:        2022,
		DurationMs:  1000,
		Genre:       "Pop",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteTrack(context.Background(), trackID, os.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
		t.Fatalf("track file still exists: %v", err)
	}

	var trackCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&trackCount); err != nil {
		t.Fatal(err)
	}
	if trackCount != 0 {
		t.Fatalf("track count = %d, want 0", trackCount)
	}
}

func TestDeleteAlbumRemovesTracksAndFiles(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)
	defer db.Close()

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Taylor Swift",
		AlbumArtist: "Taylor Swift",
		Album:       "Midnights",
		TrackNo:     1,
		Year:        2022,
		DurationMs:  1000,
		SampleRateHz: 44100,
		BitDepth:    16,
		Genre:       "Pop",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var albumID string
	if err := db.QueryRow(`SELECT album_id FROM tracks`).Scan(&albumID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteAlbum(context.Background(), albumID, os.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
		t.Fatalf("album track file still exists: %v", err)
	}

	var albumCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albumCount); err != nil {
		t.Fatal(err)
	}
	if albumCount != 0 {
		t.Fatalf("album count = %d, want 0", albumCount)
	}
}

func TestDeleteTrackRemovesQueueAndEmptyAlbum(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)
	defer db.Close()

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Only Track",
		Artist:      "Solo Artist",
		AlbumArtist: "Solo Artist",
		Album:       "Solo Album",
		TrackNo:     1,
		Year:        2024,
		DurationMs:  1000,
		Genre:       "Indie",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES (?, ?, ?, ?)`,
		"q1", "user", 0, trackID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteTrack(context.Background(), trackID, os.Remove); err != nil {
		t.Fatal(err)
	}

	var queueCount, albumCount, artistCount int
	for _, query := range []struct {
		sql  string
		dest *int
	}{
		{`SELECT COUNT(*) FROM playback_queue`, &queueCount},
		{`SELECT COUNT(*) FROM albums`, &albumCount},
		{`SELECT COUNT(*) FROM artists`, &artistCount},
	} {
		if err := db.QueryRow(query.sql).Scan(query.dest); err != nil {
			t.Fatal(err)
		}
	}
	if queueCount != 0 || albumCount != 0 || artistCount != 0 {
		t.Fatalf("queue=%d album=%d artist=%d, want all 0", queueCount, albumCount, artistCount)
	}
}
