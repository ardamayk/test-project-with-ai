package managedimport_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/google/uuid"
)

// These tests exercise the Managed Import surface purely at the HTTP seam and
// validate every response against the embedded OpenAPI contract, so the
// generated clients can trust the documented schemas without UI journeys.

const CONTRACT_JSON_MEDIA_TYPE = "application/json"

type contractPreview struct {
	Status                  string `json:"status"`
	Revision                int    `json:"revision"`
	DuplicateClassification string `json:"duplicateClassification"`
	File                    struct {
		OriginalFilename string `json:"originalFilename"`
		Format           string `json:"format"`
	} `json:"file"`
	Replacement *struct {
		ConfirmationToken string `json:"confirmationToken"`
	} `json:"replacement"`
}

type contractJob struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Revision        int    `json:"revision"`
	TrackID         string `json:"trackId"`
	ReplacesTrackID string `json:"replacesTrackId"`
}

type contractBatch struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Revision int    `json:"revision"`
	Files    []struct {
		JobID   string `json:"jobId"`
		State   string `json:"state"`
		Status  string `json:"status"`
		Outcome string `json:"outcome"`
		TrackID string `json:"trackId"`
	} `json:"files"`
}

func contractJSONRequest(method, path string, body any, headers map[string]string) testutil.ContractRequest {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	merged := map[string]string{"Content-Type": CONTRACT_JSON_MEDIA_TYPE}
	for name, value := range headers {
		merged[name] = value
	}
	return testutil.ContractRequest{Method: method, Path: path, Body: encoded, Headers: merged}
}

func contractUploadRequest(jobID, mediaType, filename string, fixture []byte, extraHeaders map[string]string) testutil.ContractRequest {
	headers := map[string]string{"Content-Type": mediaType, "X-Import-Filename": filename}
	for name, value := range extraHeaders {
		headers[name] = value
	}
	return testutil.ContractRequest{Method: http.MethodPut, Path: "/api/v1/imports/" + jobID + "/file", Body: fixture, Headers: headers}
}

func decodeContract[T any](t *testing.T, response *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, wantStatus, response.Body.String())
	}
	var value T
	testutil.DecodeJSON(t, response, &value)
	return value
}

