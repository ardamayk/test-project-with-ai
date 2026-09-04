package managedimport_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
)

func TestManagedAlbumDeletionPreviewsEveryTrackAndDeletesThemAll(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	first := managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-1", 1, "First")
	second := managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-2", 2, "Second")
	seedDeletionReferences(t, database, "track-1")
	if _, err := database.Exec(`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES ('queue-2', 'user-1', 1, 'track-2')`); err != nil {
		t.Fatal(err)
	}

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/albums/album-1/deletion", nil, false)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview managedimport.AlbumDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	if preview.AlbumID != "album-1" || preview.AlbumTitle != "Album" || preview.TrackCount != 2 {
		t.Fatalf("preview album = %+v", preview)
	}
	if preview.TotalSizeBytes != int64(len("track-1-bytes")+len("track-2-bytes")) {
		t.Fatalf("preview total size = %d", preview.TotalSizeBytes)
	}
	if len(preview.Tracks) != 2 || preview.Tracks[0].TrackTitle != "First" || preview.Tracks[1].TrackTitle != "Second" {
		t.Fatalf("preview tracks = %+v", preview.Tracks)
	}
	if len(preview.PlaylistReferences) != 1 || preview.PlaylistReferences[0].Name != "Keep This Playlist" {
		t.Fatalf("preview playlists = %+v", preview.PlaylistReferences)
	}
	if len(preview.QueueReferences) != 1 || preview.QueueReferences[0].ItemCount != 2 {
		t.Fatalf("preview queues = %+v", preview.QueueReferences)
	}
	if preview.ConfirmationToken == "" {
		t.Fatal("preview has no confirmation token")
	}

	body, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/albums/album-1", body, true)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	var result managedimport.AlbumDeletionResult
	decodeDeletionResponse(t, deleteResponse, &result)
	if len(result.Deleted) != 2 || result.StoppedAt != nil || result.DeletedFiles != 2 {
		t.Fatalf("delete result = %+v", result)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed file %q still present: %v", path, err)
		}
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM albums WHERE id = 'album-1'`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE id = 'artist-1'`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playback_queue`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playlists WHERE id = 'playlist-1'`, 1)
	assertDeletionCount(t, database, `SELECT revision FROM playback_queue_state WHERE user_id = 'user-1'`, 2)
}

func TestManagedAlbumDeletionRejectsStalePreviewWithoutMutation(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	trackPath := managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-1", 1, "First")

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/albums/album-1/deletion", nil, false)
	var preview managedimport.AlbumDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	// A sibling added after the preview changes what "delete the album" means.
	managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-2", 2, "Second")

	body, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/albums/album-1", body, true)
	if deleteResponse.Code != http.StatusConflict {
		t.Fatalf("stale delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(trackPath); err != nil {
		t.Fatalf("managed file was touched by a rejected deletion: %v", err)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks`, 2)
}

func TestManagedAlbumDeletionRefusesWhenAFileChangedAfterPreview(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	first := managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-1", 1, "First")
	second := managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-2", 2, "Second")

	previewResponse := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/albums/album-1/deletion", nil, false)
	var preview managedimport.AlbumDeletionPreview
	decodeDeletionResponse(t, previewResponse, &preview)
	if err := os.WriteFile(second, []byte("tampered-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: preview.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/albums/album-1", body, true)
	if deleteResponse.Code != http.StatusConflict {
		t.Fatalf("tampered delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	// Nothing is deleted when the album no longer matches its preview.
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first file was touched by a refused deletion: %v", err)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE missing_at IS NULL`, 2)
}

func TestManagedAlbumDeletionRequiresExplicitConfirmationAndKnownAlbum(t *testing.T) {
	managedStoragePath := t.TempDir()
	database, router := newTrackDeletionRouter(t, managedStoragePath)
	managedimport.SeedManagedAlbumTrackForTest(t, database, managedStoragePath, "album-1", "artist-1", "track-1", 1, "First")

	missing := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/albums/missing/deletion", nil, false)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing album preview status = %d", missing.Code)
	}
	body, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: "x"})
	unconfirmed := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/albums/album-1", body, false)
	if unconfirmed.Code != http.StatusForbidden {
		t.Fatalf("unconfirmed delete status = %d", unconfirmed.Code)
	}
	emptyToken, _ := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: ""})
	invalid := performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/albums/album-1", emptyToken, true)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("empty token delete status = %d", invalid.Code)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks`, 1)
}
