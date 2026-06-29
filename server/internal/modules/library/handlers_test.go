package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func setupHandlerFixture(t *testing.T) (*Handlers, *sql.DB, string) {
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
	return NewHandlers(svc), db, musicRoot
}

func seedTrack(t *testing.T, db *sql.DB, store *Store, musicRoot string) (albumID, trackID string) {
	t.Helper()
	trackPath := filepath.Join(musicRoot, "song.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := FileMetadata{
		Path:         trackPath,
		Format:       "flac",
		SizeBytes:    10,
		ModTime:      time.Now(),
		Title:        "Song",
		Artist:       "Artist",
		AlbumArtist:  "Artist",
		Album:        "Album",
		TrackNo:      1,
		Year:         2024,
		DurationMs:   1000,
		Genre:        "Pop",
		SampleRateHz: 96000,
		BitDepth:     24,
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT album_id, id FROM tracks`).Scan(&albumID, &trackID); err != nil {
		t.Fatal(err)
	}
	return albumID, trackID
}

func TestPagination(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?limit=10&offset=5", nil)
	limit, offset := pagination(req)
	if limit != 10 || offset != 5 {
		t.Fatalf("pagination = (%d, %d), want (10, 5)", limit, offset)
	}

	bad := httptest.NewRequest(http.MethodGet, "/?limit=9999&offset=-1", nil)
	limit, offset = pagination(bad)
	if limit != 50 || offset != 0 {
		t.Fatalf("pagination clamp = (%d, %d), want (50, 0)", limit, offset)
	}
}

func TestHandlersGetAlbumNotFound(t *testing.T) {
	h, _, _ := setupHandlerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("albumId", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.GetAlbum(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlersGetAlbumReturnsGenresAndAudioFormat(t *testing.T) {
	h, db, musicRoot := setupHandlerFixture(t)
	store := NewStore(db)
	albumID, _ := seedTrack(t, db, store, musicRoot)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("albumId", albumID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.GetAlbum(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body AlbumDetail
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Genres) != 1 || body.Genres[0] != "Pop" {
		t.Fatalf("genres = %#v", body.Genres)
	}
	if len(body.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(body.Tracks))
	}
	if body.Tracks[0].BitDepth != 24 || body.Tracks[0].SampleRateHz != 96000 {
		t.Fatalf("audio format = %+v", body.Tracks[0])
	}
}

func TestHandlersDeleteAlbum(t *testing.T) {
	h, db, musicRoot := setupHandlerFixture(t)
	store := NewStore(db)
	albumID, _ := seedTrack(t, db, store, musicRoot)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("albumId", albumID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.DeleteAlbum(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result DeleteResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != 1 {
		t.Fatalf("deletedFiles = %d, want 1", result.DeletedFiles)
	}
}

func TestHandlersDeleteTrackNotFound(t *testing.T) {
	h, _, _ := setupHandlerFixture(t)
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trackId", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.DeleteTrack(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlersTriggerScanWithoutMusicPaths(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()
	_, err := db.Exec(`CREATE TABLE scan_jobs (id TEXT PRIMARY KEY, status TEXT, started_at TEXT, finished_at TEXT, scanned INTEGER, added INTEGER, updated INTEGER, removed INTEGER, error_message TEXT)`)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	svc := NewService(store, config.Config{})
	h := NewHandlers(svc)

	rec := httptest.NewRecorder()
	h.TriggerScan(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