func TestContractCoversImportJobLifecycleAndStructuredErrors(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	router := newTrackReplacementRouter(t, database, t.TempDir())
	fixture := readStrictFLACFixture(t)

	job := decodeContract[contractJob](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/imports"}), http.StatusCreated)
	if job.Status != string(managedimport.STATUS_UPLOADING) {
		t.Fatalf("new Import Job = %+v", job)
	}

	uploaded := testutil.ServeContractRequest(t, router, contractUploadRequest(job.ID, "audio/flac", "Strict%20Import%20%C3%A9.flac", fixture, map[string]string{"X-Import-Filename-Encoding": "url"}))
	preview := decodeContract[contractPreview](t, uploaded, http.StatusOK)
	if preview.Status != string(managedimport.STATUS_AWAITING_CONFIRMATION) || preview.Revision < 1 || preview.File.OriginalFilename != "Strict Import é.flac" || preview.File.Format != "flac" {
		t.Fatalf("Import Preview = %+v", preview)
	}

	polled := decodeContract[contractJob](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/imports/" + job.ID}), http.StatusOK)
	if polled.Status != string(managedimport.STATUS_AWAITING_CONFIRMATION) || polled.Revision != preview.Revision {
		t.Fatalf("polled Import Job = %+v, want awaiting confirmation at revision %d", polled, preview.Revision)
	}

	invalid := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", map[string]any{}, nil))
	testutil.AssertStructuredError(t, invalid, http.StatusBadRequest, "invalid_confirmation")

	stale := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", map[string]any{"revision": preview.Revision + 1}, nil))
	testutil.AssertStructuredError(t, stale, http.StatusConflict, managedimport.ERROR_CODE_REVISION_CONFLICT)

	confirmed := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", map[string]any{"revision": preview.Revision}, nil))
	result := decodeContract[contractJob](t, confirmed, http.StatusOK)
	if result.Status != string(managedimport.STATUS_COMMITTED) || result.TrackID == "" {
		t.Fatalf("Managed Import result = %+v", result)
	}

	committed := decodeContract[contractJob](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/imports/" + job.ID}), http.StatusOK)
	if committed.Status != string(managedimport.STATUS_COMMITTED) || committed.TrackID != result.TrackID {
		t.Fatalf("committed Import Job = %+v", committed)
	}
	testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/library/tracks/" + result.TrackID})

	history := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/import-history"})
	var historyList struct {
		Items []struct {
			Files []struct {
				ResultCode string `json:"resultCode"`
			} `json:"files"`
		} `json:"items"`
	}
	testutil.DecodeJSON(t, history, &historyList)
	if history.Code != http.StatusOK || len(historyList.Items) != 1 || len(historyList.Items[0].Files) != 1 || historyList.Items[0].Files[0].ResultCode != "imported" {
		t.Fatalf("Import History = %d %+v", history.Code, historyList)
	}

	sealed := testutil.ServeContractRequest(t, router, contractUploadRequest(job.ID, "audio/flac", "again.flac", fixture, nil))
	testutil.AssertStructuredError(t, sealed, http.StatusConflict, "import_state_conflict")

	missing := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/imports/" + uuid.NewString()})
	testutil.AssertStructuredError(t, missing, http.StatusNotFound, "import_not_found")

	badEncoding := testutil.ServeContractRequest(t, router, contractUploadRequest(job.ID, "audio/flac", "%zz.flac", fixture, map[string]string{"X-Import-Filename-Encoding": "url"}))
	testutil.AssertStructuredError(t, badEncoding, http.StatusBadRequest, "invalid_import_filename")

	canceled := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodDelete, Path: "/api/v1/imports/" + createImportJob(t, router)})
	if canceled.Code != http.StatusNoContent {
		t.Fatalf("cancel Import Job status = %d, body = %s", canceled.Code, canceled.Body.String())
	}
}

func TestContractCoversStrictValidationErrorsAtTheHTTPSeam(t *testing.T) {
	router := newManagedImportTestRouter(t, t.TempDir())
	testCases := []struct {
		name   string
		body   []byte
		code   string
		field  string
		reason string
	}{
		{name: "missing artwork", body: withoutFrontCover(t, readStrictFLACFixture(t)), code: string(library.INSPECTION_ERROR_MISSING_ARTWORK), field: "artwork", reason: "embedded front cover is required"},
		{name: "missing title", body: replaceFixtureTag(t, readStrictFLACFixture(t), "TITLE=  Inspection   Fixture  ", "XITLE=  Inspection   Fixture  "), code: string(library.INSPECTION_ERROR_INVALID_METADATA), field: "TITLE", reason: "required tag is missing"},
		{name: "truncated stream", body: readStrictFLACFixture(t)[:len(readStrictFLACFixture(t))-8], code: string(library.INSPECTION_ERROR_AUDIO_DECODE), field: "audio", reason: "audio stream failed full decode"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobID := createImportJob(t, router)
			response := testutil.ServeContractRequest(t, router, contractUploadRequest(jobID, "application/octet-stream", "candidate.flac", testCase.body, nil))
			failure := testutil.AssertStructuredError(t, response, http.StatusUnprocessableEntity, testCase.code)
			if failure.Field != testCase.field || failure.Reason != testCase.reason {
				t.Fatalf("structured validation error = %+v, want field %q reason %q", failure, testCase.field, testCase.reason)
			}
		})
	}
}

func TestContractCoversBinaryUploadForEveryDocumentedMediaType(t *testing.T) {
	router := newTrackReplacementRouter(t, testutil.OpenMigratedDB(t), t.TempDir())
	contract := testutil.Contract(t)
	uploadBodies := contract.Paths.Find("/api/v1/imports/{importId}/file").Put.RequestBody.Value.Content
	testCases := []struct {
		name      string
		mediaType string
		filename  string
		fixture   []byte
		format    string
	}{
		{name: "flac", mediaType: "audio/flac", filename: "strict.flac", fixture: readStrictFLACFixture(t), format: "flac"},
		{name: "wav", mediaType: "audio/wav", filename: "strict.wav", fixture: buildStrictWAV(t), format: "wav"},
		{name: "mp3", mediaType: "audio/mpeg", filename: "strict.mp3", fixture: testutil.StrictMP3Fixture(), format: "mp3"},
		{name: "m4a aac", mediaType: "audio/mp4", filename: "strict-aac.m4a", fixture: readM4AFixture(t, "strict-import-aac.m4a"), format: "m4a"},
		{name: "m4a alac", mediaType: "audio/mp4", filename: "strict-alac.m4a", fixture: readM4AFixture(t, "strict-import-alac.m4a"), format: "m4a"},
		{name: "ogg vorbis", mediaType: "audio/ogg", filename: "strict.ogg", fixture: readLibraryFixture(t, "strict-import.ogg"), format: "ogg"},
		{name: "opus", mediaType: "audio/opus", filename: "strict.opus", fixture: readLibraryFixture(t, "strict-import.opus"), format: "opus"},
		{name: "sniffed flac", mediaType: "application/octet-stream", filename: "misleading.wav", fixture: secondTrackFixture(readStrictFLACFixture(t)), format: "flac"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, documented := uploadBodies[testCase.mediaType]; !documented {
				t.Fatalf("request media type %s is not documented for uploadManagedImportFile", testCase.mediaType)
			}
			jobID := createImportJob(t, router)
			response := testutil.ServeContractRequest(t, router, contractUploadRequest(jobID, testCase.mediaType, testCase.filename, testCase.fixture, nil))
			preview := decodeContract[contractPreview](t, response, http.StatusOK)
			if preview.File.Format != testCase.format {
				t.Fatalf("detected format = %q, want %q", preview.File.Format, testCase.format)
			}
		})
	}
	for mediaType := range uploadBodies {
		exercised := false
		for _, testCase := range testCases {
			exercised = exercised || testCase.mediaType == mediaType
		}
		if !exercised {
			t.Errorf("documented upload media type %s is not exercised at the HTTP seam", mediaType)
		}
	}
}

