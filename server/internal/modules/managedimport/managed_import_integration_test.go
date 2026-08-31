package managedimport_test

import (
	"bytes"
	"database/sql"
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
	if preview.File.Format != "flac" || preview.File.ArtworkMediaType != "image/png" {
		t.Fatalf("Import Preview media = %+v", preview.File)
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

func TestManagedImportRejectsMissingAndEmptyIdentityMetadataWithoutLibraryEntities(t *testing.T) {
	testCases := []struct {
		name        string
		tag         string
		fixtureTag  string
		replacement string
	}{
		{name: "missing TITLE", tag: "TITLE", fixtureTag: "TITLE=  Inspection   Fixture  ", replacement: "XITLE=  Inspection   Fixture  "},
		{name: "empty TITLE", tag: "TITLE", fixtureTag: "TITLE=  Inspection   Fixture  "},
		{name: "missing ARTIST", tag: "ARTIST", fixtureTag: "ARTIST=Test Artist", replacement: "XRTIST=Test Artist"},
		{name: "empty ARTIST", tag: "ARTIST", fixtureTag: "ARTIST=Test Artist"},
		{name: "missing ALBUMARTIST", tag: "ALBUMARTIST", fixtureTag: "ALBUMARTIST=Test Album Artist", replacement: "XLBUMARTIST=Test Album Artist"},
		{name: "empty ALBUMARTIST", tag: "ALBUMARTIST", fixtureTag: "ALBUMARTIST=Test Album Artist"},
		{name: "missing ALBUM", tag: "ALBUM", fixtureTag: "ALBUM=Strict Import Tests", replacement: "XLBUM=Strict Import Tests"},
		{name: "empty ALBUM", tag: "ALBUM", fixtureTag: "ALBUM=Strict Import Tests"},
		{name: "missing TRACKNUMBER", tag: "TRACKNUMBER", fixtureTag: "TRACKNUMBER=3/9", replacement: "XRACKNUMBER=3/9"},
		{name: "empty TRACKNUMBER", tag: "TRACKNUMBER", fixtureTag: "TRACKNUMBER=3/9"},
		{name: "missing GENRE", tag: "GENRE", fixtureTag: "GENRE=Electronic", replacement: "XENRE=Electronic"},
		{name: "empty GENRE", tag: "GENRE", fixtureTag: "GENRE=Electronic"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			configuration := config.Config{ManagedStoragePath: t.TempDir()}
			importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
			router := chi.NewRouter()
			importModule.RegisterRoutes(router)
			fixture := replaceFixtureTag(t, readStrictFLACFixture(t), testCase.fixtureTag, testCase.replacement)

			jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
			var job struct {
				ID string `json:"id"`
			}
			decodeJSON(t, jobResponse, &job)
			response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
				"Content-Type":      "audio/flac",
				"X-Import-Filename": "invalid-identity.flac",
			})

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid identity status = %d, body = %s", response.Code, response.Body.String())
			}
			var failure struct {
				Code  string `json:"code"`
				Field string `json:"field"`
			}
			decodeJSON(t, response, &failure)
			if failure.Code != "invalid_metadata" || failure.Field != testCase.tag {
				t.Fatalf("invalid identity error = %+v", failure)
			}
			assertNoLibraryEntities(t, database)
		})
	}
}

func TestManagedImportRequiresDiscNumberForTaggedMultiDiscFile(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "XISCNUMBER=1/1")
	fixture = replaceFixtureTag(t, fixture, "encoder=Lavf63.1.101", "TOTALDISCS=2")
	response, database := uploadManagedImportFixture(t, fixture)

	assertValidationFailure(t, response, "invalid_metadata", "DISCNUMBER")
	assertNoLibraryEntities(t, database)
}

func TestManagedImportUsesEffectiveDiscOneForSingleDiscFile(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "XISCNUMBER=1/1")
	fixture = replaceFixtureTag(t, fixture, "encoder=Lavf63.1.101", "TOTALDISCS=1")
	response, database := uploadManagedImportFixture(t, fixture)

	if response.Code != http.StatusOK {
		t.Fatalf("single-disc upload status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview struct {
		File struct {
			DiscNo    int `json:"discNo"`
			DiscTotal int `json:"discTotal"`
		} `json:"file"`
	}
	decodeJSON(t, response, &preview)
	if preview.File.DiscNo != 1 || preview.File.DiscTotal != 1 {
		t.Fatalf("single-disc position = %d/%d", preview.File.DiscNo, preview.File.DiscTotal)
	}
	assertNoLibraryEntities(t, database)
}

