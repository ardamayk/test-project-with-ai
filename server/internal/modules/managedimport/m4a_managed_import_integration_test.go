package managedimport_test

import (
	"bytes"
	"context"
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

func TestManagedImportCommitsStrictM4AThroughLibraryPlayback(t *testing.T) {
	tests := []struct {
		name             string
		fixtureName      string
		codec            string
		expectedTitle    string
		expectedTrack    int
		expectedBitDepth int
	}{
		{name: "AAC", fixtureName: "strict-import-aac.m4a", codec: "aac", expectedTitle: "M4A AAC Fixture", expectedTrack: 3},
		{name: "ALAC", fixtureName: "strict-import-alac.m4a", codec: "alac", expectedTitle: "M4A ALAC Fixture", expectedTrack: 4, expectedBitDepth: 16},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, managedStoragePath := newM4AManagedImportRouter(t)
			fixture := readM4AFixture(t, test.fixtureName)
			jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

			uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
				"Content-Type":      "audio/flac",
				"X-Import-Filename": "misleading.flac",
			})
			if uploadResponse.Code != http.StatusOK {
				t.Fatalf("upload M4A status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
			}
			var preview struct {
				Revision int `json:"revision"`
				File     struct {
					Title        string   `json:"title"`
					Artists      []string `json:"artists"`
					AlbumArtists []string `json:"albumArtists"`
					Genres       []string `json:"genres"`
					TrackNo      int      `json:"trackNo"`
					TrackTotal   int      `json:"trackTotal"`
					DiscNo       int      `json:"discNo"`
					DiscTotal    int      `json:"discTotal"`
					Format       string   `json:"format"`
					Container    string   `json:"container"`
					Codec        string   `json:"codec"`
					BitDepth     int      `json:"bitDepth"`
					BitrateKbps  int      `json:"bitrateKbps"`
				} `json:"file"`
			}
			testutil.DecodeJSON(t, uploadResponse, &preview)
			if preview.File.Title != test.expectedTitle || preview.File.TrackNo != test.expectedTrack || preview.File.TrackTotal != 9 || preview.File.DiscNo != 1 || preview.File.DiscTotal != 1 {
				t.Fatalf("M4A metadata = %+v", preview.File)
			}
			if strings.Join(preview.File.Artists, ",") != "Test Artist" || strings.Join(preview.File.AlbumArtists, ",") != "Test Album Artist" || strings.Join(preview.File.Genres, ",") != "Electronic,Ambient" {
				t.Fatalf("M4A relationships = %+v", preview.File)
			}
			if preview.File.Format != "m4a" || preview.File.Container != "m4a" || preview.File.Codec != test.codec || preview.File.BitDepth != test.expectedBitDepth || preview.File.BitrateKbps <= 0 {
				t.Fatalf("M4A technical properties = %+v", preview.File)
			}

			confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{"Content-Type": "application/json"})
			if confirmResponse.Code != http.StatusOK {
				t.Fatalf("confirm M4A status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
			}
			var result struct {
				TrackID string `json:"trackId"`
			}
			testutil.DecodeJSON(t, confirmResponse, &result)
			streamResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+result.TrackID+"/stream", nil, nil)
			if streamResponse.Code != http.StatusOK || !bytes.Equal(streamResponse.Body.Bytes(), fixture) {
				t.Fatal("committed M4A bytes differ from uploaded bytes")
			}
			assertCanonicalM4A(t, managedStoragePath, fixture)
		})
	}
}

func TestManagedImportRejectsUnsupportedM4ACodec(t *testing.T) {
	router, _ := newM4AManagedImportRouter(t)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	response := uploadM4AFixture(t, router, jobID, readM4AFixture(t, "unsupported-opus.m4a"))
	responseBody := response.Body.String()

	testutil.AssertErrorCode(t, response, http.StatusUnprocessableEntity, "unsupported_format")
	if !strings.Contains(responseBody, `"field":"codec"`) {
		t.Fatalf("unsupported codec response = %s", responseBody)
	}
}