func TestContractCoversBatchPerFileStatusAndDuplicateDecisions(t *testing.T) {
	router := newManagedImportTestRouter(t, t.TempDir())
	importOneFLAC(t, router, readStrictFLACFixture(t), "existing.flac")

	batch := decodeContract[contractBatch](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/import-batches"}), http.StatusCreated)
	createJob := func() contractJob {
		body := map[string]string{"batchId": batch.ID, "clientFileId": uuid.NewString()}
		return decodeContract[contractJob](t, testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, "/api/v1/imports", body, nil)), http.StatusCreated)
	}
	possibleDuplicate := createJob()
	rejected := createJob()
	accepted := createJob()

	candidate := replaceFixtureTag(t, readStrictFLACFixture(t), "TITLE=  Inspection   Fixture  ", "TITLE=Inspection       Fixture")
	preview := decodeContract[contractPreview](t, testutil.ServeContractRequest(t, router, contractUploadRequest(possibleDuplicate.ID, "audio/flac", "candidate.flac", candidate, nil)), http.StatusOK)
	if preview.DuplicateClassification != string(managedimport.DUPLICATE_POSSIBLE) {
		t.Fatalf("duplicate classification = %q, want possible duplicate", preview.DuplicateClassification)
	}
	broken := testutil.ServeContractRequest(t, router, contractUploadRequest(rejected.ID, "audio/flac", "broken.flac", withoutFrontCover(t, thirdTrackFixture(readStrictFLACFixture(t))), nil))
	testutil.AssertStructuredError(t, broken, http.StatusUnprocessableEntity, string(library.INSPECTION_ERROR_MISSING_ARTWORK))
	decodeContract[contractPreview](t, testutil.ServeContractRequest(t, router, contractUploadRequest(accepted.ID, "audio/flac", "second.flac", secondTrackFixture(readStrictFLACFixture(t)), nil)), http.StatusOK)

	current := decodeContract[contractBatch](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/import-batches/" + batch.ID}), http.StatusOK)
	states := map[string]string{}
	for _, file := range current.Files {
		states[file.JobID] = file.State
	}
	if states[possibleDuplicate.ID] != "accepted" || states[accepted.ID] != "accepted" || states[rejected.ID] != "rejected" {
		t.Fatalf("per-file batch states = %+v", states)
	}

	confirmPath := "/api/v1/import-batches/" + batch.ID + "/confirm"
	undecided := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, confirmPath, map[string]any{"revision": current.Revision, "selectedFileIds": []string{possibleDuplicate.ID, accepted.ID}}, nil))
	testutil.AssertStructuredError(t, undecided, http.StatusBadRequest, "invalid_upload")

	malformed := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, confirmPath, map[string]any{"revision": 0}, nil))
	testutil.AssertStructuredError(t, malformed, http.StatusBadRequest, "invalid_batch_confirmation")

	confirmation := map[string]any{
		"revision":           current.Revision,
		"selectedFileIds":    []string{possibleDuplicate.ID, accepted.ID},
		"duplicateDecisions": []map[string]string{{"jobId": possibleDuplicate.ID, "action": "import_separately"}},
	}
	completed := decodeContract[contractBatch](t, testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, confirmPath, confirmation, nil)), http.StatusOK)
	outcomes := map[string]string{}
	for _, file := range completed.Files {
		outcomes[file.JobID] = file.Outcome
	}
	if completed.Status != "completed" || outcomes[possibleDuplicate.ID] != "imported" || outcomes[accepted.ID] != "imported" || outcomes[rejected.ID] != "rejected" {
		t.Fatalf("confirmed batch = %+v", completed)
	}

	replayed := decodeContract[contractBatch](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/import-batches/" + batch.ID}), http.StatusOK)
	if replayed.Status != "completed" {
		t.Fatalf("completed batch status = %q", replayed.Status)
	}

	abandoned := decodeContract[contractBatch](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/import-batches"}), http.StatusCreated)
	canceled := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodDelete, Path: "/api/v1/import-batches/" + abandoned.ID})
	if canceled.Code != http.StatusNoContent {
		t.Fatalf("cancel batch status = %d, body = %s", canceled.Code, canceled.Body.String())
	}
	missing := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/import-batches/" + uuid.NewString()})
	testutil.AssertStructuredError(t, missing, http.StatusNotFound, "import_not_found")
}