func TestManagedImportReadsSeparateTrackTotal(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "TRACKNUMBER=3/9", "TRACKNUMBER=3")
	fixture = replaceFixtureTag(t, fixture, "encoder=Lavf63.1.101", "TOTALTRACKS=9")
	response, database := uploadManagedImportFixture(t, fixture)

	if response.Code != http.StatusOK {
		t.Fatalf("separate Track total upload status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview struct {
		File struct {
			TrackNo    int `json:"trackNo"`
			TrackTotal int `json:"trackTotal"`
		} `json:"file"`
	}
	decodeJSON(t, response, &preview)
	if preview.File.TrackNo != 3 || preview.File.TrackTotal != 9 {
		t.Fatalf("Track position = %d/%d", preview.File.TrackNo, preview.File.TrackTotal)
	}
	assertNoLibraryEntities(t, database)
}

func TestManagedImportReadsSeparateDiscTotal(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "DISCNUMBER=1")
	fixture = replaceFixtureTag(t, fixture, "encoder=Lavf63.1.101", "TOTALDISCS=2")
	response, database := uploadManagedImportFixture(t, fixture)

	if response.Code != http.StatusOK {
		t.Fatalf("separate Disc total upload status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview struct {
		File struct {
			DiscNo    int `json:"discNo"`
			DiscTotal int `json:"discTotal"`
		} `json:"file"`
	}
	decodeJSON(t, response, &preview)
	if preview.File.DiscNo != 1 || preview.File.DiscTotal != 2 {
		t.Fatalf("Disc position = %d/%d", preview.File.DiscNo, preview.File.DiscTotal)
	}
	assertNoLibraryEntities(t, database)
}

func TestManagedImportRejectsInvalidOptionalTotals(t *testing.T) {
	testCases := []struct {
		name  string
		tag   string
		value string
	}{
		{name: "Track total below Track number", tag: "TOTALTRACKS", value: "TOTALTRACKS=2"},
		{name: "Track total conflicts with inline total", tag: "TOTALTRACKS", value: "TOTALTRACKS=8"},
		{name: "Disc total conflicts with inline total", tag: "TOTALDISCS", value: "TOTALDISCS=2"},
		{name: "Disc total is not positive", tag: "TOTALDISCS", value: "TOTALDISCS=0"},
		{name: "Disc total exceeds supported range", tag: "TOTALDISCS", value: "TOTALDISCS=10000"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "encoder=Lavf63.1.101", testCase.value)
			response, database := uploadManagedImportFixture(t, fixture)

			assertValidationFailure(t, response, "invalid_metadata", testCase.tag)
			assertNoLibraryEntities(t, database)
		})
	}
}

func TestManagedImportRequiresDiscNumberForExistingMultiDiscAlbum(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	libraryModule := library.NewModule(database, configuration)
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	multiDiscFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "DISCNUMBER=2/2")
	importOneFLAC(t, router, multiDiscFixture, "disc-two.flac")
	untaggedFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "XISCNUMBER=1/1")

	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, jobResponse, &job)
	response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(untaggedFixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "missing-disc-number.flac",
	})

	assertValidationFailure(t, response, "invalid_metadata", "DISCNUMBER")
	if tracks := listTracks(t, router); len(tracks.Items) != 1 || tracks.Items[0].DiscNo != 2 {
		t.Fatalf("existing multi-disc Album Tracks = %+v", tracks.Items)
	}
}

func TestManagedImportRequiresDiscNumberForAwaitingMultiDiscSibling(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	multiDiscFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "DISCNUMBER=2/2")
	firstResponse := uploadFixtureThroughRouter(t, router, multiDiscFixture)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("multi-disc sibling upload status = %d, body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	untaggedFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "DISCNUMBER=1/1", "XISCNUMBER=1/1")
	secondResponse := uploadFixtureThroughRouter(t, router, untaggedFixture)

	assertValidationFailure(t, secondResponse, "invalid_metadata", "DISCNUMBER")
	assertNoLibraryEntities(t, database)
}

