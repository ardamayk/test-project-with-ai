package managedimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestManagedTrackDeletionPreviewAndConfirmation(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
	siblingPath := filepath.Join(filepath.Dir(trackPath), "keep.flac")
	writeDeletionFile(t, siblingPath, "keep")
	seedDeletionReferences(t, database, "track-1")
	if _, err := database.Exec(`INSERT INTO managed_import_jobs (id, status, track_id) VALUES ('completed-import', 'committed', 'track-1')`); err != nil {
		t.Fatal(err)
	}

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview managedimport.TrackDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	if preview.TrackTitle != "Delete Me" || preview.ManagedFile.Path == trackPath || preview.ManagedFile.SizeBytes != 11 {
		t.Fatalf("preview Track/file = %+v", preview)
	}
	if len(preview.PlaylistReferences) != 1 || preview.PlaylistReferences[0].Name != "Keep This Playlist" {
		t.Fatalf("preview Playlist references = %+v", preview.PlaylistReferences)
	}
	if len(preview.QueueReferences) != 1 || preview.QueueReferences[0].ItemCount != 1 {
		t.Fatalf("preview Queue references = %+v", preview.QueueReferences)
	}
	assertTrackDeletionState(t, database, trackPath, "track-1", 1, 1)

	body, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", body, true)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	assertTrackDeletionState(t, database, trackPath, "track-1", 0, 0)
	if _, err := os.Stat(siblingPath); err != nil {
		t.Fatalf("sibling file changed: %v", err)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playlists WHERE id = 'playlist-1'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM albums WHERE id = 'album-1'`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE id = 'artist-1'`, 0)
	assertDeletionCount(t, database, `SELECT revision FROM playback_queue_state WHERE user_id = 'user-1'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_import_jobs WHERE id = 'completed-import'`, 0)
}

func TestManagedTrackDeletionRejectsStalePreviewWithoutMutation(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
	var preview managedimport.TrackDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	seedDeletionReferences(t, database, "track-1")
	body, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	response := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", body, true)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assertTrackDeletionState(t, database, trackPath, "track-1", 1, 1)
}

func TestManagedTrackDeletionRetainsAlbumAndArtistWithActiveTrack(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
	siblingPath := filepath.Join(filepath.Dir(trackPath), "track-2.flac")
	contents := "sibling-audio"
	writeDeletionFile(t, siblingPath, contents)
	contentSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
	if _, err := database.Exec(`
		INSERT INTO tracks (id, album_id, title, title_sort, artist_name, track_no, disc_no, duration_ms, format, size_bytes, file_path, identity_key)
		VALUES ('track-2', 'album-1', 'Keep Me', 'keep me', 'Artist', 2, 1, 1000, 'flac', ?, ?, 'track-2')`, len(contents), siblingPath); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO track_sources (id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes)
		VALUES ('source-track-2', 'track-2', 'managed', ?, ?, 'flac', ?)`, siblingPath, contentSHA256, len(contents)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO track_artists (track_id, artist_id, position) VALUES ('track-2', 'artist-1', 0)`); err != nil {
		t.Fatal(err)
	}

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
	var preview managedimport.TrackDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	body, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	response := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", body, true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	assertDeletionCount(t, database, `SELECT COUNT(*) FROM albums WHERE id = 'album-1'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE id = 'artist-1'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = 'track-2'`, 1)
	if _, err := os.Stat(siblingPath); err != nil {
		t.Fatalf("active sibling file changed: %v", err)
	}
}

func TestManagedTrackDeletionRemovesFinalAlbumArtwork(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
	artworkPath := filepath.Join(managedStoragePath, "library", "artist-1", "album-1", "cover.png")
	artwork := "album-artwork"
	writeDeletionFile(t, artworkPath, artwork)
	artworkHash := fmt.Sprintf("%x", sha256.Sum256([]byte(artwork)))
	if _, err := database.Exec(`
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type,
			width, height, encoded_size_bytes, file_path
		) VALUES ('artwork-1', 'album-1', 'track-1', ?, 'image/png', 1, 1, ?, ?)`, artworkHash, len(artwork), artworkPath); err != nil {
		t.Fatal(err)
	}

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
	var preview managedimport.TrackDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	body, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	response := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", body, true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(artworkPath); !os.IsNotExist(err) {
		t.Fatalf("orphaned final-Album Artwork stat error = %v", err)
	}
}

func TestManagedTrackDeletionRequiresExplicitApplicationConfirmation(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
	response := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", []byte(`{"confirmationToken":"anything"}`), false)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = 'track-1'`, 1)
	if _, err := os.Stat(trackPath); err != nil {
		t.Fatalf("managed file changed: %v", err)
	}
}