func TestManagedImportRejectsMalformedM4AContainer(t *testing.T) {
	router, _ := newM4AManagedImportRouter(t)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	fixture := readM4AFixture(t, "strict-import-aac.m4a")
	response := uploadM4AFixture(t, router, jobID, fixture[:32])
	responseBody := response.Body.String()

	testutil.AssertErrorCode(t, response, http.StatusUnprocessableEntity, "unsupported_format")
	if !strings.Contains(responseBody, `"field":"container"`) {
		t.Fatalf("malformed container response = %s", responseBody)
	}
}

func TestManagedImportRejectsM4AThatFailsFullDecode(t *testing.T) {
	router, _ := newM4AManagedImportRouter(t)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	fixture := readM4AFixture(t, "strict-import-aac.m4a")
	response := uploadM4AFixture(t, router, jobID, fixture[:len(fixture)-8])

	testutil.AssertErrorCode(t, response, http.StatusUnprocessableEntity, "audio_decode_failed")
}

func TestManagedImportPersistsM4AReplayGain(t *testing.T) {
	inspector := replayGainInspector{MediaInspector: library.NewMediaInspector()}
	router, _ := newM4AManagedImportRouterWithInspector(t, inspector)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	uploadResponse := uploadM4AFixture(t, router, jobID, readM4AFixture(t, "strict-import-aac.m4a"))
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload ReplayGain M4A status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm ReplayGain M4A status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}

	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ReplayGain.TrackGainDB == nil || *tracks.Items[0].ReplayGain.TrackGainDB != -7.25 || tracks.Items[0].ReplayGain.TrackPeak == nil || *tracks.Items[0].ReplayGain.TrackPeak != 0.987654 {
		t.Fatalf("persisted M4A ReplayGain = %+v", tracks.Items)
	}
}

type replayGainInspector struct {
	library.MediaInspector
}

func (inspector replayGainInspector) Inspect(ctx context.Context, path string, reportProgress library.InspectionProgressReporter) (library.MediaInspection, error) {
	inspection, err := inspector.MediaInspector.Inspect(ctx, path, reportProgress)
	if err != nil {
		return library.MediaInspection{}, err
	}
	trackGainDB := -7.25
	trackPeak := 0.987654
	inspection.Metadata.ReplayGain = library.ReplayGainMetadata{TrackGainDB: &trackGainDB, TrackPeak: &trackPeak}
	return inspection, nil
}

func newM4AManagedImportRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	return newM4AManagedImportRouterWithInspector(t, library.NewMediaInspector())
}

func newM4AManagedImportRouterWithInspector(t *testing.T, inspector library.MediaInspector) (http.Handler, string) {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{ManagedStoragePath: managedStoragePath}
	libraryModule := library.NewModule(database, configuration)
	router := chi.NewRouter()
	managedimport.NewModule(database, configuration, inspector).RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playback.NewModule(database, libraryModule.TrackAccess()).RegisterRoutes(router)
	return router, managedStoragePath
}

func uploadM4AFixture(t *testing.T, router http.Handler, jobID string, fixture []byte) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/mp4",
		"X-Import-Filename": "fixture.m4a",
	})
}

func readM4AFixture(t *testing.T, name string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "library", "testdata", name))
	if err != nil {
		t.Fatalf("read M4A fixture: %v", err)
	}
	return fixture
}

func assertCanonicalM4A(t *testing.T, managedStoragePath string, fixture []byte) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(managedStoragePath, "library", "*", "*", "*.m4a"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("canonical M4A paths = %v, error = %v", paths, err)
	}
	committed, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read canonical M4A: %v", err)
	}
	if !bytes.Equal(committed, fixture) {
		t.Fatal("canonical M4A bytes differ from uploaded bytes")
	}
}
