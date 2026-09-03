package managedimport_test

import (
	"bytes"
	"database/sql"
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

func TestTrackReplacementPreservesIdentityAndReferences(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	seedDeletionReferences(t, database, trackID)
	replacement := replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Ambient")

	job := createTrackReplacementJob(t, router, trackID)
	if job.ReplacesTrackID != trackID || job.Status != managedimport.STATUS_UPLOADING {
		t.Fatalf("Track Replacement job = %+v", job)
	}
	preview := uploadFLACToJob(t, router, job.ID, replacement, "better.flac")
	if preview.Status != managedimport.STATUS_AWAITING_CONFIRMATION || preview.Replacement == nil {
		t.Fatalf("replacement Import Preview = %+v", preview)
	}
	assertReplacementPreviewDifferences(t, preview.Replacement, trackID)

	plainConfirm := serveImportConfirmation(router, job.ID, preview.Revision)
	testutil.AssertErrorCode(t, plainConfirm, http.StatusConflict, "track_replacement_required")
	unconfirmed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, false)
	testutil.AssertErrorCode(t, unconfirmed, http.StatusForbidden, "track_replacement_forbidden")
	staleToken := serveReplacementConfirmation(router, job.ID, preview.Revision, "stale-token", true)
	testutil.AssertErrorCode(t, staleToken, http.StatusConflict, "replacement_preview_changed")
	assertStreamedBytes(t, router, trackID, original)

	confirmed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("replacement confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	var result managedimport.TrackReplacementResult
	testutil.DecodeJSON(t, confirmed, &result)
	if result.TrackID != trackID || result.Status != managedimport.STATUS_COMMITTED || result.DeletedFiles != 1 || result.Revision != preview.Revision+1 {
		t.Fatalf("Track Replacement result = %+v", result)
	}
	assertReplacedTrack(t, router, trackID, "Ambient")
	assertStreamedBytes(t, router, trackID, replacement)
	assertCanonicalStorage(t, managedStoragePath, replacement)
	assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playlist_tracks WHERE track_id = ?`, 1, trackID)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM playback_queue WHERE track_id = ?`, 1, trackID)
	assertDeletionCount(t, database, `SELECT revision FROM playback_queue_state WHERE user_id = 'user-1'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_track_replacements WHERE track_id = ? AND phase = 'completed'`, 1, trackID)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_import_history_files WHERE replaced_track_id = ? AND result_code = 'replaced'`, 1, trackID)

	replayed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	if replayed.Code != http.StatusOK {
		t.Fatalf("idempotent replacement status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	var replayedResult managedimport.TrackReplacementResult
	testutil.DecodeJSON(t, replayed, &replayedResult)
	if replayedResult.TrackID != trackID || replayedResult.DeletedFiles != 1 {
		t.Fatalf("idempotent Track Replacement result = %+v", replayedResult)
	}
}

func TestTrackReplacementMovesAlbumAndCleansEmptyEntities(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	originalTrack := listTracks(t, router).Items[0]
	replacement := replaceFixtureTag(t, original, "ALBUM=Strict Import Tests", "ALBUM=Other Import Tests")
	replacement = replaceFixtureTag(t, replacement, "ALBUMARTIST=Test Album Artist", "ALBUMARTIST=New Album Artist")

	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "moved.flac")
	if preview.Replacement == nil {
		t.Fatalf("replacement Import Preview = %+v", preview)
	}
	change := preview.Replacement.Library
	if !change.MovesAlbum || !change.CreatesAlbum || !change.RemovesEmptyAlbum || change.CurrentAlbumID != originalTrack.AlbumID {
		t.Fatalf("library change = %+v", change)
	}
	if strings.Join(change.RemovesEmptyArtists, ",") != "Test Album Artist" || strings.Join(change.CreatesArtists, ",") != "New Album Artist" {
		t.Fatalf("Artist changes = %+v", change)
	}
	if !preview.Replacement.CanonicalPath.IsChanged || preview.Replacement.OldFile.Path != preview.Replacement.CanonicalPath.Current {
		t.Fatalf("canonical path change = %+v", preview.Replacement.CanonicalPath)
	}

	confirmed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("replacement confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	var result managedimport.TrackReplacementResult
	testutil.DecodeJSON(t, confirmed, &result)
	if result.TrackID != trackID || result.DeletedFiles != 2 {
		t.Fatalf("Track Replacement result = %+v", result)
	}
	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != trackID || tracks.Items[0].AlbumTitle != "Other Import Tests" || tracks.Items[0].AlbumID == originalTrack.AlbumID {
		t.Fatalf("replaced Track = %+v", tracks.Items)
	}
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM albums WHERE id = ?`, 0, originalTrack.AlbumID)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE name = 'Test Album Artist'`, 0)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE name = 'Test Artist'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM artists WHERE name = 'New Album Artist'`, 1)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM album_artwork WHERE source_track_id = ?`, 1, trackID)
	assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
	if _, err := os.Stat(filepath.Join(managedStoragePath, "library", filepath.Base(filepath.Dir(filepath.Dir(preview.Replacement.OldFile.Path))))); !os.IsNotExist(err) {
		t.Fatalf("emptied Album Artist directory should be removed: %v", err)
	}
	assertStreamedBytes(t, router, trackID, replacement)
}

func TestTrackReplacementRejectsExactDuplicateAndOccupiedPosition(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	second := secondTrackFixture(original)
	importOneFLAC(t, router, second, "second.flac")

	sameBytesJob := createTrackReplacementJob(t, router, trackID)
	sameBytes := uploadFixtureToJob(t, router, sameBytesJob.ID, original)
	if sameBytes.Code != http.StatusOK {
		t.Fatalf("same-bytes upload status = %d, body = %s", sameBytes.Code, sameBytes.Body.String())
	}
	var sameBytesPreview managedimport.Preview
	testutil.DecodeJSON(t, sameBytes, &sameBytesPreview)
	if sameBytesPreview.Status != managedimport.STATUS_FAILED || sameBytesPreview.DuplicateClassification != managedimport.DUPLICATE_EXACT || sameBytesPreview.Replacement != nil {
		t.Fatalf("same-bytes preview = %+v", sameBytesPreview)
	}
	if len(sameBytesPreview.DuplicateCandidates) != 1 || sameBytesPreview.DuplicateCandidates[0].TrackID != trackID {
		t.Fatalf("same-bytes candidates = %+v", sameBytesPreview.DuplicateCandidates)
	}
	sameBytesConfirm := serveReplacementConfirmation(router, sameBytesJob.ID, sameBytesPreview.Revision, "any-token", true)
	testutil.AssertErrorCode(t, sameBytesConfirm, http.StatusConflict, managedimport.ERROR_CODE_EXACT_DUPLICATE)

	occupiedJob := createTrackReplacementJob(t, router, trackID)
	occupied := uploadFixtureToJob(t, router, occupiedJob.ID, replaceFixtureTag(t, second, "GENRE=Electronic", "GENRE=Ambient"))
	testutil.AssertErrorCode(t, occupied, http.StatusUnprocessableEntity, managedimport.ERROR_CODE_ALBUM_POSITION_CONFLICT)
	if getManagedImportJob(t, router, occupiedJob.ID).Status != string(managedimport.STATUS_FAILED) {
		t.Fatal("occupied-position replacement should fail the Import Job")
	}

	legacyResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library/tracks/missing-track/replacement", nil, nil)
	testutil.AssertErrorCode(t, legacyResponse, http.StatusNotFound, "track_not_found")
	assertStreamedBytes(t, router, trackID, original)
	assertCanonicalFileCounts(t, managedStoragePath, 2, 1)
}

func TestTrackReplacementAcceptsCorrectedPositionTotalsForTheTargetTrack(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	replacement := replaceFixtureTag(t, original, "TRACKNUMBER=3/9", "TRACKNUMBER=3/8")

	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "corrected.flac")
	if preview.Status != managedimport.STATUS_AWAITING_CONFIRMATION || preview.Replacement == nil {
		t.Fatalf("corrected totals preview = %+v", preview)
	}
	trackTotal := findFieldDiff(t, preview.Replacement.Metadata, "trackTotal")
	if !trackTotal.IsChanged || trackTotal.Current != "9" || trackTotal.Replacement != "8" {
		t.Fatalf("trackTotal diff = %+v", trackTotal)
	}
	confirmed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("replacement confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
}

func TestTrackReplacementRejectsPreviewWhenSiblingRemovalChangesCleanup(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	siblingID := importOneFLAC(t, router, secondTrackFixture(original), "second.flac")
	replacement := replaceFixtureTag(t, original, "ALBUM=Strict Import Tests", "ALBUM=Other Import Tests")

	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "moved.flac")
	if preview.Replacement == nil || preview.Replacement.Library.RemovesEmptyAlbum {
		t.Fatalf("preview with a sibling should keep the Album: %+v", preview.Replacement)
	}
	if _, err := database.Exec(`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, siblingID); err != nil {
		t.Fatal(err)
	}

	conflict := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	testutil.AssertErrorCode(t, conflict, http.StatusConflict, "replacement_preview_changed")
	assertStreamedBytes(t, router, trackID, original)
	assertCanonicalFileCounts(t, managedStoragePath, 2, 1)
}

