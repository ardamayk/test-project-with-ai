package managedimport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestManagedImportCommitsOneStrictFLACThroughLibraryPlayback(t *testing.T) {
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
	fixture := readStrictFLACFixture(t)

	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	if jobResponse.Code != http.StatusCreated {
		t.Fatalf("create Import Job status = %d, body = %s", jobResponse.Code, jobResponse.Body.String())
	}
	var job struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Revision int    `json:"revision"`
	}
	decodeJSON(t, jobResponse, &job)
	if job.ID == "" || job.Status != "uploading" || job.Revision != 1 {
		t.Fatalf("created Import Job = %+v", job)
	}

	uploadResponse := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		JobID    string `json:"jobId"`
		Status   string `json:"status"`
		Revision int    `json:"revision"`
		File     struct {
			Title            string   `json:"title"`
			Artists          []string `json:"artists"`
			AlbumArtists     []string `json:"albumArtists"`
			Album            string   `json:"album"`
			Genres           []string `json:"genres"`
			TrackNo          int      `json:"trackNo"`
			DiscNo           int      `json:"discNo"`
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
	decodeJSON(t, uploadResponse, &preview)
	if preview.JobID != job.ID || preview.Status != "awaiting_confirmation" || preview.Revision != 2 {
		t.Fatalf("Import Preview = %+v", preview)
	}
	if preview.File.Title != "Inspection Fixture" || preview.File.Album != "Strict Import Tests" || preview.File.TrackNo != 3 || preview.File.DiscNo != 1 {
		t.Fatalf("Import Preview file = %+v", preview.File)
	}
	if strings.Join(preview.File.Artists, ",") != "Test Artist" || strings.Join(preview.File.AlbumArtists, ",") != "Test Album Artist" || strings.Join(preview.File.Genres, ",") != "Electronic" {
		t.Fatalf("Import Preview relationships = %+v", preview.File)
	}
	if preview.File.Format != "flac" || preview.File.Container != "flac" || preview.File.Codec != "flac" || preview.File.ArtworkMediaType != "image/png" {
		t.Fatalf("Import Preview media = %+v", preview.File)
	}
	if preview.File.DurationMs <= 0 || preview.File.SampleRateHz <= 0 || preview.File.ChannelCount <= 0 || preview.File.BitDepth <= 0 || preview.File.BitrateKbps <= 0 {
		t.Fatalf("Import Preview technical audio properties = %+v", preview.File)
	}

	tracksBeforeConfirm := listTracks(t, router)
	if len(tracksBeforeConfirm.Items) != 0 {
		t.Fatalf("Tracks before confirmation = %+v", tracksBeforeConfirm.Items)
	}
	staleResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", strings.NewReader(`{"revision":1}`), map[string]string{"Content-Type": "application/json"})
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale confirmation status = %d, body = %s", staleResponse.Code, staleResponse.Body.String())
	}

	confirmationBody := strings.NewReader(`{"revision":2}`)
	confirmResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", confirmationBody, map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var result struct {
		JobID    string `json:"jobId"`
		Status   string `json:"status"`
		Revision int    `json:"revision"`
		TrackID  string `json:"trackId"`
	}
	decodeJSON(t, confirmResponse, &result)
	if result.JobID != job.ID || result.Status != "committed" || result.Revision != 3 || result.TrackID == "" {
		t.Fatalf("Managed Import result = %+v", result)
	}

	tracksAfterConfirm := listTracks(t, router)
	if len(tracksAfterConfirm.Items) != 1 || tracksAfterConfirm.Items[0].ID != result.TrackID || tracksAfterConfirm.Items[0].Title != "Inspection Fixture" {
		t.Fatalf("Tracks after confirmation = %+v", tracksAfterConfirm.Items)
	}
	committedTrack := tracksAfterConfirm.Items[0]
	if committedTrack.AlbumTitle != "Strict Import Tests" || committedTrack.DiscNo != 1 || len(committedTrack.Artists) != 1 || committedTrack.Artists[0].Name != "Test Artist" || len(committedTrack.Genres) != 1 || committedTrack.Genres[0].Name != "Electronic" {
		t.Fatalf("normalized Managed Track = %+v", committedTrack)
	}
	if committedTrack.Container != "flac" || committedTrack.Codec != "flac" || committedTrack.DurationMs <= 0 || committedTrack.SampleRateHz <= 0 || committedTrack.ChannelCount <= 0 || committedTrack.BitDepth <= 0 || committedTrack.BitrateBps <= 0 {
		t.Fatalf("persisted Managed Track technical properties = %+v", committedTrack)
	}
	idempotentResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{"Content-Type": "application/json"})
	if idempotentResponse.Code != http.StatusOK {
		t.Fatalf("idempotent confirm status = %d, body = %s", idempotentResponse.Code, idempotentResponse.Body.String())
	}
	var idempotentResult struct {
		TrackID string `json:"trackId"`
	}
	decodeJSON(t, idempotentResponse, &idempotentResult)
	if idempotentResult.TrackID != result.TrackID || len(listTracks(t, router).Items) != 1 {
		t.Fatalf("idempotent result = %+v", idempotentResult)
	}
	assertNormalizedAlbum(t, router, committedTrack.AlbumID)
	runLibraryScan(t, router)
	if len(listTracks(t, router).Items) != 1 {
		t.Fatal("legacy library reconciliation hid the committed Managed Track")
	}
	streamResponse := serveRequest(t, router, http.MethodGet, "/api/v1/tracks/"+result.TrackID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK {
		t.Fatalf("stream status = %d, body = %s", streamResponse.Code, streamResponse.Body.String())
	}
	if !bytes.Equal(streamResponse.Body.Bytes(), fixture) {
		t.Fatal("streamed Managed Track bytes differ from uploaded FLAC")
	}

	assertCanonicalStorage(t, managedStoragePath, fixture)
}

