package managedimport_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
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

const (
	WAV_IMPORT_SAMPLE_RATE_HZ = 48000
	WAV_IMPORT_CHANNELS       = 2
	WAV_IMPORT_BIT_DEPTH      = 24
	WAV_IMPORT_PCM_FRAMES     = 4800
)

// buildStrictWAV produces a fully tagged, artwork-bearing integer-PCM WAV that
// satisfies the Strict Import Profile.
func buildStrictWAV(t *testing.T) []byte {
	t.Helper()
	frames := []testutil.WAVID3Frame{
		testutil.WAVTextFrame("TIT2", "WAV Fixture"),
		testutil.WAVTextFrame("TPE1", "WAV Artist"),
		testutil.WAVTextFrame("TPE2", "WAV Album Artist"),
		testutil.WAVTextFrame("TALB", "WAV Strict Import"),
		testutil.WAVTextFrame("TRCK", "3/9"),
		testutil.WAVTextFrame("TPOS", "1/1"),
		testutil.WAVTextFrame("TCON", "Ambient"),
		testutil.WAVTextFrame("TDRC", "2026"),
		testutil.WAVAPICFrame(3, "image/png", "cover", testutil.WAVCoverPNG()),
	}
	return testutil.EncodeWAV(t, testutil.WAVFixture{
		AudioFormat:  1,
		ChannelCount: WAV_IMPORT_CHANNELS,
		SampleRateHz: WAV_IMPORT_SAMPLE_RATE_HZ,
		BitDepth:     WAV_IMPORT_BIT_DEPTH,
		PCMFrames:    WAV_IMPORT_PCM_FRAMES,
		ID3Frames:    frames,
	})
}

func TestManagedImportCommitsStrictWAVThroughLibraryPlayback(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{
		ManagedStoragePath: managedStoragePath,
		MusicPaths:         []string{t.TempDir()},
	}
	libraryModule := library.NewModule(database, configuration)
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)
	fixture := buildStrictWAV(t)

	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	testutil.DecodeJSON(t, jobResponse, &job)
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/wav",
		"X-Import-Filename": "strict-import.wav",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("WAV upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
		File     struct {
			Title            string   `json:"title"`
			Artists          []string `json:"artists"`
			AlbumArtists     []string `json:"albumArtists"`
			Album            string   `json:"album"`
			Genres           []string `json:"genres"`
			TrackNo          int      `json:"trackNo"`
			Format           string   `json:"format"`
			Container        string   `json:"container"`
			Codec            string   `json:"codec"`
			DurationMs       int      `json:"durationMs"`
			SampleRateHz     int      `json:"sampleRateHz"`
			ChannelCount     int      `json:"channelCount"`
			BitDepth         int      `json:"bitDepth"`
			BitrateKbps      int      `json:"bitrateKbps"`
			ArtworkMediaType string   `json:"artworkMediaType"`
		} `json:"file"`
	}
	testutil.DecodeJSON(t, uploadResponse, &preview)
	if preview.Revision != 2 || preview.File.Title != "WAV Fixture" || preview.File.Album != "WAV Strict Import" {
		t.Fatalf("WAV Import Preview = %+v", preview)
	}
	if strings.Join(preview.File.Artists, ",") != "WAV Artist" || strings.Join(preview.File.AlbumArtists, ",") != "WAV Album Artist" || strings.Join(preview.File.Genres, ",") != "Ambient" {
		t.Fatalf("WAV preview relationships = %+v", preview.File)
	}
	if preview.File.Format != "wav" || preview.File.Container != "wav" || preview.File.Codec != "pcm_s24le" || preview.File.ArtworkMediaType != "image/png" {
		t.Fatalf("WAV preview media = %+v", preview.File)
	}
	if preview.File.TrackNo != 3 || preview.File.DurationMs != 100 || preview.File.SampleRateHz != WAV_IMPORT_SAMPLE_RATE_HZ || preview.File.ChannelCount != WAV_IMPORT_CHANNELS || preview.File.BitDepth != WAV_IMPORT_BIT_DEPTH || preview.File.BitrateKbps <= 0 {
		t.Fatalf("WAV preview technical properties = %+v", preview.File)
	}

	confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", strings.NewReader(fmt.Sprintf(`{"revision":%d}`, preview.Revision)), map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("WAV confirm status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var result struct {
		TrackID string `json:"trackId"`
	}
	testutil.DecodeJSON(t, confirmResponse, &result)
	if result.TrackID == "" {
		t.Fatalf("WAV import result = %+v", result)
	}

	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != result.TrackID {
		t.Fatalf("WAV library tracks = %+v", tracks.Items)
	}
	track := tracks.Items[0]
	if track.Container != "wav" || track.Codec != "pcm_s24le" || track.DurationMs != 100 || track.SampleRateHz != WAV_IMPORT_SAMPLE_RATE_HZ || track.ChannelCount != WAV_IMPORT_CHANNELS || track.BitDepth != WAV_IMPORT_BIT_DEPTH || track.BitrateBps != 2_304_000 {
		t.Fatalf("persisted WAV track = %+v", track)
	}

	streamResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+result.TrackID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK {
		t.Fatalf("WAV stream status = %d, body = %s", streamResponse.Code, streamResponse.Body.String())
	}
	if !bytes.Equal(streamResponse.Body.Bytes(), fixture) {
		t.Fatal("streamed WAV bytes differ from uploaded file")
	}

	audioPath := findCanonicalAudio(t, managedStoragePath)
	if audioPath == "" || !strings.HasSuffix(audioPath, ".wav") {
		t.Fatalf("canonical WAV path = %q", audioPath)
	}
	stored, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read canonical WAV: %v", err)
	}
	if !bytes.Equal(stored, fixture) {
		t.Fatal("canonical WAV bytes differ from uploaded file")
	}
}

func findCanonicalAudio(t *testing.T, root string) string {
	t.Helper()
	var audioPath string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".wav", ".flac":
			audioPath = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Managed Storage: %v", err)
	}
	return audioPath
}

func TestManagedImportRejectsUntaggedWAVWithActionableError(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)

	fixture := testutil.EncodeWAV(t, testutil.WAVFixture{
		AudioFormat:  1,
		ChannelCount: WAV_IMPORT_CHANNELS,
		SampleRateHz: WAV_IMPORT_SAMPLE_RATE_HZ,
		BitDepth:     WAV_IMPORT_BIT_DEPTH,
		PCMFrames:    WAV_IMPORT_PCM_FRAMES,
		OmitID3:      true,
	})
	response := uploadWAVThroughRouter(t, router, fixture)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("untagged WAV status = %d, body = %s", response.Code, response.Body.String())
	}
	var failure struct {
		Code   string `json:"code"`
		Field  string `json:"field"`
		Reason string `json:"reason"`
	}
	testutil.DecodeJSON(t, response, &failure)
	if failure.Code != "invalid_metadata" || failure.Field != "metadata" {
		t.Fatalf("untagged WAV failure = %+v", failure)
	}
}

func uploadWAVThroughRouter(t *testing.T, router http.Handler, fixture []byte) *httptest.ResponseRecorder {
	t.Helper()
	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	testutil.DecodeJSON(t, jobResponse, &job)
	return testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/wav",
		"X-Import-Filename": "recording.wav",
	})
}
