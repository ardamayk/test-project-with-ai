package librarysearch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/librarysearch"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

// Reuse the real strict audio fixture. All observations are HTTP Search results;
// SQL is used only to inject an atomic write failure, never as a result oracle.
func TestLibrarySearchQualityImportReplacementDeletion(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	handler := chi.NewRouter()
	managedimport.NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector()).RegisterRoutes(handler)
	librarysearch.NewModule(database).RegisterRoutes(handler)
	request := func(method, path string, body []byte, headers map[string]string, status int, target any) {
		t.Helper()
		response := testutil.ServeRequest(t, handler, method, path, bytes.NewReader(body), headers)
		if response.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.Code, status, response.Body.String())
		}
		if target != nil {
			testutil.DecodeJSON(t, response, target)
		}
	}
	assertEmpty := func(query string) {
		t.Helper()
		result := search(t, handler, query)
		if result.BestMatch != nil {
			t.Fatalf("%q unexpectedly found %+v", query, result.BestMatch)
		}
		for kind, group := range qualityGroups(result) {
			assertIDs(t, query+" "+kind, group)
		}
	}
	fixture := testutil.StrictMP3Fixture()
	var job managedimport.Job
	request(http.MethodPost, "/api/v1/imports", nil, nil, http.StatusCreated, &job)
	var preview managedimport.Preview
	uploadHeaders := map[string]string{"Content-Type": "audio/mpeg", "X-Import-Filename": "quality.mp3"}
	request(http.MethodPut, "/api/v1/imports/"+job.ID+"/file", fixture, uploadHeaders, http.StatusOK, &preview)
	assertEmpty("MP3 Inspection Fixture")
	var imported struct {
		TrackID string `json:"trackId"`
	}
	request(http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", []byte(fmt.Sprintf(`{"revision":%d}`, preview.Revision)), map[string]string{"Content-Type": "application/json"}, http.StatusOK, &imported)
	if imported.TrackID == "" {
		t.Fatal("import omitted Track identity")
	}
	assertBestMatch(t, search(t, handler, "MP3 Inspection Fixture"), apigen.LibrarySearchResultTypeTrack, imported.TrackID)
	result := search(t, handler, "guest artist mp3")
	assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, imported.TrackID)
	if result.BestMatch.Artists == nil || len(*result.BestMatch.Artists) != 2 {
		t.Fatalf("structured import credits missing: %+v", result.BestMatch)
	}

	// Equal byte length keeps the existing ID3 frame intact, changing metadata
	// only. This is the same strict-fixture replacement technique as import tests.
	replacement := bytes.Replace(fixture, []byte("Guest Artist"), []byte("Beyoncé One"), 1)
	if bytes.Equal(fixture, replacement) || len(fixture) != len(replacement) {
		t.Fatal("replacement fixture must change exactly one equal-length credit")
	}
	request(http.MethodPost, "/api/v1/library/tracks/"+imported.TrackID+"/replacement", nil, nil, http.StatusCreated, &job)
	request(http.MethodPut, "/api/v1/imports/"+job.ID+"/file", replacement, uploadHeaders, http.StatusOK, &preview)
	if preview.Replacement == nil {
		t.Fatal("missing replacement preview")
	}
	assertEmpty("beyonce")
	assertBestMatch(t, search(t, handler, "guest artist mp3"), apigen.LibrarySearchResultTypeTrack, imported.TrackID)
	confirm := []byte(fmt.Sprintf(`{"revision":%d,"confirmationToken":%q}`, preview.Revision, preview.Replacement.ConfirmationToken))
	confirmHeaders := map[string]string{"Content-Type": "application/json", managedimport.TRACK_REPLACEMENT_CONFIRMATION_HEADER: "1"}
	request(http.MethodPost, "/api/v1/imports/"+job.ID+"/replacement", confirm, confirmHeaders, http.StatusOK, nil)
	result = search(t, handler, "beyonce mp3")
	assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, imported.TrackID)
	if result.BestMatch.Artists == nil || len(*result.BestMatch.Artists) != 2 || (*result.BestMatch.Artists)[1].Name != "Beyoncé One" {
		t.Fatalf("replacement lost original display credit: %+v", result.BestMatch)
	}
	assertEmpty("guest artist")

	request(http.MethodPost, "/api/v1/library/tracks/"+imported.TrackID+"/replacement", nil, nil, http.StatusCreated, &job)
	rejected := bytes.Replace(replacement, []byte("Beyoncé One"), []byte("Şebnem Solo"), 1)
	if len(rejected) != len(replacement) {
		t.Fatal("rejected replacement must preserve ID3 frame length")
	}
	request(http.MethodPut, "/api/v1/imports/"+job.ID+"/file", rejected, uploadHeaders, http.StatusOK, &preview)
	if preview.Replacement == nil {
		t.Fatal("missing rejected replacement preview")
	}
	execute(t, database, `CREATE TRIGGER reject_quality_search_write BEFORE INSERT ON library_search_texts WHEN NEW.kind = 'track' BEGIN SELECT RAISE(ABORT, 'injected search preparation failure'); END`)
	confirm = []byte(fmt.Sprintf(`{"revision":%d,"confirmationToken":%q}`, preview.Revision, preview.Replacement.ConfirmationToken))
	response := testutil.ServeRequest(t, handler, http.MethodPost, "/api/v1/imports/"+job.ID+"/replacement", bytes.NewReader(confirm), confirmHeaders)
	if response.Code < http.StatusBadRequest {
		t.Fatalf("failed preparation reported success: %s", response.Body.String())
	}
	execute(t, database, `DROP TRIGGER reject_quality_search_write`)
	assertBestMatch(t, search(t, handler, "beyonce mp3"), apigen.LibrarySearchResultTypeTrack, imported.TrackID)
	assertEmpty("sebnem")

	var deletion managedimport.TrackDeletionPreview
	request(http.MethodGet, "/api/v1/library/tracks/"+imported.TrackID+"/deletion", nil, nil, http.StatusOK, &deletion)
	body, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: deletion.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodDelete, "/api/v1/library/tracks/"+imported.TrackID, body, map[string]string{"Content-Type": "application/json", managedimport.PERMANENT_DELETE_CONFIRMATION_HEADER: "1"}, http.StatusOK, nil)
	for _, query := range []string{"MP3 Inspection Fixture", "beyonce", "Primary Artist", "Strict MP3 Import Tests", "Electronic", "Ambient"} {
		assertEmpty(query)
	}
}
