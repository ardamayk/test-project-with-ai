package library_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func TestRescanWithMissingGenrePreservesExistingTrackAndAlbumGenre(t *testing.T) {
	db := setupLibraryDB(t)

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
	if _, _, err := store.SeedLegacyTrack(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	meta.ModTime = meta.ModTime.Add(time.Minute)
	meta.Genre = ""
	if _, _, err := store.SeedLegacyTrack(context.Background(), meta); err != nil {
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
