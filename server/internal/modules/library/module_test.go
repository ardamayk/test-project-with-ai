package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/go-chi/chi/v5"
)

func TestModuleStartScansConfiguredMusicPaths(t *testing.T) {
	musicRoot := t.TempDir()
	trackPath := filepath.Join(musicRoot, "Artist - Album - Startup Track.mp3")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	module := NewModule(openMemoryDB(t), config.Config{MusicPaths: []string{musicRoot}})
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForCompletedScan(t, router)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list tracks status = %d, want %d", response.Code, http.StatusOK)
	}

	var tracks TrackList
	if err := json.Unmarshal(response.Body.Bytes(), &tracks); err != nil {
		t.Fatal(err)
	}
	if len(tracks.Items) != 1 || tracks.Items[0].Title != "Artist - Album - Startup Track" {
		t.Fatalf("tracks = %#v, want indexed startup track", tracks.Items)
	}
}

func TestModuleStartRecoversInterruptedScan(t *testing.T) {
	musicRoot := t.TempDir()
	trackPath := filepath.Join(musicRoot, "Recovered Startup Track.mp3")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openMemoryDB(t)
	if _, err := NewStore(db).BeginScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	module := NewModule(db, config.Config{MusicPaths: []string{musicRoot}})
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForCompletedScan(t, router)
}

func waitForCompletedScan(t *testing.T, handler http.Handler) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/library/scan/status", nil)
		handler.ServeHTTP(response, request)

		var status ScanStatus
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Status == "completed" {
			return
		}
		if status.Status == "failed" {
			t.Fatalf("startup scan failed: %s", status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("startup scan did not complete")
}
