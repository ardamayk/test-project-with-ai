package library

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkSeenPathsMarksMissingTracksWithoutLocking(t *testing.T) {
	db := openMemoryDB(t)
	store := NewStore(db)
	musicRoot := t.TempDir()
	visiblePath := filepath.Join(musicRoot, "visible.flac")
	missingPath := filepath.Join(musicRoot, "missing.flac")
	now := time.Now()

	for _, metadata := range []FileMetadata{
		{
			Path:        visiblePath,
			Format:      "flac",
			ModTime:     now,
			Title:       "Visible Track",
			Artist:      "Artist",
			AlbumArtist: "Artist",
			Album:       "Album",
		},
		{
			Path:        missingPath,
			Format:      "flac",
			ModTime:     now,
			Title:       "Missing Track",
			Artist:      "Artist",
			AlbumArtist: "Artist",
			Album:       "Album",
		},
	} {
		if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := store.MarkSeenPaths(context.Background(), map[string]struct{}{visiblePath: {}})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	tracks, err := store.ListTracks(context.Background(), 10, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks.Items) != 1 || tracks.Items[0].Title != "Visible Track" {
		t.Fatalf("tracks = %#v, want only Visible Track", tracks.Items)
	}
}

func TestRecomputeAllAlbumGenresWithoutLocking(t *testing.T) {
	db := openMemoryDB(t)
	store := NewStore(db)
	metadata := FileMetadata{
		Path:        filepath.Join(t.TempDir(), "track.flac"),
		Format:      "flac",
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Artist",
		AlbumArtist: "Artist",
		Album:       "Album",
		Genre:       "Rock",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}

	if err := store.RecomputeAllAlbumGenres(context.Background()); err != nil {
		t.Fatal(err)
	}

	albums, err := store.ListAlbums(context.Background(), 10, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums.Items) != 1 || len(albums.Items[0].Genres) != 1 || albums.Items[0].Genres[0] != "Rock" {
		t.Fatalf("albums = %#v, want one Rock album", albums.Items)
	}
}
