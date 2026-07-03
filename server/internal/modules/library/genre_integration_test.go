package library_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestScanStoresTrackAndAlbumGenresFromFlac(t *testing.T) {
	musicDir := filepath.Join("..", "..", "..", "music")
	weekndFlac := filepath.Join(musicDir, "The Weeknd-2025-Hurry Up Tomorrow - 01 - Wake Me Up-24bit-88.2Khz.flac")
	if _, err := os.Stat(weekndFlac); err != nil {
		t.Skip("sample flac not present")
	}

	db := testutil.OpenMigratedDB(t)
	defer db.Close()

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

func TestRescanWithMissingGenrePreservesExistingTrackAndAlbumGenre(t *testing.T) {
	db := setupLibraryDB(t)
	defer db.Close()

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        filepath.Join(t.TempDir(), "track.flac"),
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Taylor Swift",
		AlbumArtist: "Taylor Swift",
		Album:       "The Life of a Showgirl",
		TrackNo:     1,
		Year:        2025,
		DurationMs:  1000,
		Genre:       "Pop",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	meta.ModTime = meta.ModTime.Add(time.Minute)
	meta.Genre = ""
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackGenre string
	if err := db.QueryRow(`SELECT genre FROM tracks`).Scan(&trackGenre); err != nil {
		t.Fatal(err)
	}
	if trackGenre != "Pop" {
		t.Fatalf("track genre = %q, want Pop", trackGenre)
	}

	var genresJSON string
	if err := db.QueryRow(`SELECT genres FROM albums`).Scan(&genresJSON); err != nil {
		t.Fatal(err)
	}
	var genres []string
	if err := json.Unmarshal([]byte(genresJSON), &genres); err != nil {
		t.Fatal(err)
	}
	if len(genres) != 1 || genres[0] != "Pop" {
		t.Fatalf("album genres = %#v, want [Pop]", genres)
	}
}