func TestManagedImportSeparatesDisplayValuesFromUnicodeComparisonKeys(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	libraryModule := library.NewModule(database, configuration)
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	firstFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ALBUMARTIST=Test Album Artist", "ALBUMARTIST=Straße Artist")
	importOneFLAC(t, router, firstFixture, "first-case.flac")
	secondFixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ALBUMARTIST=Test Album Artist", "ALBUMARTIST=STRASSE ARTIST")
	secondFixture = replaceFixtureTag(t, secondFixture, "TITLE=  Inspection   Fixture  ", "TITLE=  Inspection   Second!  ")
	secondFixture = replaceFixtureTag(t, secondFixture, "TRACKNUMBER=3/9", "TRACKNUMBER=4/9")
	importOneFLAC(t, router, secondFixture, "second-case.flac")

	tracks := listTracks(t, router)
	if len(tracks.Items) != 2 || tracks.Items[0].AlbumID != tracks.Items[1].AlbumID {
		t.Fatalf("Unicode case-folded Albums = %+v", tracks.Items)
	}
	response := serveRequest(t, router, http.MethodGet, "/api/v1/library/albums/"+tracks.Items[0].AlbumID, nil, nil)
	var album struct {
		AlbumArtists []struct {
			Name string `json:"name"`
		} `json:"albumArtists"`
	}
	decodeJSON(t, response, &album)
	if len(album.AlbumArtists) != 1 || album.AlbumArtists[0].Name != "Straße Artist" {
		t.Fatalf("Album Artist display value = %+v", album.AlbumArtists)
	}
}

func TestManagedImportRejectsControlCharactersInIdentityValues(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ARTIST=Test Artist", "ARTIST=Test\x1fArtist")
	response, database := uploadManagedImportFixture(t, fixture)

	assertValidationFailure(t, response, "invalid_metadata", "ARTIST")
	assertNoLibraryEntities(t, database)
}

func TestManagedImportRejectsControlWhitespaceAtIdentityValueEdges(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "Leading tab", value: "ARTIST=\tTest Artis"},
		{name: "Trailing newline", value: "ARTIST=Test Artis\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ARTIST=Test Artist", testCase.value)
			response, database := uploadManagedImportFixture(t, fixture)

			assertValidationFailure(t, response, "invalid_metadata", "ARTIST")
			assertNoLibraryEntities(t, database)
		})
	}
}

func TestManagedImportRejectsBidirectionalControlsInIdentityValues(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ARTIST=Test Artist", "ARTIST=A\u202e Artist")
	response, database := uploadManagedImportFixture(t, fixture)

	assertValidationFailure(t, response, "invalid_metadata", "ARTIST")
	assertNoLibraryEntities(t, database)
}

func TestManagedImportRejectsInvalidUTF8InIdentityValues(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "ARTIST=Test Artist", "ARTIST=\xffest Artist")
	response, database := uploadManagedImportFixture(t, fixture)

	assertValidationFailure(t, response, "invalid_metadata", "ARTIST")
	assertNoLibraryEntities(t, database)
}

func TestManagedImportValidationErrorsExplainHowToRepairMetadata(t *testing.T) {
	fixture := replaceFixtureTag(t, readStrictFLACFixture(t), "TITLE=  Inspection   Fixture  ", "XITLE=  Inspection   Fixture  ")
	response, _ := uploadManagedImportFixture(t, fixture)
	var failure struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	decodeJSON(t, response, &failure)

	if failure.Reason != "required tag is missing" || !strings.Contains(failure.Message, failure.Reason) {
		t.Fatalf("actionable validation error = %+v", failure)
	}
}

func TestManagedImportValidationErrorsDoNotExposeParserDetails(t *testing.T) {
	fixture := readStrictFLACFixture(t)
	response, _ := uploadManagedImportFixture(t, fixture[:len(fixture)-8])
	var failure struct {
		Code   string `json:"code"`
		Field  string `json:"field"`
		Reason string `json:"reason"`
	}
	decodeJSON(t, response, &failure)

	if failure.Code != "audio_decode_failed" || failure.Field != "audio" || failure.Reason != "audio stream failed full decode" {
		t.Fatalf("safe validation error = %+v", failure)
	}
}

