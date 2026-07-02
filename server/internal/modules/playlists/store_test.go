package playlists

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

type fakeTrackAccess struct {
	tracks map[string]library.Track
}

func (f fakeTrackAccess) GetTrack(_ context.Context, trackID string) (library.Track, error) {
	track, ok := f.tracks[trackID]
	if !ok {
		return library.Track{}, library.ErrNotFound
	}
	return track, nil
}

func (f fakeTrackAccess) GetTrackFilePath(_ context.Context, trackID string) (string, error) {
	track, ok := f.tracks[trackID]
	if !ok {
		return "", library.ErrNotFound
	}
	return track.FilePath, nil
}

func setupPlaylistStore(t *testing.T) (*Store, *library.Store, *sql.DB, string) {
	t.Helper()
	musicRoot := t.TempDir()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE artists (id TEXT PRIMARY KEY, name TEXT, name_sort TEXT, created_at TEXT, updated_at TEXT);
		CREATE TABLE albums (id TEXT PRIMARY KEY, artist_id TEXT, title TEXT, title_sort TEXT, year INTEGER, genres TEXT NOT NULL DEFAULT '[]', cover_mime TEXT, cover_data BLOB, created_at TEXT, updated_at TEXT);
		CREATE TABLE tracks (id TEXT PRIMARY KEY, album_id TEXT, title TEXT, title_sort TEXT, artist_name TEXT, track_no INTEGER, duration_ms INTEGER, format TEXT, size_bytes INTEGER, file_path TEXT UNIQUE, file_mtime INTEGER, missing_at TEXT, genre TEXT, sample_rate_hz INTEGER, bit_depth INTEGER, created_at TEXT, updated_at TEXT);
		CREATE TABLE playlists (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(user_id, name));
		CREATE TABLE playlist_tracks (playlist_id TEXT NOT NULL, track_id TEXT NOT NULL, position INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (playlist_id, track_id));
	`)
	if err != nil {
		t.Fatal(err)
	}
	libStore := library.NewStore(db)
	return NewStore(db, libStore), libStore, db, musicRoot
}

func setupPlaylistStoreWithTrackAccess(t *testing.T, tracks map[string]library.Track) *Store {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE playlists (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(user_id, name));
		CREATE TABLE playlist_tracks (playlist_id TEXT NOT NULL, track_id TEXT NOT NULL, position INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (playlist_id, track_id));
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db, fakeTrackAccess{tracks: tracks})
}

func seedPlaylistTrack(t *testing.T, db *sql.DB, libStore *library.Store, musicRoot string) string {
	t.Helper()
	trackPath := filepath.Join(musicRoot, "song.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Song",
		Artist:      "Artist",
		AlbumArtist: "Artist",
		Album:       "Album",
		TrackNo:     1,
		DurationMs:  1000,
	}
	if _, _, err := libStore.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}
	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	return trackID
}

func TestListPlaylistsCreatesDefaultFavorites(t *testing.T) {
	store, _, _, _ := setupPlaylistStore(t)

	playlists, err := store.ListPlaylists(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}

	if len(playlists.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(playlists.Items))
	}
	if playlists.Items[0].Name != DefaultFavoritesName || !playlists.Items[0].IsDefault {
		t.Fatalf("default playlist = %+v", playlists.Items[0])
	}
}

func TestAddAndRemoveTrackAreIdempotent(t *testing.T) {
	store, libStore, db, musicRoot := setupPlaylistStore(t)
	trackID := seedPlaylistTrack(t, db, libStore, musicRoot)

	favorites, err := store.GetDefaultFavorites(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.AddTrack(context.Background(), "user-1", favorites.ID, trackID); err != nil {
		t.Fatal(err)
	}
	detail, err := store.AddTrack(context.Background(), "user-1", favorites.ID, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tracks) != 1 || detail.Tracks[0].ID != trackID {
		t.Fatalf("tracks after duplicate add = %+v", detail.Tracks)
	}

	detail, err = store.RemoveTrack(context.Background(), "user-1", favorites.ID, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tracks) != 0 {
		t.Fatalf("tracks after remove = %d, want 0", len(detail.Tracks))
	}
	detail, err = store.RemoveTrack(context.Background(), "user-1", favorites.ID, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tracks) != 0 {
		t.Fatalf("tracks after duplicate remove = %d, want 0", len(detail.Tracks))
	}
}

func TestCreatePlaylistAndListWithFavoritesPinned(t *testing.T) {
	store, _, _, _ := setupPlaylistStore(t)

	created, err := store.CreatePlaylist(context.Background(), "user-1", "Road")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Road" || created.IsDefault {
		t.Fatalf("created = %+v", created)
	}

	playlists, err := store.ListPlaylists(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(playlists.Items))
	}
	if playlists.Items[0].Name != DefaultFavoritesName || playlists.Items[1].Name != "Road" {
		t.Fatalf("order = %+v", playlists.Items)
	}
}

func TestStoreUsesTrackAccessInterface(t *testing.T) {
	trackID := "track-1"
	store := setupPlaylistStoreWithTrackAccess(t, map[string]library.Track{
		trackID: {ID: trackID, Title: "Song"},
	})

	favorites, err := store.GetDefaultFavorites(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := store.AddTrack(context.Background(), "user-1", favorites.ID, trackID)
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(detail.Tracks))
	}
	if detail.Tracks[0].Title != "Song" {
		t.Fatalf("track = %+v", detail.Tracks[0])
	}
}