func runLibraryScan(t *testing.T, router http.Handler) {
	t.Helper()
	response := serveRequest(t, router, http.MethodPost, "/api/v1/library/scan", nil, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("trigger library scan status = %d, body = %s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusResponse := serveRequest(t, router, http.MethodGet, "/api/v1/library/scan/status", nil, nil)
		var status struct {
			Status string `json:"status"`
		}
		decodeJSON(t, statusResponse, &status)
		if status.Status == "completed" {
			return
		}
		if status.Status == "failed" {
			t.Fatalf("library scan failed: %s", statusResponse.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("library scan did not complete")
}

func TestManagedImportStreamsUploadIntoServerOwnedStaging(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{ManagedStoragePath: managedStoragePath}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, jobResponse, &job)
	reader := &stagingObservingReader{
		data:               readStrictFLACFixture(t),
		managedStoragePath: managedStoragePath,
	}

	response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", reader, map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("streaming upload status = %d, body = %s", response.Code, response.Body.String())
	}
	if !reader.observedStagedBytes {
		t.Fatal("upload body was consumed before bytes appeared in server-owned staging")
	}
}

func TestManagedImportRejectsInvalidFLACWithoutLibraryMutation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{ManagedStoragePath: managedStoragePath}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	libraryModule := library.NewModule(database, configuration)
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, jobResponse, &job)
	fixture := readStrictFLACFixture(t)

	response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture[:len(fixture)-8]), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "truncated.flac",
	})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid FLAC status = %d, body = %s", response.Code, response.Body.String())
	}
	var failure struct {
		Code string `json:"code"`
	}
	decodeJSON(t, response, &failure)
	if failure.Code != "audio_decode_failed" {
		t.Fatalf("invalid FLAC error code = %q", failure.Code)
	}
	if len(listTracks(t, router).Items) != 0 {
		t.Fatal("invalid FLAC mutated the normal library")
	}
	stagedFiles, err := filepath.Glob(filepath.Join(managedStoragePath, ".staging", "*.upload"))
	if err != nil {
		t.Fatalf("list staged files: %v", err)
	}
	if len(stagedFiles) != 0 {
		t.Fatalf("invalid FLAC left staged files: %v", stagedFiles)
	}
}