func TestManagedTrackDeletionPublishesAffectedQueueInvalidation(t *testing.T) {
	managedStoragePath := t.TempDir()
	database := testutil.OpenMigratedDB(t)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close deletion test database: %v", err)
		}
	})
	queueEvents := playback.NewQueueEventBroker()
	events, unsubscribe := queueEvents.Subscribe("user-1")
	t.Cleanup(unsubscribe)
	router := chi.NewRouter()
	managedimport.NewModule(database, config.Config{ManagedStoragePath: managedStoragePath}, library.NewMediaInspector(), queueEvents).RegisterRoutes(router)
	seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
	seedDeletionReferences(t, database, "track-1")

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
	var preview managedimport.TrackDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	body, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	response := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/track-1", body, true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	select {
	case event := <-events:
		if event.Revision != "1" || event.Sequence != "1" {
			t.Fatalf("Queue invalidation = %+v", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("affected Queue invalidation was not published")
	}
}

func TestManagedTrackDeletionRecoveryCompletesPreparedDeletion(t *testing.T) {
	for _, isFileRemoved := range []bool{false, true} {
		t.Run(fmt.Sprintf("file_removed_%t", isFileRemoved), func(t *testing.T) {
			managedStoragePath := t.TempDir()
			database, _ := newTrackDeletionRouter(t, managedStoragePath)
			trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
			var contentSHA256 string
			if err := database.QueryRow(`SELECT content_sha256 FROM track_sources WHERE track_id = 'track-1'`).Scan(&contentSHA256); err != nil {
				t.Fatal(err)
			}
			artworkPath := filepath.Join(managedStoragePath, "library", "artist-1", "album-1", "cover.png")
			artwork := "album-artwork"
			writeDeletionFile(t, artworkPath, artwork)
			artworkHash := fmt.Sprintf("%x", sha256.Sum256([]byte(artwork)))
			if _, err := database.Exec(`
				INSERT INTO album_artwork (
					id, album_id, source_track_id, content_sha256, media_type,
					width, height, encoded_size_bytes, file_path
				) VALUES ('artwork-1', 'album-1', 'track-1', ?, 'image/png', 1, 1, ?, ?)`, artworkHash, len(artwork), artworkPath); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`
				INSERT INTO permanent_track_deletions (
					track_id, file_path, content_sha256, artwork_file_path, artwork_content_sha256
				) VALUES ('track-1', ?, ?, ?, ?)`, trackPath, contentSHA256, artworkPath, artworkHash); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = 'track-1'`); err != nil {
				t.Fatal(err)
			}
			if isFileRemoved {
				if err := os.Remove(trackPath); err != nil {
					t.Fatal(err)
				}
			}

			module := managedimport.NewModule(database, config.Config{ManagedStoragePath: managedStoragePath}, library.NewMediaInspector())
			ctx, cancel := context.WithCancel(context.Background())
			if err := module.Start(ctx); err != nil {
				t.Fatalf("recover pending Permanent Track Deletion: %v", err)
			}
			cancel()

			assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = 'track-1'`, 0)
			assertDeletionCount(t, database, `SELECT COUNT(*) FROM permanent_track_deletions`, 0)
			if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
				t.Fatalf("managed file still exists: %v", err)
			}
			if _, err := os.Stat(artworkPath); !os.IsNotExist(err) {
				t.Fatalf("Managed Album Artwork still exists: %v", err)
			}
		})
	}
}

