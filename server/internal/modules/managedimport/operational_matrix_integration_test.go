package managedimport_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

// These tests are the operational recovery matrix for Managed Import (issue #56). Each test drives the
// versioned HTTP seam and asserts only externally observable outcomes: response codes, library reads, and
// the exact contents of Managed Storage after the operation.

const (
	PNG_IHDR_WIDTH_OFFSET     = 16
	PNG_IHDR_HEIGHT_OFFSET    = 20
	PNG_IHDR_CRC_OFFSET       = 29
	PNG_IHDR_CRC_INPUT_OFFSET = 12
	PNG_CHUNK_FIELD_SIZE      = 4
	// OVERSIZED_ARTWORK_* declare a decoded size just above the 50-megapixel limit so the image bomb is
	// rejected by dimension validation before any pixel buffer is allocated.
	OVERSIZED_ARTWORK_WIDTH  = 10_000
	OVERSIZED_ARTWORK_HEIGHT = 5_001
)

func TestManagedImportRejectsStaleRevisionWithoutMutation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newManagedImportTestRouterWithDatabase(t, database, managedStoragePath)
	fixture := readStrictFLACFixture(t)
	jobID, revision := uploadFLACForPreview(t, router, fixture, "strict-import.flac")

	stale := serveImportConfirmation(router, jobID, revision-1)
	testutil.AssertErrorCode(t, stale, http.StatusConflict, managedimport.ERROR_CODE_REVISION_CONFLICT)
	future := serveImportConfirmation(router, jobID, revision+1)
	testutil.AssertErrorCode(t, future, http.StatusConflict, managedimport.ERROR_CODE_REVISION_CONFLICT)

	assertNoLibraryEntities(t, database)
	assertNoCanonicalFilesInStorage(t, managedStoragePath)
	assertStagedStorage(t, managedStoragePath, jobID, fixture)
	if job := getManagedImportJob(t, router, jobID); job.Status != string(managedimport.STATUS_AWAITING_CONFIRMATION) {
		t.Fatalf("job status after stale confirmations = %q", job.Status)
	}
	if importOneFLACFromPreview(t, router, jobID, revision) == "" {
		t.Fatal("current revision should still commit after stale confirmations")
	}
}

func TestManagedImportBatchRejectsStaleRevisionWithoutMutation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newManagedImportTestRouterWithDatabase(t, database, managedStoragePath)
	fixture := readStrictFLACFixture(t)
	batchID := testutil.CreateResourceID(t, router, "/api/v1/import-batches")
	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", strings.NewReader(fmt.Sprintf(`{"batchId":%q,"clientFileId":"00000000-0000-4000-8000-000000000056"}`, batchID)), map[string]string{"Content-Type": "application/json"})
	var job managedimport.Job
	testutil.DecodeJSON(t, jobResponse, &job)
	uploadFLACToJob(t, router, job.ID, fixture, "strict-import.flac")
	var batch managedimport.Batch
	testutil.DecodeJSON(t, testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/import-batches/"+batchID, nil, nil), &batch)

	stale := confirmBatchFile(t, router, batchID, batch.Revision-1, job.ID)
	testutil.AssertErrorCode(t, stale, http.StatusConflict, managedimport.ERROR_CODE_REVISION_CONFLICT)

	assertNoLibraryEntities(t, database)
	assertNoCanonicalFilesInStorage(t, managedStoragePath)
	assertStagedStorage(t, managedStoragePath, job.ID, fixture)
	current := confirmBatchFile(t, router, batchID, batch.Revision, job.ID)
	if current.Code != http.StatusOK {
		t.Fatalf("current revision confirm status = %d, body = %s", current.Code, current.Body.String())
	}
	assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
}