func TestManagedImportExposesValidationProgressAndCancellation(t *testing.T) {
	inspector := &cancellingInspector{reported: make(chan struct{})}
	router := newManagedImportRouter(t, inspector)
	jobID := createManagedImportJob(t, router)
	cancel, uploadResponse, uploadDone := startManagedImportUpload(t, router, jobID)
	waitForSignal(t, inspector.reported, "validation progress was not reported")
	activeJob := getManagedImportJob(t, router, jobID)
	if activeJob.Status != "uploading" || activeJob.ValidationProgress != 40 || activeJob.ErrorCode != "" {
		t.Fatalf("active Managed Import Job = %+v", activeJob)
	}

	cancel()
	waitForSignal(t, uploadDone, "validation did not stop after cancellation")
	if uploadResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cancelled validation status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	failedJob := getManagedImportJob(t, router, jobID)
	if failedJob.Status != "failed" || failedJob.ValidationProgress != 40 || failedJob.ErrorCode != "validation_cancelled" {
		t.Fatalf("cancelled Managed Import Job = %+v", failedJob)
	}
}

func TestManagedImportRecordsCancellationAtPreviewBoundary(t *testing.T) {
	inspector := &completionCancellingInspector{}
	router := newManagedImportRouter(t, inspector)
	jobID := createManagedImportJob(t, router)
	ctx, cancel := context.WithCancel(context.Background())
	inspector.cancel = cancel
	request := httptest.NewRequest(http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStrictFLACFixture(t))).WithContext(ctx)
	request.Header.Set("Content-Type", "audio/flac")
	request.Header.Set("X-Import-Filename", "strict-import.flac")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	job := getManagedImportJob(t, router, jobID)
	if job.Status != "failed" || job.ValidationProgress != 100 || job.ErrorCode != "validation_cancelled" {
		t.Fatalf("preview-boundary cancellation result = %+v", job)
	}
}