func TestManagedImportRejectsTotalsConflictingWithExistingAlbum(t *testing.T) {
	testCases := []struct {
		name             string
		firstPosition    string
		secondPosition   string
		expectedField    string
		positionTagValue string
	}{
		{name: "Disc total", firstPosition: "DISCNUMBER=1/2", secondPosition: "DISCNUMBER=2/3", expectedField: "TOTALDISCS", positionTagValue: "DISCNUMBER=1/1"},
		{name: "Disc total below existing position", firstPosition: "DISCNUMBER=2", secondPosition: "DISCNUMBER=1/1", expectedField: "TOTALDISCS", positionTagValue: "DISCNUMBER=1/1"},
		{name: "Track total on same disc", firstPosition: "TRACKNUMBER=2/9", secondPosition: "TRACKNUMBER=4/8", expectedField: "TOTALTRACKS", positionTagValue: "TRACKNUMBER=3/9"},
		{name: "Track total below existing position", firstPosition: "TRACKNUMBER=10", secondPosition: "TRACKNUMBER=1/9", expectedField: "TOTALTRACKS", positionTagValue: "TRACKNUMBER=3/9"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			configuration := config.Config{ManagedStoragePath: t.TempDir()}
			importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
			libraryModule := library.NewModule(database, configuration)
			router := chi.NewRouter()
			importModule.RegisterRoutes(router)
			libraryModule.RegisterRoutes(router)
			firstFixture := replaceFixtureTag(t, readStrictFLACFixture(t), testCase.positionTagValue, testCase.firstPosition)
			importOneFLAC(t, router, firstFixture, "first-total.flac")
			secondFixture := readStrictFLACFixture(t)
			if testCase.secondPosition != testCase.positionTagValue {
				secondFixture = replaceFixtureTag(t, secondFixture, testCase.positionTagValue, testCase.secondPosition)
			}
			secondFixture = replaceFixtureTag(t, secondFixture, "TITLE=  Inspection   Fixture  ", "TITLE=  Inspection   Second!  ")

			jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
			var job struct {
				ID string `json:"id"`
			}
			decodeJSON(t, jobResponse, &job)
			response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(secondFixture), map[string]string{
				"Content-Type":      "audio/flac",
				"X-Import-Filename": "conflicting-total.flac",
			})

			assertValidationFailure(t, response, "invalid_metadata", testCase.expectedField)
			if tracks := listTracks(t, router); len(tracks.Items) != 1 {
				t.Fatalf("Tracks after conflicting total = %+v", tracks.Items)
			}
		})
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
		ID         string `json:"id"`
		Title      string `json:"title"`
		AlbumID    string `json:"albumId"`
		AlbumTitle string `json:"albumTitle"`
		DiscNo     int    `json:"discNo"`
		Artists    []struct {
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

func replaceFixtureTag(t *testing.T, fixture []byte, oldValue string, newValue string) []byte {
	t.Helper()
	if newValue == "" {
		separatorIndex := strings.Index(oldValue, "=")
		newValue = oldValue[:separatorIndex+1] + strings.Repeat(" ", len(oldValue)-separatorIndex-1)
	}
	if len(newValue) < len(oldValue) {
		newValue += strings.Repeat(" ", len(oldValue)-len(newValue))
	}
	if len(oldValue) != len(newValue) {
		t.Fatalf("fixture tag replacement lengths differ: %d != %d", len(oldValue), len(newValue))
	}
	updated := bytes.Replace(fixture, []byte(oldValue), []byte(newValue), 1)
	if bytes.Equal(updated, fixture) {
		t.Fatalf("fixture tag %q not found", oldValue)
	}
	return updated
}

func uploadManagedImportFixture(t *testing.T, fixture []byte) (*httptest.ResponseRecorder, *sql.DB) {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	return uploadFixtureThroughRouter(t, router, fixture), database
}

func uploadFixtureThroughRouter(t *testing.T, router http.Handler, fixture []byte) *httptest.ResponseRecorder {
	t.Helper()
	jobResponse := serveRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	decodeJSON(t, jobResponse, &job)
	response := serveRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "identity-test.flac",
	})
	return response
}

func assertValidationFailure(t *testing.T, response *httptest.ResponseRecorder, expectedCode, expectedField string) {
	t.Helper()
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d, body = %s", response.Code, response.Body.String())
	}
	var failure struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	}
	decodeJSON(t, response, &failure)
	if failure.Code != expectedCode || failure.Field != expectedField {
		t.Fatalf("validation error = %+v", failure)
	}
}

func assertNoLibraryEntities(t *testing.T, database interface {
	QueryRow(query string, args ...any) *sql.Row
}) {
	t.Helper()
	for _, tableName := range []string{"artists", "albums", "genres", "album_artwork", "tracks"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", tableName, err)
		}
		if count != 0 {
			t.Fatalf("%s row count = %d, want 0", tableName, count)
		}
	}
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