func TestManagedImportStandaloneCancellationRemovesStagingAndRecordsHistory(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newManagedImportTestRouterWithDatabase(t, database, managedStoragePath)
	fixture := readStrictFLACFixture(t)
	jobID, _ := uploadFLACForPreview(t, router, fixture, "cancel-me.flac")
	assertStagedStorage(t, managedStoragePath, jobID, fixture)

	canceled := testutil.ServeRequest(t, router, http.MethodDelete, "/api/v1/imports/"+jobID, nil, nil)

	if canceled.Code != http.StatusNoContent {
		t.Fatalf("cancel awaiting standalone job status = %d, body = %s", canceled.Code, canceled.Body.String())
	}
	assertNoLibraryEntities(t, database)
	assertNoStorageResidue(t, managedStoragePath)
	if missing := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/imports/"+jobID, nil, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("canceled job status = %d, body = %s", missing.Code, missing.Body.String())
	}
	var history struct {
		Items []managedimport.HistoryItem `json:"items"`
	}
	testutil.DecodeJSON(t, testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/import-history", nil, nil), &history)
	if len(history.Items) != 1 || history.Items[0].ImportID != jobID || history.Items[0].ResultCode != "canceled" {
		t.Fatalf("Import History after cancellation = %+v", history.Items)
	}
}

func TestManagedImportRejectsCorruptStreamsWithoutResidue(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		corrupt      func(t *testing.T, fixture []byte) []byte
		expectedCode library.InspectionErrorCode
	}{
		{name: "corrupt stream head", expectedCode: library.INSPECTION_ERROR_UNSUPPORTED_FORMAT, corrupt: func(_ *testing.T, fixture []byte) []byte {
			result := append([]byte(nil), fixture...)
			copy(result, "fLaX")
			return result
		}},
		{name: "corrupt stream middle", expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, corrupt: func(t *testing.T, fixture []byte) []byte {
			result := append([]byte(nil), fixture...)
			audioOffset := flacAudioOffset(t, result)
			middle := audioOffset + (len(result)-audioOffset)/2
			for index := middle; index < middle+64 && index < len(result); index++ {
				result[index] ^= 0xff
			}
			return result
		}},
		{name: "truncated stream end", expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, corrupt: func(_ *testing.T, fixture []byte) []byte {
			return append([]byte(nil), fixture[:len(fixture)-8]...)
		}},
		{name: "image bomb artwork", expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK, corrupt: func(t *testing.T, fixture []byte) []byte {
			return replaceFrontCover(t, fixture, oversizedPNGArtwork(t, embeddedFrontCover(t, fixture)))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			managedStoragePath := t.TempDir()
			router := newManagedImportTestRouterWithDatabase(t, database, managedStoragePath)
			jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
			corrupted := testCase.corrupt(t, readStrictFLACFixture(t))

			response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(corrupted), map[string]string{
				"Content-Type": "audio/flac", "X-Import-Filename": "corrupt.flac",
			})

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("corrupt upload status = %d, body = %s", response.Code, response.Body.String())
			}
			var failure struct {
				Code string `json:"code"`
			}
			testutil.DecodeJSON(t, response, &failure)
			if failure.Code != string(testCase.expectedCode) {
				t.Fatalf("rejection code = %q, want %q", failure.Code, testCase.expectedCode)
			}
			if job := getManagedImportJob(t, router, jobID); job.Status != string(managedimport.STATUS_FAILED) {
				t.Fatalf("job status after rejection = %q", job.Status)
			}
			assertNoLibraryEntities(t, database)
			assertNoStorageResidue(t, managedStoragePath)
		})
	}
}

func TestManagedImportRejectsPathAttackFilenamesWithoutResidue(t *testing.T) {
	for _, filename := range []string{
		"../strict-import.flac",
		"/etc/strict-import.flac",
		`..\strict-import.flac`,
		"nested/strict-import.flac",
		"strict\x00import.flac",
		".",
		"",
	} {
		t.Run(fmt.Sprintf("%q", filename), func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			managedStoragePath := t.TempDir()
			router := newManagedImportTestRouterWithDatabase(t, database, managedStoragePath)
			jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

			response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStrictFLACFixture(t)), map[string]string{
				"Content-Type": "audio/flac", "X-Import-Filename": filename,
			})

			testutil.AssertErrorCode(t, response, http.StatusBadRequest, "invalid_upload")
			assertNoLibraryEntities(t, database)
			assertNoStorageResidue(t, managedStoragePath)
			if _, err := os.Stat(filepath.Join(managedStoragePath, "library")); !os.IsNotExist(err) {
				t.Fatalf("canonical library directory should not exist after rejected filename: %v", err)
			}
		})
	}
}