func TestManagedTrackDeletionRejectsUnsafeSources(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		unsafePath func(t *testing.T, root, trackPath string) string
	}{
		{name: "outside Managed Storage", unsafePath: func(t *testing.T, _, _ string) string {
			outsidePath := filepath.Join(t.TempDir(), "outside.flac")
			writeDeletionFile(t, outsidePath, "outside")
			return outsidePath
		}},
		{name: "symbolic link", unsafePath: func(t *testing.T, root, trackPath string) string {
			outsidePath := filepath.Join(t.TempDir(), "outside.flac")
			writeDeletionFile(t, outsidePath, "outside")
			if err := os.Remove(trackPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outsidePath, trackPath); err != nil {
				t.Fatal(err)
			}
			return trackPath
		}},
		{name: "broad directory", unsafePath: func(_ *testing.T, root, _ string) string { return root }},
		{name: "unresolved variable", unsafePath: func(_ *testing.T, root, _ string) string {
			return filepath.Join(root, "$TRACK_FILE")
		}},
		{name: "traversal", unsafePath: func(t *testing.T, root, _ string) string {
			outsidePath := filepath.Join(filepath.Dir(root), "outside.flac")
			writeDeletionFile(t, outsidePath, "outside")
			return filepath.Join(root, "library", "..", "..", "outside.flac")
		}},
		{name: "malformed metadata", unsafePath: func(_ *testing.T, root, _ string) string {
			return filepath.Join(root, "library", "bad\x00track.flac")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			managedStoragePath := t.TempDir()
			database, router := newTrackDeletionRouter(t, managedStoragePath)
			trackPath := seedManagedTrackForDeletion(t, database, managedStoragePath, "track-1", "album-1", "artist-1", "Delete Me")
			unsafePath := testCase.unsafePath(t, managedStoragePath, trackPath)
			if _, err := database.Exec(`UPDATE tracks SET file_path = ? WHERE id = 'track-1'`, unsafePath); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE track_sources SET file_path = ? WHERE track_id = 'track-1'`, unsafePath); err != nil {
				t.Fatal(err)
			}

			response := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/track-1/deletion", nil, false)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = 'track-1'`, 1)
		})
	}
}

func newTrackDeletionRouter(t *testing.T, managedStoragePath string) (*sql.DB, http.Handler) {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close deletion test database: %v", err)
		}
	})
	router := chi.NewRouter()
	managedimport.NewModule(database, config.Config{ManagedStoragePath: managedStoragePath}, library.NewMediaInspector()).RegisterRoutes(router)
	return database, router
}

func seedManagedTrackForDeletion(t *testing.T, database *sql.DB, root, trackID, albumID, artistID, title string) string {
	t.Helper()
	trackPath := filepath.Join(root, "library", artistID, albumID, trackID+".flac")
	contents := "audio-bytes"
	writeDeletionFile(t, trackPath, contents)
	contentSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO artists (id, name, name_sort, name_normalized) VALUES (?, 'Artist', 'artist', 'artist')`, []any{artistID}},
		{`INSERT INTO albums (id, artist_id, title, title_sort, identity_key) VALUES (?, ?, 'Album', 'album', ?)`, []any{albumID, artistID, albumID}},
		{`INSERT INTO tracks (id, album_id, title, title_sort, artist_name, track_no, disc_no, duration_ms, format, size_bytes, file_path, identity_key) VALUES (?, ?, ?, ?, 'Artist', 1, 1, 1000, 'flac', 11, ?, ?)`, []any{trackID, albumID, title, title, trackPath, trackID}},
		{`INSERT INTO track_sources (id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes) VALUES (?, ?, 'managed', ?, ?, 'flac', 11)`, []any{"source-" + trackID, trackID, trackPath, contentSHA256}},
		{`INSERT INTO track_artists (track_id, artist_id, position) VALUES (?, ?, 0)`, []any{trackID, artistID}},
		{`INSERT INTO album_artists (album_id, artist_id, position) VALUES (?, ?, 0)`, []any{albumID, artistID}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed Managed Track: %v", err)
		}
	}
	return trackPath
}

func seedDeletionReferences(t *testing.T, database *sql.DB, trackID string) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO playlists (id, user_id, name, is_default) VALUES ('playlist-1', 'user-1', 'Keep This Playlist', 0);
		INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES ('playlist-1', ?, 0);
		INSERT INTO playback_queue (id, user_id, position, track_id) VALUES ('queue-1', 'user-1', 0, ?)`, trackID, trackID); err != nil {
		t.Fatal(err)
	}
}

func performTrackDeletionRequest(t *testing.T, router http.Handler, method, path string, body []byte, confirm bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if confirm {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Permanent-Delete", "1")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeDeletionResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}

func writeDeletionFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTrackDeletionState(t *testing.T, database *sql.DB, trackPath, trackID string, trackCount, referenceCount int) {
	t.Helper()
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = ?`, trackCount, trackID)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playlist_tracks WHERE track_id = ?`, referenceCount, trackID)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playback_queue WHERE track_id = ?`, referenceCount, trackID)
	_, statErr := os.Stat(trackPath)
	if trackCount == 0 && !os.IsNotExist(statErr) {
		t.Fatalf("deleted file stat error = %v", statErr)
	}
	if trackCount == 1 && statErr != nil {
		t.Fatalf("retained file stat error = %v", statErr)
	}
}

func assertDeletionCount(t *testing.T, database *sql.DB, query string, expected int, args ...any) {
	t.Helper()
	var actual int
	if err := database.QueryRowContext(context.Background(), query, args...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q = %d, want %d", query, actual, expected)
	}
}
