package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/go-chi/chi/v5"
)

func newTestLibraryRouter(t *testing.T, musicRoot string) http.Handler {
	t.Helper()
	module := NewModule(openMemoryDB(t), config.Config{MusicPaths: []string{musicRoot}})
	router := chi.NewRouter()
	module.RegisterRoutes(router)
	return router
}

func listLibraryTracks(t *testing.T, handler http.Handler) TrackList {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list tracks status = %d, want %d", response.Code, http.StatusOK)
	}
	var tracks TrackList
	if err := json.Unmarshal(response.Body.Bytes(), &tracks); err != nil {
		t.Fatal(err)
	}
	return tracks
}

func TestModuleNeverIndexesFilesFromMusicPaths(t *testing.T) {
	musicRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(musicRoot, "Artist - Album - Startup Track.mp3"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := newTestLibraryRouter(t, musicRoot)
	if tracks := listLibraryTracks(t, router); len(tracks.Items) != 0 || tracks.Total != 0 {
		t.Fatalf("tracks = %#v, want no Tracks discovered from MUSIC_PATHS at startup", tracks.Items)
	}

	// Copying a new file into the former server music directory must not add a Track,
	// even after a client invokes the deprecated scan trigger.
	if err := os.WriteFile(filepath.Join(musicRoot, "Copied Later.flac"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	trigger := httptest.NewRecorder()
	router.ServeHTTP(trigger, httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", nil))
	if trigger.Code != http.StatusGone {
		t.Fatalf("deprecated scan trigger status = %d, want %d", trigger.Code, http.StatusGone)
	}
	if tracks := listLibraryTracks(t, router); len(tracks.Items) != 0 {
		t.Fatalf("tracks = %#v, want no Tracks after copying a file into the music directory", tracks.Items)
	}
}

func TestDeprecatedScanRoutesStayCompatible(t *testing.T) {
	router := newTestLibraryRouter(t, t.TempDir())

	trigger := httptest.NewRecorder()
	router.ServeHTTP(trigger, httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", nil))
	if trigger.Code != http.StatusGone {
		t.Fatalf("scan trigger status = %d, want %d", trigger.Code, http.StatusGone)
	}
	if trigger.Header().Get("Deprecation") != "true" {
		t.Fatal("scan trigger response should carry the Deprecation header")
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(trigger.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != LEGACY_SCAN_RETIRED_CODE {
		t.Fatalf("scan trigger code = %q, want %q", problem.Code, LEGACY_SCAN_RETIRED_CODE)
	}

	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/library/scan/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("scan status status = %d, want %d", status.Code, http.StatusOK)
	}
	if status.Header().Get("Deprecation") != "true" {
		t.Fatal("scan status response should carry the Deprecation header")
	}
	var scanStatus ScanStatus
	if err := json.Unmarshal(status.Body.Bytes(), &scanStatus); err != nil {
		t.Fatal(err)
	}
	if scanStatus.Status != "idle" || scanStatus.Scanned != 0 || scanStatus.Added != 0 {
		t.Fatalf("scan status = %#v, want permanently idle", scanStatus)
	}
}