func TestManagedImportUploadLimitViolationsLeaveNoResidue(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		fileLimit     int64
		batchLimit    int64
		contentLength int64
		expectedCode  string
	}{
		{name: "declared length over file limit", fileLimit: 1024, batchLimit: 4096, contentLength: 2048, expectedCode: "upload_too_large"},
		{name: "undeclared length over file limit", fileLimit: 1024, batchLimit: 4096, contentLength: -1, expectedCode: "upload_too_large"},
		{name: "false length over file limit", fileLimit: 1024, batchLimit: 4096, contentLength: 512, expectedCode: "upload_too_large"},
		{name: "declared length over batch limit", fileLimit: 4096, batchLimit: 1024, contentLength: 2048, expectedCode: "batch_upload_too_large"},
		{name: "undeclared length over batch limit", fileLimit: 4096, batchLimit: 1024, contentLength: -1, expectedCode: "batch_upload_too_large"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			managedStoragePath := t.TempDir()
			module := managedimport.NewModule(database, config.Config{
				ManagedStoragePath:           managedStoragePath,
				ManagedImportFileLimitBytes:  testCase.fileLimit,
				ManagedImportBatchLimitBytes: testCase.batchLimit,
			}, library.NewMediaInspector())
			router := chi.NewRouter()
			module.RegisterRoutes(router)
			jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
			request := httptest.NewRequest(http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(make([]byte, 2048)))
			request.ContentLength = testCase.contentLength
			request.Header.Set("Content-Type", "audio/flac")
			request.Header.Set("X-Import-Filename", "oversized.flac")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			testutil.AssertErrorCode(t, response, http.StatusRequestEntityTooLarge, testCase.expectedCode)
			assertNoLibraryEntities(t, database)
			assertNoStorageResidue(t, managedStoragePath)
			// A limit violation is a transport-level refusal: the partial upload is discarded and the job
			// stays in the uploading state so the client can retry with a compliant file.
			if job := getManagedImportJob(t, router, jobID); job.Status != string(managedimport.STATUS_UPLOADING) {
				t.Fatalf("job status after limit violation = %q, want %q", job.Status, managedimport.STATUS_UPLOADING)
			}
		})
	}
}

