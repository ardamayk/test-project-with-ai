package playback

import (
	"bytes"
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
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func setupPlaybackHandlers(t *testing.T) (*Handlers, *Store, *library.Store, *sql.DB, string) {
	t.Helper()
	musicRoot := t.TempDir()
	db := testutil.OpenMigratedDB(t)

	libStore := library.NewStore(db)
	libSvc := library.NewService(libStore, config.Config{MusicPaths: []string{musicRoot}})
	pbStore := NewStore(db, libStore)
	return NewHandlers(pbStore, libSvc), pbStore, libStore, db, musicRoot
}

func seedPlaybackTrack(t *testing.T, db *sql.DB, libStore *library.Store, musicRoot string) string {
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

func TestHandlersGetQueueEmpty(t *testing.T) {
	h, _, _, _, _ := setupPlaybackHandlers(t)
	rec := httptest.NewRecorder()
	h.GetQueue(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body Queue
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(body.Items))
	}
}

func TestHandlersReplaceQueue(t *testing.T) {
	h, _, libStore, db, musicRoot := setupPlaybackHandlers(t)
	trackID := seedPlaybackTrack(t, db, libStore, musicRoot)

	body, _ := json.Marshal(map[string][]string{"trackIds": {trackID}})
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ReplaceQueue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var queue Queue
	if err := json.NewDecoder(rec.Body).Decode(&queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Items) != 1 || queue.Items[0].TrackID != trackID {
		t.Fatalf("queue = %+v", queue.Items)
	}
}

func TestHandlersAppendItemRequiresTrackID(t *testing.T) {
	h, _, _, _, _ := setupPlaybackHandlers(t)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h.AppendItem(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlersStreamTrackNotFound(t *testing.T) {
	h, _, _, _, _ := setupPlaybackHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trackId", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.StreamTrack(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