func TestTrackReplacementRollsBackDatabaseFailureWithoutTouchingOldTrack(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	replacement := replaceFixtureTag(t, original, "GENRE=Electronic", "GENRE=Ambient")
	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "better.flac")
	if _, err := database.Exec(`UPDATE tracks SET revision = revision + 1 WHERE id = ?`, trackID); err != nil {
		t.Fatal(err)
	}

	conflict := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	testutil.AssertErrorCode(t, conflict, http.StatusConflict, "replacement_preview_changed")
	assertStreamedBytes(t, router, trackID, original)
	assertCanonicalStorage(t, managedStoragePath, original)
	assertDeletionCount(t, database, `SELECT COUNT(*) FROM managed_track_replacements`, 0)
	if getManagedImportJob(t, router, job.ID).Status != string(managedimport.STATUS_AWAITING_CONFIRMATION) {
		t.Fatal("stale replacement should keep the Import Job reviewable")
	}
}

func TestTrackReplacementReplacesSoleTrackAlbumArtwork(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	alternateArtwork := encodeAlternatePNG(t)
	replacement := replaceFrontCover(t, original, alternateArtwork)

	job := createTrackReplacementJob(t, router, trackID)
	preview := uploadFLACToJob(t, router, job.ID, replacement, "new-cover.flac")
	if preview.Replacement == nil || !preview.Replacement.Artwork.IsChanged || !preview.Replacement.Artwork.ReplacesAlbumArtwork {
		t.Fatalf("artwork replacement preview = %+v", preview.Replacement)
	}
	confirmed := serveReplacementConfirmation(router, job.ID, preview.Revision, preview.Replacement.ConfirmationToken, true)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("replacement confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	var result managedimport.TrackReplacementResult
	testutil.DecodeJSON(t, confirmed, &result)
	if result.DeletedFiles != 2 {
		t.Fatalf("deleted files = %d, want previous audio and artwork", result.DeletedFiles)
	}

	var artworkPath, artworkSHA256 string
	if err := database.QueryRow(`SELECT file_path, content_sha256 FROM album_artwork WHERE source_track_id = ?`, trackID).Scan(&artworkPath, &artworkSHA256); err != nil {
		t.Fatalf("read replaced Album Artwork: %v", err)
	}
	stored, err := os.ReadFile(artworkPath)
	if err != nil || !bytes.Equal(stored, alternateArtwork) {
		t.Fatalf("replaced Album Artwork bytes differ (error = %v)", err)
	}
	if artworkSHA256 != preview.Replacement.Artwork.ReplacementSHA256 {
		t.Fatalf("Album Artwork hash = %s, want %s", artworkSHA256, preview.Replacement.Artwork.ReplacementSHA256)
	}
	assertCanonicalFileCounts(t, managedStoragePath, 1, 1)
	assertStreamedBytes(t, router, trackID, replacement)
}

func TestTrackReplacementRejectsConflictingArtworkForSharedAlbum(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	router := newTrackReplacementRouter(t, database, managedStoragePath)
	original := readStrictFLACFixture(t)
	trackID := importOneFLAC(t, router, original, "original.flac")
	importOneFLAC(t, router, secondTrackFixture(original), "second.flac")
	replacement := replaceFrontCover(t, original, encodeAlternatePNG(t))

	job := createTrackReplacementJob(t, router, trackID)
	response := uploadFixtureToJob(t, router, job.ID, replacement)
	testutil.AssertErrorCode(t, response, http.StatusUnprocessableEntity, managedimport.ERROR_CODE_ALBUM_ARTWORK_CONFLICT)
	assertStreamedBytes(t, router, trackID, original)
	assertCanonicalFileCounts(t, managedStoragePath, 2, 1)
}

func newTrackReplacementRouter(t *testing.T, database *sql.DB, managedStoragePath string) http.Handler {
	t.Helper()
	configuration := config.Config{ManagedStoragePath: managedStoragePath, MusicPaths: []string{t.TempDir()}}
	libraryModule := library.NewModule(database, configuration)
	queueEvents := playback.NewQueueEventBroker()
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector(), queueEvents)
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess(), queueEvents)
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)
	return router
}