func TestTrackReplacementAndPermanentDeletionRaceIsDeterministic(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	replacement := replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Ambient")
	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "better.flac")
	if preview.Replacement == nil {
		t.Fatalf("replacement Import Preview = %+v", preview)
	}
	deletionPreview := previewTrackDeletion(t, router, trackID)
	deletionBody, err := json.Marshal(managedimport.TrackDeletionConfirmation{ConfirmationToken: deletionPreview.ConfirmationToken})
	if err != nil {
		t.Fatal(err)
	}

	var replaced, deleted *httptest.ResponseRecorder
	var group sync.WaitGroup
	start := make(chan struct{})
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		replaced = serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	}()
	go func() {
		defer group.Done()
		<-start
		deleted = performTrackDeletionRequest(t, router, http.MethodDelete, "/api/v1/library/tracks/"+trackID, deletionBody, true)
	}()
	close(start)
	group.Wait()

	switch {
	case replaced.Code == http.StatusOK && deleted.Code == http.StatusConflict:
		testutil.AssertErrorCode(t, deleted, http.StatusConflict, "deletion_preview_changed")
		assertStreamedBytes(t, router, trackID, replacement)
		assertCanonicalStorage(t, managedStoragePath, replacement)
		assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
		assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = ?`, 1, trackID)
	case deleted.Code == http.StatusOK && replaced.Code != http.StatusOK:
		testutil.AssertErrorCode(t, replaced, http.StatusNotFound, "track_not_found")
		assertCanonicalFileCounts(t, managedStoragePath, 0, 0)
		assertDeletionCount(t, database, `SELECT COUNT(*) FROM tracks WHERE id = ?`, 0, trackID)
		if stream := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+trackID+"/stream", nil, nil); stream.Code != http.StatusNotFound {
			t.Fatalf("deleted Track stream status = %d", stream.Code)
		}
		// The orphaned replacement job keeps its staged bytes until the client cancels it or inactivity
		// cleanup runs; explicit cancellation must remove them.
		if canceled := testutil.ServeRequest(t, router, http.MethodDelete, "/api/v1/imports/"+job.ID, nil, nil); canceled.Code != http.StatusNoContent {
			t.Fatalf("cancel orphaned replacement job status = %d, body = %s", canceled.Code, canceled.Body.String())
		}
	default:
		t.Fatalf("race outcome: replacement = %d (%s), deletion = %d (%s)", replaced.Code, replaced.Body.String(), deleted.Code, deleted.Body.String())
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM permanent_track_deletions`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_track_replacements WHERE phase NOT IN ('completed', 'rolled_back')`, 0)
	assertNoStagingResidue(t, managedStoragePath)
}

func TestTrackReplacementConcurrentJobsForOneTrackAreSerialized(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	candidates := [][]byte{
		replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Ambient"),
		replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Downtempo"),
	}
	previews := make([]managedimport.Preview, len(candidates))
	jobIDs := make([]string, len(candidates))
	for index, candidate := range candidates {
		job := createTrackReplacementJob(t, router, trackID)
		jobIDs[index] = job.ID
		previews[index] = uploadFLACToJob(t, router, job.ID, candidate, fmt.Sprintf("candidate-%d.flac", index))
		if previews[index].Replacement == nil {
			t.Fatalf("candidate %d Import Preview = %+v", index, previews[index])
		}
	}

	responses := make([]*httptest.ResponseRecorder, len(candidates))
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := range candidates {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			responses[index] = serveReplacementConfirmation(router, jobIDs[index], previews[index].Revision, previews[index].Replacement.ConfirmationToken, true)
		}(index)
	}
	close(start)
	group.Wait()

	winner := -1
	for index, response := range responses {
		switch response.Code {
		case http.StatusOK:
			if winner != -1 {
				t.Fatalf("both concurrent replacements committed: %s / %s", responses[0].Body.String(), responses[1].Body.String())
			}
			winner = index
		case http.StatusConflict:
			testutil.AssertErrorCode(t, response, http.StatusConflict, "replacement_preview_changed")
		default:
			t.Fatalf("candidate %d status = %d, body = %s", index, response.Code, response.Body.String())
		}
	}
	if winner == -1 {
		t.Fatalf("no concurrent replacement committed: %s / %s", responses[0].Body.String(), responses[1].Body.String())
	}
	assertStreamedBytes(t, router, trackID, candidates[winner])
	assertCanonicalStorage(t, managedStoragePath, candidates[winner])
	assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != trackID {
		t.Fatalf("Tracks after concurrent replacement = %+v", tracks.Items)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_track_replacements WHERE phase NOT IN ('completed', 'rolled_back')`, 0)

	loser := 1 - winner
	retried := serveReplacementConfirmation(router, jobIDs[loser], previews[loser].Revision, previews[loser].Replacement.ConfirmationToken, true)
	testutil.AssertErrorCode(t, retried, http.StatusConflict, "replacement_preview_changed")
	assertStreamedBytes(t, router, trackID, candidates[winner])
}