func TestContractCoversTrackReplacementAndPermanentDeletion(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	router := newTrackReplacementRouter(t, database, t.TempDir())
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	replacement := replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Ambient")

	unknownTrack := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library/tracks/" + uuid.NewString() + "/replacement"})
	testutil.AssertStructuredError(t, unknownTrack, http.StatusNotFound, "track_not_found")

	job := decodeContract[contractJob](t, testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library/tracks/" + trackID + "/replacement"}), http.StatusCreated)
	if job.ReplacesTrackID != trackID {
		t.Fatalf("Track Replacement job = %+v", job)
	}
	preview := decodeContract[contractPreview](t, testutil.ServeContractRequest(t, router, contractUploadRequest(job.ID, "audio/flac", "better.flac", replacement, nil)), http.StatusOK)
	if preview.Replacement == nil || preview.Replacement.ConfirmationToken == "" {
		t.Fatalf("replacement Import Preview = %+v", preview)
	}

	replacementPath := "/api/v1/imports/" + job.ID + "/replacement"
	confirmationBody := map[string]any{"revision": preview.Revision, "confirmationToken": preview.Replacement.ConfirmationToken}
	plain := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", map[string]any{"revision": preview.Revision}, nil))
	testutil.AssertStructuredError(t, plain, http.StatusConflict, "track_replacement_required")
	unconfirmed := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, replacementPath, confirmationBody, nil))
	testutil.AssertStructuredError(t, unconfirmed, http.StatusForbidden, "track_replacement_forbidden")
	staleToken := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, replacementPath, map[string]any{"revision": preview.Revision, "confirmationToken": "stale"}, map[string]string{managedimport.TRACK_REPLACEMENT_CONFIRMATION_HEADER: "1"}))
	testutil.AssertStructuredError(t, staleToken, http.StatusConflict, "replacement_preview_changed")

	confirmed := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodPost, replacementPath, confirmationBody, map[string]string{managedimport.TRACK_REPLACEMENT_CONFIRMATION_HEADER: "1"}))
	var result struct {
		TrackID      string `json:"trackId"`
		Status       string `json:"status"`
		DeletedFiles int    `json:"deletedFiles"`
	}
	testutil.DecodeJSON(t, confirmed, &result)
	if confirmed.Code != http.StatusOK || result.TrackID != trackID || result.Status != string(managedimport.STATUS_COMMITTED) || result.DeletedFiles != 1 {
		t.Fatalf("Track Replacement result = %d %+v", confirmed.Code, result)
	}

	deletionPath := "/api/v1/library/tracks/" + trackID
	unknownDeletion := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/library/tracks/" + uuid.NewString() + "/deletion"})
	testutil.AssertStructuredError(t, unknownDeletion, http.StatusNotFound, "track_not_found")
	deletionPreview := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: deletionPath + "/deletion"})
	var deletion struct {
		ConfirmationToken string `json:"confirmationToken"`
		TrackTitle        string `json:"trackTitle"`
	}
	testutil.DecodeJSON(t, deletionPreview, &deletion)
	if deletionPreview.Code != http.StatusOK || deletion.ConfirmationToken == "" || deletion.TrackTitle == "" {
		t.Fatalf("Permanent Track Deletion preview = %d %+v", deletionPreview.Code, deletion)
	}
	deleteBody := map[string]string{"confirmationToken": deletion.ConfirmationToken}
	forbidden := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodDelete, deletionPath, deleteBody, nil))
	testutil.AssertStructuredError(t, forbidden, http.StatusForbidden, "permanent_deletion_forbidden")
	changed := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodDelete, deletionPath, map[string]string{"confirmationToken": "stale"}, map[string]string{"X-Permanent-Delete": "1"}))
	testutil.AssertStructuredError(t, changed, http.StatusConflict, "deletion_preview_changed")
	deleted := testutil.ServeContractRequest(t, router, contractJSONRequest(http.MethodDelete, deletionPath, deleteBody, map[string]string{"X-Permanent-Delete": "1"}))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	gone := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: deletionPath})
	if gone.Code != http.StatusNotFound {
		t.Fatalf("deleted Track lookup status = %d, body = %s", gone.Code, gone.Body.String())
	}
}

func readLibraryFixture(t *testing.T, name string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "library", "testdata", name))
	if err != nil {
		t.Fatalf("read %s fixture: %v", name, err)
	}
	return fixture
}

var _ = fmt.Sprintf