func createTrackReplacementJob(t *testing.T, router http.Handler, trackID string) managedimport.Job {
	t.Helper()
	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library/tracks/"+trackID+"/replacement", nil, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Track Replacement status = %d, body = %s", response.Code, response.Body.String())
	}
	var job managedimport.Job
	testutil.DecodeJSON(t, response, &job)
	return job
}

func uploadFixtureToJob(t *testing.T, router http.Handler, jobID string, fixture []byte) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type": "audio/flac", "X-Import-Filename": "replacement.flac",
	})
}

func serveReplacementConfirmation(router http.Handler, jobID string, revision int, token string, confirm bool) *httptest.ResponseRecorder {
	body := strings.NewReader(fmt.Sprintf(`{"revision":%d,"confirmationToken":%q}`, revision, token))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/"+jobID+"/replacement", body)
	request.Header.Set("Content-Type", "application/json")
	if confirm {
		request.Header.Set(managedimport.TRACK_REPLACEMENT_CONFIRMATION_HEADER, "1")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertReplacementPreviewDifferences(t *testing.T, replacement *managedimport.TrackReplacementPreview, trackID string) {
	t.Helper()
	if replacement.TrackID != trackID || replacement.TrackTitle != "Inspection Fixture" || replacement.ConfirmationToken == "" {
		t.Fatalf("replacement preview identity = %+v", replacement)
	}
	if replacement.SourceFormat.IsChanged || replacement.SourceFormat.Current != "flac" {
		t.Fatalf("source format diff = %+v", replacement.SourceFormat)
	}
	genres := findFieldDiff(t, replacement.Metadata, "genres")
	if !genres.IsChanged || genres.Current != "Electronic" || genres.Replacement != "Ambient" {
		t.Fatalf("genre diff = %+v", genres)
	}
	if findFieldDiff(t, replacement.Metadata, "title").IsChanged || findFieldDiff(t, replacement.TechnicalProperties, "sampleRateHz").IsChanged {
		t.Fatalf("unchanged fields reported as changed: %+v", replacement)
	}
	if replacement.CanonicalPath.IsChanged || replacement.OldFile.Path != replacement.CanonicalPath.Current || replacement.OldFile.SizeBytes <= 0 {
		t.Fatalf("canonical path/old file = %+v / %+v", replacement.CanonicalPath, replacement.OldFile)
	}
	if replacement.Artwork.IsChanged || replacement.Artwork.ReplacesAlbumArtwork || replacement.Library.MovesAlbum || replacement.Library.RemovesEmptyAlbum {
		t.Fatalf("artwork/library change = %+v / %+v", replacement.Artwork, replacement.Library)
	}
	if len(replacement.PlaylistReferences) != 1 || replacement.PlaylistReferences[0].Name != "Keep This Playlist" || len(replacement.QueueReferences) != 1 {
		t.Fatalf("preserved references = %+v / %+v", replacement.PlaylistReferences, replacement.QueueReferences)
	}
}

func findFieldDiff(t *testing.T, diffs []managedimport.TrackReplacementFieldDiff, field string) managedimport.TrackReplacementFieldDiff {
	t.Helper()
	for _, diff := range diffs {
		if diff.Field == field {
			return diff
		}
	}
	t.Fatalf("field diff %q missing from %+v", field, diffs)
	return managedimport.TrackReplacementFieldDiff{}
}

func assertReplacedTrack(t *testing.T, router http.Handler, trackID, genre string) {
	t.Helper()
	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != trackID {
		t.Fatalf("Tracks after replacement = %+v", tracks.Items)
	}
	if len(tracks.Items[0].Genres) != 1 || tracks.Items[0].Genres[0].Name != genre {
		t.Fatalf("replaced Track Genres = %+v", tracks.Items[0].Genres)
	}
}

func assertStreamedBytes(t *testing.T, router http.Handler, trackID string, expected []byte) {
	t.Helper()
	response := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+trackID+"/stream", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("stream status = %d, body = %s", response.Code, response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), expected) {
		t.Fatal("streamed Managed Track bytes differ from the expected file")
	}
}