func confirmBatchFile(t *testing.T, router http.Handler, batchID string, revision int, fileID string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"revision":%d,"selectedFileIds":[%q]}`, revision, fileID))
	return testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/import-batches/"+batchID+"/confirm", body, map[string]string{"Content-Type": "application/json"})
}

func importOneFLACFromPreview(t *testing.T, router http.Handler, jobID string, revision int) string {
	t.Helper()
	response := serveImportConfirmation(router, jobID, revision)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		TrackID string `json:"trackId"`
	}
	testutil.DecodeJSON(t, response, &result)
	return result.TrackID
}

func previewTrackDeletion(t *testing.T, router http.Handler, trackID string) managedimport.TrackDeletionPreview {
	t.Helper()
	response := performTrackDeletionRequest(t, router, http.MethodGet, "/api/v1/library/tracks/"+trackID+"/deletion", nil, false)
	if response.Code != http.StatusOK {
		t.Fatalf("deletion preview status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview managedimport.TrackDeletionPreview
	testutil.DecodeJSON(t, response, &preview)
	return preview
}

// flacAudioOffset returns the byte offset of the first audio frame, i.e. the end of the last metadata block.
func flacAudioOffset(t *testing.T, fixture []byte) int {
	t.Helper()
	offset := library.FLAC_SIGNATURE_SIZE_BYTES
	for offset+library.FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES <= len(fixture) {
		header := fixture[offset]
		bodyEnd := offset + library.FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES + flacMetadataBlockLength(fixture, offset)
		if bodyEnd > len(fixture) {
			t.Fatal("strict FLAC fixture metadata is truncated")
		}
		offset = bodyEnd
		if header&FLAC_METADATA_BLOCK_LAST_FLAG != 0 {
			return offset
		}
	}
	t.Fatal("strict FLAC fixture has no last metadata block")
	return 0
}

func embeddedFrontCover(t *testing.T, fixture []byte) []byte {
	t.Helper()
	_, bodyOffset, bodyEnd := findFLACPictureBlock(t, fixture)
	body := fixture[bodyOffset:bodyEnd]
	dataOffset := pictureDataLengthOffset(t, body) + FLAC_PICTURE_FIELD_SIZE_BYTES
	return append([]byte(nil), body[dataOffset:]...)
}

// oversizedPNGArtwork rewrites the IHDR dimensions of a valid PNG so it claims a decoded size above the
// artwork pixel limit while keeping a valid chunk checksum, mirroring a real decompression bomb header.
func oversizedPNGArtwork(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < PNG_IHDR_CRC_OFFSET+PNG_CHUNK_FIELD_SIZE || string(data[PNG_IHDR_CRC_INPUT_OFFSET:PNG_IHDR_WIDTH_OFFSET]) != "IHDR" {
		t.Fatal("embedded front cover is not a PNG with an IHDR chunk")
	}
	result := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(result[PNG_IHDR_WIDTH_OFFSET:PNG_IHDR_HEIGHT_OFFSET], OVERSIZED_ARTWORK_WIDTH)
	binary.BigEndian.PutUint32(result[PNG_IHDR_HEIGHT_OFFSET:PNG_IHDR_HEIGHT_OFFSET+PNG_CHUNK_FIELD_SIZE], OVERSIZED_ARTWORK_HEIGHT)
	binary.BigEndian.PutUint32(result[PNG_IHDR_CRC_OFFSET:PNG_IHDR_CRC_OFFSET+PNG_CHUNK_FIELD_SIZE], crc32.ChecksumIEEE(result[PNG_IHDR_CRC_INPUT_OFFSET:PNG_IHDR_CRC_OFFSET]))
	return result
}

// assertNoStorageResidue proves a rejected operation left neither staged bytes nor canonical files behind.
func assertNoStorageResidue(t *testing.T, managedStoragePath string) {
	t.Helper()
	assertNoStagingResidue(t, managedStoragePath)
	assertNoCanonicalFilesInStorage(t, managedStoragePath)
}

func assertNoStagingResidue(t *testing.T, managedStoragePath string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(managedStoragePath, ".staging"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read staging directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging residue remains: %v", entryNames(entries))
	}
}

func assertNoCanonicalFilesInStorage(t *testing.T, managedStoragePath string) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(managedStoragePath, "library"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			t.Fatalf("canonical residue remains at %q", path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk canonical library: %v", err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
