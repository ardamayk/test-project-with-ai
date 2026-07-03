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

func TestHandlersGetAlbumDeduplicatesRepeatedTracks(t *testing.T) {
	h, db, musicRoot := setupHandlerFixture(t)
	store := NewStore(db)
	albumID, _ := seedTrack(t, db, store, musicRoot)

	duplicatePath := filepath.Join(musicRoot, "song-copy.flac")
	if err := os.WriteFile(duplicatePath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := FileMetadata{
		Path:         duplicatePath,
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
	if len(body.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(body.Tracks))
	}
	if body.TrackCount != 1 {
		t.Fatalf("trackCount = %d, want 1", body.TrackCount)
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

	store := NewStore(db)
	svc := NewService(store, config.Config{})
	h := NewHandlers(svc)

	rec := httptest.NewRecorder()
	h.TriggerScan(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
