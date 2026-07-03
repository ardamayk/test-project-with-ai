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

func TestServiceListTracksSearchesAlbumAndGenre(t *testing.T) {
	svc, store, db, musicRoot := setupServiceDB(t)
	defer db.Close()

	now := time.Now()
	metas := []FileMetadata{
		{
			Path:        filepath.Join(musicRoot, "blue-monday.flac"),
			Format:      "flac",
			SizeBytes:   10,
			ModTime:     now,
			Title:       "Blue Monday",
			Artist:      "New Order",
			AlbumArtist: "New Order",
			Album:       "Low-Life",
			TrackNo:     1,
			DurationMs:  180_000,
			Genre:       "Synthpop",
		},
		{
			Path:        filepath.Join(musicRoot, "age-of-consent.flac"),
			Format:      "flac",
			SizeBytes:   10,
			ModTime:     now,
			Title:       "Age of Consent",
			Artist:      "New Order",
			AlbumArtist: "New Order",
			Album:       "Power Corruption and Lies",
			TrackNo:     1,
			DurationMs:  300_000,
			Genre:       "Rock",
		},
	}
	for _, meta := range metas {
		if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
			t.Fatal(err)
		}
	}

	byAlbum, err := svc.ListTracks(context.Background(), 10, 0, "Low-Life")
	if err != nil {
		t.Fatal(err)
	}
	if len(byAlbum.Items) != 1 || byAlbum.Items[0].Title != "Blue Monday" {
		t.Fatalf("album search items = %#v, want Blue Monday", byAlbum.Items)
	}

	byGenre, err := svc.ListTracks(context.Background(), 10, 0, "Synthpop")
	if err != nil {
		t.Fatal(err)
	}
	if len(byGenre.Items) != 1 || byGenre.Items[0].Title != "Blue Monday" {
		t.Fatalf("genre search items = %#v, want Blue Monday", byGenre.Items)
	}
}
