package managedimport_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestManagedImportCommitsStrictMP3ThroughLibraryPlayback(t *testing.T) {
	router, database, managedStoragePath := newMP3ManagedImportRouter(t)
	fixture := testutil.StrictMP3Fixture()
	jobID, revision := uploadStrictMP3(t, router, fixture)
	trackID := confirmStrictMP3(t, router, jobID, revision)
	assertCommittedMP3(t, router, trackID, fixture)
	assertCanonicalMP3(t, database, trackID, fixture, managedStoragePath)
}

func newMP3ManagedImportRouter(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{ManagedStoragePath: managedStoragePath, MusicPaths: []string{t.TempDir()}}
	libraryModule := library.NewModule(database, configuration)
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)
	return router, database, managedStoragePath
}

func uploadStrictMP3(t *testing.T, router http.Handler, fixture []byte) (string, int) {
	t.Helper()
	jobID := createManagedImportJob(t, router)
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "application/octet-stream",
		"X-Import-Filename": "misleading.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload strict MP3 status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if strings.Contains(uploadResponse.Body.String(), `"bitDepth"`) {
		t.Fatalf("MP3 Import Preview exposes inapplicable bit depth: %s", uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
		File     struct {
			Format       string `json:"format"`
			Container    string `json:"container"`
			Codec        string `json:"codec"`
			BitDepth     int    `json:"bitDepth"`
			BitrateKbps  int    `json:"bitrateKbps"`
			ChannelCount int    `json:"channelCount"`
		} `json:"file"`
	}
	testutil.DecodeJSON(t, uploadResponse, &preview)
	if preview.File.Format != "mp3" || preview.File.Container != "mp3" || preview.File.Codec != "mp3" || preview.File.BitDepth != 0 || preview.File.BitrateKbps != 52 || preview.File.ChannelCount != 1 {
		t.Fatalf("MP3 Import Preview = %+v", preview.File)
	}
	return jobID, preview.Revision
}

func confirmStrictMP3(t *testing.T, router http.Handler, jobID string, revision int) string {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"revision":%d}`, revision))
	confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", body, map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm strict MP3 status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var result struct {
		TrackID string `json:"trackId"`
	}
	testutil.DecodeJSON(t, confirmResponse, &result)
	return result.TrackID
}

func assertCommittedMP3(t *testing.T, router http.Handler, trackID string, fixture []byte) {
	t.Helper()
	tracksResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/library/tracks", nil, nil)
	var tracks library.TrackList
	testutil.DecodeJSON(t, tracksResponse, &tracks)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != trackID {
		t.Fatalf("committed MP3 Tracks = %+v", tracks.Items)
	}
	track := tracks.Items[0]
	if track.Format != "mp3" || track.Container != "mp3" || track.Codec != "mp3" || track.BitDepth != 0 || track.BitrateKbps != 52 {
		t.Fatalf("persisted MP3 technical properties = %+v", track)
	}
	assertMP3ReplayGain(t, track.ReplayGain)
	streamResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+trackID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK || !bytes.Equal(streamResponse.Body.Bytes(), fixture) {
		t.Fatalf("streamed MP3 status/size = %d/%d", streamResponse.Code, streamResponse.Body.Len())
	}
}

func assertMP3ReplayGain(t *testing.T, replayGain library.ReplayGainMetadata) {
	t.Helper()
	if replayGain.TrackGainDB == nil || *replayGain.TrackGainDB != -7.25 || replayGain.TrackPeak == nil || *replayGain.TrackPeak != 0.9123 || replayGain.AlbumGainDB == nil || *replayGain.AlbumGainDB != -6.75 || replayGain.AlbumPeak == nil || *replayGain.AlbumPeak != 0.9789 {
		t.Fatalf("persisted MP3 ReplayGain = %+v", replayGain)
	}
}

func assertCanonicalMP3(t *testing.T, database *sql.DB, trackID string, fixture []byte, managedStoragePath string) {
	t.Helper()
	var canonicalPath, contentSHA256 string
	if err := database.QueryRow(`SELECT file_path, content_sha256 FROM track_sources WHERE track_id = ?`, trackID).Scan(&canonicalPath, &contentSHA256); err != nil {
		t.Fatalf("read canonical MP3 source: %v", err)
	}
	canonicalBytes, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read canonical MP3: %v", err)
	}
	if filepath.Ext(canonicalPath) != ".mp3" || !bytes.Equal(canonicalBytes, fixture) || contentSHA256 == "" {
		t.Fatalf("canonical MP3 path/hash/bytes = %q/%q/%t", canonicalPath, contentSHA256, bytes.Equal(canonicalBytes, fixture))
	}
	relativePath, err := filepath.Rel(managedStoragePath, canonicalPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		t.Fatalf("canonical MP3 escaped Managed Storage: %q", canonicalPath)
	}
}