func TestManagedImportDetectsFLACWithoutTrustingFilenameOrContentType(t *testing.T) {
	router := newManagedImportRouter(t, library.NewMediaInspector())
	jobID := createManagedImportJob(t, router)

	response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStrictFLACFixture(t)), map[string]string{
		"Content-Type":      "audio/mpeg",
		"X-Import-Filename": "misleading.mp3",
	})

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"container":"flac"`) || !strings.Contains(response.Body.String(), `"codec":"flac"`) {
		t.Fatalf("detected FLAC response = %d %s", response.Code, response.Body.String())
	}
}

type managedImportJobResult struct {
	Status             string `json:"status"`
	ValidationProgress int    `json:"validationProgress"`
	ErrorCode          string `json:"errorCode"`
}

func getManagedImportJob(t *testing.T, router http.Handler, jobID string) managedImportJobResult {
	t.Helper()
	response := serveRequest(t, router, http.MethodGet, "/api/v1/imports/"+jobID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get Managed Import Job status = %d, body = %s", response.Code, response.Body.String())
	}
	var result managedImportJobResult
	decodeJSON(t, response, &result)
	return result
}

type cancellingInspector struct {
	reported chan struct{}
}

type completionCancellingInspector struct {
	cancel context.CancelFunc
}

func (inspector *completionCancellingInspector) Inspect(ctx context.Context, path string, reportProgress library.InspectionProgressReporter) (library.MediaInspection, error) {
	inspection, err := library.NewMediaInspector().Inspect(ctx, path, reportProgress)
	if err != nil {
		return library.MediaInspection{}, err
	}
	inspector.cancel()
	return inspection, nil
}

func createManagedImportJob(t *testing.T, router http.Handler) string {
	t.Helper()
	response := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, response, &job)
	return job.ID
}

func newManagedImportRouter(t *testing.T, inspector library.MediaInspector) http.Handler {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	router := chi.NewRouter()
	managedimport.NewModule(database, configuration, inspector).RegisterRoutes(router)
	return router
}

func startManagedImportUpload(t *testing.T, router http.Handler, jobID string) (context.CancelFunc, *httptest.ResponseRecorder, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStrictFLACFixture(t))).WithContext(ctx)
	request.Header.Set("Content-Type", "audio/flac")
	request.Header.Set("X-Import-Filename", "strict-import.flac")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(response, request)
	}()
	return cancel, response, done
}

func waitForSignal(t *testing.T, signal <-chan struct{}, failureMessage string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(failureMessage)
	}
}

func (inspector *cancellingInspector) Inspect(ctx context.Context, _ string, reportProgress library.InspectionProgressReporter) (library.MediaInspection, error) {
	if err := reportProgress(library.InspectionProgress{DecodedSamples: 40, TotalSamples: 100, Percent: 40}); err != nil {
		return library.MediaInspection{}, err
	}
	close(inspector.reported)
	<-ctx.Done()
	return library.MediaInspection{}, &library.InspectionError{
		Code:  library.INSPECTION_ERROR_VALIDATION_CANCELLED,
		Field: "validation",
		Err:   ctx.Err(),
	}
}

func TestManagedImportReusesMatchingNormalizedAlbumArtwork(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	libraryModule := library.NewModule(database, configuration)
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	firstFixture := readStrictFLACFixture(t)
	firstTrackID := importOneFLAC(t, router, firstFixture, "first.flac")
	secondFixture := bytes.Replace(firstFixture, []byte("TITLE=  Inspection   Fixture  "), []byte("TITLE=  Inspection   Second!  "), 1)
	secondFixture = bytes.Replace(secondFixture, []byte("TRACKNUMBER=3/9"), []byte("TRACKNUMBER=4/9"), 1)

	secondTrackID := importOneFLAC(t, router, secondFixture, "second.flac")

	if secondTrackID == firstTrackID {
		t.Fatalf("second Managed Track reused Track ID %q", firstTrackID)
	}
	tracks := listTracks(t, router)
	if len(tracks.Items) != 2 || tracks.Items[0].AlbumID != tracks.Items[1].AlbumID {
		t.Fatalf("Tracks in matching normalized Album = %+v", tracks.Items)
	}
	assertNormalizedAlbum(t, router, tracks.Items[0].AlbumID)
}

func importOneFLAC(t *testing.T, router http.Handler, fixture []byte, filename string) string {
	t.Helper()
	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, jobResponse, &job)
	uploadResponse := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": filename,
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload %q status = %d, body = %s", filename, uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
	}
	decodeJSON(t, uploadResponse, &preview)
	confirmation := strings.NewReader(fmt.Sprintf(`{"revision":%d}`, preview.Revision))
	confirmResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", confirmation, map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm %q status = %d, body = %s", filename, confirmResponse.Code, confirmResponse.Body.String())
	}
	var result struct {
		TrackID string `json:"trackId"`
	}
	decodeJSON(t, confirmResponse, &result)
	return result.TrackID
}

type stagingObservingReader struct {
	data                []byte
	offset              int
	managedStoragePath  string
	observedStagedBytes bool
}

func (reader *stagingObservingReader) Read(buffer []byte) (int, error) {
	if reader.offset > 0 && !reader.observedStagedBytes {
		matches, err := filepath.Glob(filepath.Join(reader.managedStoragePath, ".staging", "*.upload"))
		if err != nil {
			return 0, err
		}
		for _, path := range matches {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return 0, statErr
			}
			if info.Size() > 0 {
				reader.observedStagedBytes = true
				break
			}
		}
		if !reader.observedStagedBytes {
			return 0, errors.New("server-owned staging has no bytes while upload is still being read")
		}
	}
	if reader.offset == len(reader.data) {
		return 0, io.EOF
	}
	chunkSize := min(256, len(reader.data)-reader.offset, len(buffer))
	copy(buffer, reader.data[reader.offset:reader.offset+chunkSize])
	reader.offset += chunkSize
	return chunkSize, nil
}

type trackList struct {
	Items []struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		AlbumID      string `json:"albumId"`
		AlbumTitle   string `json:"albumTitle"`
		DiscNo       int    `json:"discNo"`
		DurationMs   int    `json:"durationMs"`
		Container    string `json:"container"`
		Codec        string `json:"codec"`
		SampleRateHz int    `json:"sampleRateHz"`
		ChannelCount int    `json:"channelCount"`
		BitDepth     int    `json:"bitDepth"`
		BitrateBps   int    `json:"bitrateBps"`
		Artists      []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
	} `json:"items"`
}

func assertNormalizedAlbum(t *testing.T, router http.Handler, albumID string) {
	t.Helper()
	response := serveRequest(t, router, http.MethodGet, "/api/v1/library/albums/"+albumID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get imported Album status = %d, body = %s", response.Code, response.Body.String())
	}
	var album struct {
		Title        string `json:"title"`
		ReleaseDate  string `json:"releaseDate"`
		AlbumArtists []struct {
			Name string `json:"name"`
		} `json:"albumArtists"`
		GenreItems []struct {
			Name string `json:"name"`
		} `json:"genreItems"`
		Artwork *struct {
			MediaType string `json:"mediaType"`
		} `json:"artwork"`
	}
	decodeJSON(t, response, &album)
	if album.Title != "Strict Import Tests" || album.ReleaseDate != "2026" || len(album.AlbumArtists) != 1 || album.AlbumArtists[0].Name != "Test Album Artist" {
		t.Fatalf("normalized imported Album = %+v", album)
	}
	if len(album.GenreItems) != 1 || album.GenreItems[0].Name != "Electronic" || album.Artwork == nil || album.Artwork.MediaType != "image/png" {
		t.Fatalf("imported Album Genre/Artwork = %+v", album)
	}
	coverResponse := serveRequest(t, router, http.MethodGet, "/api/v1/library/albums/"+albumID+"/cover", nil, nil)
	if coverResponse.Code != http.StatusOK || coverResponse.Header().Get("Content-Type") != "image/png" || coverResponse.Body.Len() == 0 {
		t.Fatalf("imported Album Artwork response = %d %q (%d bytes)", coverResponse.Code, coverResponse.Header().Get("Content-Type"), coverResponse.Body.Len())
	}
}

func listTracks(t *testing.T, router http.Handler) trackList {
	t.Helper()
	response := serveRequest(t, router, http.MethodGet, "/api/v1/library/tracks", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list Tracks status = %d, body = %s", response.Code, response.Body.String())
	}
	var result trackList
	decodeJSON(t, response, &result)
	return result
}

func assertCanonicalStorage(t *testing.T, managedStoragePath string, fixture []byte) {
	t.Helper()
	var audioPath string
	var artworkPath string
	err := filepath.WalkDir(managedStoragePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".flac":
			audioPath = path
		case ".png":
			artworkPath = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Managed Storage: %v", err)
	}
	if audioPath == "" || artworkPath == "" {
		t.Fatalf("canonical audio/artwork paths = %q / %q", audioPath, artworkPath)
	}
	if filepath.Base(audioPath) == "strict-import.flac" || !strings.Contains(audioPath, "strict-import-tests") || !strings.Contains(audioPath, "inspection-fixture") {
		t.Fatalf("audio path is not canonical: %q", audioPath)
	}
	stored, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read canonical audio: %v", err)
	}
	if !bytes.Equal(stored, fixture) {
		t.Fatal("canonical audio bytes differ from uploaded FLAC")
	}
}

func readStrictFLACFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "library", "testdata", "strict-import.flac"))
	if err != nil {
		t.Fatalf("read strict FLAC fixture: %v", err)
	}
	return fixture
}

func serveRequest(t *testing.T, router http.Handler, method string, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
