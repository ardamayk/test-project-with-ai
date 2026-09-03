package managedimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

// cleanupFixture seeds a legacy music root with two migratable sources in a
// nested Album directory and one rejected source in a sibling directory.
type cleanupFixture struct {
	*cutoverFixture
	musicRoot      string
	firstPath      string
	secondPath     string
	rejectedPath   string
	firstContents  []byte
	secondContents []byte
	firstLegacyID  string
	secondLegacyID string
	rejectedID     string
}

func newCleanupFixture(t *testing.T, options ...cutoverFixtureOption) *cleanupFixture {
	t.Helper()
	musicRoot := t.TempDir()
	fixture := &cleanupFixture{
		musicRoot:      musicRoot,
		firstContents:  []byte("cleanup legacy audio one"),
		secondContents: []byte("cleanup legacy audio two"),
	}
	fixture.cutoverFixture = newCutoverFixture(t, append([]cutoverFixtureOption{withMusicPaths(musicRoot)}, options...)...)
	albumDirectory := filepath.Join(musicRoot, "Cutover Artist", "Cutover Album")
	fixture.firstPath, fixture.firstLegacyID, _ = fixture.seedLegacyTrackIn(t, albumDirectory, "one.flac", "Cleanup One", fixture.firstContents, 1)
	fixture.secondPath, fixture.secondLegacyID, _ = fixture.seedLegacyTrackIn(t, albumDirectory, "two.flac", "Cleanup Two", fixture.secondContents, 2)
	fixture.inspect(fixture.firstContents, "Cleanup One", 1)
	fixture.inspect(fixture.secondContents, "Cleanup Two", 2)
	rejectedContents := []byte("cleanup rejected audio")
	fixture.rejectedPath, fixture.rejectedID, _ = fixture.seedLegacyTrackIn(t, filepath.Join(musicRoot, "Other"), "bad.flac", "Rejected", rejectedContents, 3)
	fixture.rejectInspection(fixture.rejectedPath, &library.InspectionError{Code: library.INSPECTION_ERROR_MISSING_ARTWORK, Field: "artwork", Reason: "missing artwork"})
	return fixture
}

// migrate stages and cuts over the fixture and asserts that every legacy
// source file is still intact afterwards: migration success never cleans up.
func (fixture *cleanupFixture) migrate(t *testing.T) (firstTrackID, secondTrackID string) {
	t.Helper()
	fixture.stage(t)
	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 2 || cutover.RejectedCount != 1 {
		t.Fatalf("cutover = %+v, want 2 migrated and 1 rejected", cutover)
	}
	for _, path := range []string{fixture.firstPath, fixture.secondPath, fixture.rejectedPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("legacy source %q was touched by the migration: %v", path, err)
		}
	}
	return findCutoverFile(t, cutover, fixture.firstLegacyID).CreatedTrackID, findCutoverFile(t, cutover, fixture.secondLegacyID).CreatedTrackID
}

func (fixture *cleanupFixture) preview(t *testing.T) MigrationCleanupPreview {
	t.Helper()
	response := testutil.ServeRequest(t, fixture.router, http.MethodGet, "/api/v1/library-migrations/cleanup", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("cleanup preview status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview MigrationCleanupPreview
	testutil.DecodeJSON(t, response, &preview)
	return preview
}

func (fixture *cleanupFixture) confirm(t *testing.T, confirmation MigrationCleanupConfirmation, wantStatus int) MigrationCleanup {
	t.Helper()
	body, err := json.Marshal(confirmation)
	if err != nil {
		t.Fatalf("encode cleanup confirmation: %v", err)
	}
	response := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cleanup", bytes.NewReader(body), map[string]string{MIGRATION_CLEANUP_REQUEST_HEADER: "1"})
	if response.Code != wantStatus {
		t.Fatalf("cleanup status = %d, want %d, body = %s", response.Code, wantStatus, response.Body.String())
	}
	if wantStatus != http.StatusOK {
		return MigrationCleanup{}
	}
	var cleanup MigrationCleanup
	testutil.DecodeJSON(t, response, &cleanup)
	return cleanup
}

func (fixture *cleanupFixture) assertSourcesIntact(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("legacy source %q is no longer intact: %v", path, err)
		}
	}
}

func findCleanupPreviewFile(t *testing.T, preview MigrationCleanupPreview, trackID string) MigrationCleanupPreviewFile {
	t.Helper()
	for _, file := range preview.Files {
		if file.TrackID == trackID {
			return file
		}
	}
	t.Fatalf("cleanup preview does not contain Track %q", trackID)
	return MigrationCleanupPreviewFile{}
}

func TestLegacySourceCleanupPreviewOffersOnlyMigratedSources(t *testing.T) {
	fixture := newCleanupFixture(t)
	firstTrackID, secondTrackID := fixture.migrate(t)

	preview := fixture.preview(t)

	if preview.EligibleCount != 2 || preview.IneligibleCount != 1 {
		t.Fatalf("preview counts = %d eligible, %d ineligible, want 2 and 1", preview.EligibleCount, preview.IneligibleCount)
	}
	wantSize := int64(len(fixture.firstContents) + len(fixture.secondContents))
	if preview.TotalSizeBytes != wantSize {
		t.Fatalf("preview total size = %d, want %d", preview.TotalSizeBytes, wantSize)
	}
	first := findCleanupPreviewFile(t, preview, firstTrackID)
	if first.State != MIGRATION_CLEANUP_ELIGIBLE || first.SizeBytes != int64(len(fixture.firstContents)) || first.SourceTrackID != fixture.firstLegacyID || first.ContentSHA256 != migrationTestSHA256(fixture.firstContents) || first.OriginalFilename != "one.flac" {
		t.Fatalf("first eligible file = %+v", first)
	}
	if second := findCleanupPreviewFile(t, preview, secondTrackID); second.State != MIGRATION_CLEANUP_ELIGIBLE {
		t.Fatalf("second file = %+v, want eligible", second)
	}
	rejected := findCleanupPreviewFile(t, preview, fixture.rejectedID)
	if rejected.State != MIGRATION_CLEANUP_INELIGIBLE || rejected.ErrorCode != ERROR_CODE_CLEANUP_NOT_MIGRATED || rejected.SizeBytes != 0 {
		t.Fatalf("rejected file = %+v, want ineligible %s", rejected, ERROR_CODE_CLEANUP_NOT_MIGRATED)
	}
	fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)
}

func TestLegacySourceCleanupDeletesOnlyConfirmedFilesAndPrunesEmptyDirectories(t *testing.T) {
	fixture := newCleanupFixture(t)
	firstTrackID, secondTrackID := fixture.migrate(t)
	albumDirectory := filepath.Dir(fixture.firstPath)
	artistDirectory := filepath.Dir(albumDirectory)

	cleanup := fixture.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{firstTrackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.firstContents))}, http.StatusOK)

	if cleanup.DeletedCount != 1 || cleanup.FailedCount != 0 || cleanup.DeletedBytes != int64(len(fixture.firstContents)) || cleanup.PrunedDirectoryCount != 0 {
		t.Fatalf("first cleanup = %+v", cleanup)
	}
	if len(cleanup.Files) != 1 || cleanup.Files[0].State != MIGRATION_CLEANUP_DELETED || cleanup.Files[0].TrackID != firstTrackID || cleanup.Files[0].SourceTrackID != fixture.firstLegacyID {
		t.Fatalf("first cleanup files = %+v", cleanup.Files)
	}
	if _, err := os.Stat(fixture.firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first legacy source still exists: %v", err)
	}
	fixture.assertSourcesIntact(t, fixture.secondPath, fixture.rejectedPath, albumDirectory)
	preview := fixture.preview(t)
	if preview.EligibleCount != 1 || findCleanupPreviewFile(t, preview, secondTrackID).State != MIGRATION_CLEANUP_ELIGIBLE {
		t.Fatalf("preview after first cleanup = %+v", preview)
	}
	for _, file := range preview.Files {
		if file.TrackID == firstTrackID {
			t.Fatalf("cleaned source is still listed: %+v", file)
		}
	}

	cleanup = fixture.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{secondTrackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.secondContents))}, http.StatusOK)

	if cleanup.DeletedCount != 1 || cleanup.PrunedDirectoryCount != 2 {
		t.Fatalf("second cleanup = %+v, want 1 deleted and 2 pruned directories", cleanup)
	}
	for _, directory := range []string{albumDirectory, artistDirectory} {
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("emptied directory %q still exists: %v", directory, err)
		}
	}
	if _, err := os.Stat(fixture.musicRoot); err != nil {
		t.Fatalf("music path root was removed: %v", err)
	}
	fixture.assertSourcesIntact(t, fixture.rejectedPath)
	for _, trackID := range []string{firstTrackID, secondTrackID} {
		managedPath, _, err := NewStore(fixture.database).FindActiveManagedTrackSource(t.Context(), trackID)
		if err != nil || managedPath == "" {
			t.Fatalf("migrated Track %q is no longer active: %v", trackID, err)
		}
		if _, statErr := os.Stat(managedPath); statErr != nil {
			t.Fatalf("managed file %q missing after cleanup: %v", managedPath, statErr)
		}
	}
	if fixture.count(t, `SELECT COUNT(*) FROM legacy_migration_sources WHERE cleaned_at IS NULL`) != 0 {
		t.Fatal("cleaned sources still recorded as pending cleanup")
	}
	fixture.requireNoForeignKeyViolations(t)
}

func TestLegacySourceCleanupRejectsMismatchedConfirmationsWithoutDeleting(t *testing.T) {
	fixture := newCleanupFixture(t)
	firstTrackID, secondTrackID := fixture.migrate(t)
	firstSize := int64(len(fixture.firstContents))
	secondSize := int64(len(fixture.secondContents))
	confirmations := map[string]MigrationCleanupConfirmation{
		"wrong file count":      {TrackIDs: []string{firstTrackID, secondTrackID}, FileCount: 1, TotalSizeBytes: firstSize + secondSize},
		"wrong total size":      {TrackIDs: []string{firstTrackID}, FileCount: 1, TotalSizeBytes: firstSize + 1},
		"duplicate selection":   {TrackIDs: []string{firstTrackID, firstTrackID}, FileCount: 2, TotalSizeBytes: firstSize * 2},
		"rejected legacy Track": {TrackIDs: []string{fixture.rejectedID}, FileCount: 1, TotalSizeBytes: firstSize},
		"unknown Track":         {TrackIDs: []string{"00000000-0000-4000-8000-000000000000"}, FileCount: 1, TotalSizeBytes: firstSize},
	}
	for name, confirmation := range confirmations {
		t.Run(name, func(t *testing.T) {
			fixture.confirm(t, confirmation, http.StatusConflict)
			fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)
		})
	}
}

func TestLegacySourceCleanupRefusesWholeSelectionWhenOneSourceChanged(t *testing.T) {
	fixture := newCleanupFixture(t)
	firstTrackID, secondTrackID := fixture.migrate(t)
	changedContents := []byte("cleanup legacy audio two changed")
	if err := os.WriteFile(fixture.secondPath, changedContents, 0o640); err != nil {
		t.Fatalf("change second legacy source: %v", err)
	}

	preview := fixture.preview(t)
	changed := findCleanupPreviewFile(t, preview, secondTrackID)
	if changed.State != MIGRATION_CLEANUP_INELIGIBLE || changed.ErrorCode != ERROR_CODE_CLEANUP_SOURCE_CHANGED {
		t.Fatalf("changed source = %+v, want ineligible %s", changed, ERROR_CODE_CLEANUP_SOURCE_CHANGED)
	}
	if preview.EligibleCount != 1 || preview.TotalSizeBytes != int64(len(fixture.firstContents)) {
		t.Fatalf("preview = %+v, want only the unchanged source eligible", preview)
	}

	fixture.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{firstTrackID, secondTrackID}, FileCount: 2, TotalSizeBytes: int64(len(fixture.firstContents) + len(changedContents))}, http.StatusConflict)

	fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)
}

func TestLegacySourceCleanupIgnoresVerifiedCopiesAndInactiveManagedTracks(t *testing.T) {
	fixture := newCleanupFixture(t)
	stage := fixture.stage(t)
	if stage.VerifiedCount != 2 {
		t.Fatalf("stage = %+v, want 2 verified copies", stage)
	}

	preview := fixture.preview(t)

	if preview.EligibleCount != 0 || preview.IneligibleCount != 3 || preview.TotalSizeBytes != 0 {
		t.Fatalf("preview before cutover = %+v, want nothing eligible", preview)
	}
	verified := findCleanupPreviewFile(t, preview, fixture.firstLegacyID)
	if verified.State != MIGRATION_CLEANUP_INELIGIBLE || verified.ErrorCode != ERROR_CODE_CLEANUP_NOT_MIGRATED {
		t.Fatalf("verified copy = %+v, want ineligible %s", verified, ERROR_CODE_CLEANUP_NOT_MIGRATED)
	}
	var pendingTrackID string
	if err := fixture.database.QueryRow(`SELECT pending_track_id FROM legacy_migration_copies WHERE source_track_id = ?`, fixture.firstLegacyID).Scan(&pendingTrackID); err != nil {
		t.Fatalf("read pending Track ID: %v", err)
	}
	for _, trackID := range []string{fixture.firstLegacyID, pendingTrackID} {
		fixture.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{trackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.firstContents))}, http.StatusConflict)
	}
	fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)

	firstTrackID, _ := fixture.migrate(t)
	if _, err := fixture.database.Exec(`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, firstTrackID); err != nil {
		t.Fatalf("deactivate migrated Track: %v", err)
	}

	preview = fixture.preview(t)
	inactive := findCleanupPreviewFile(t, preview, firstTrackID)
	if inactive.State != MIGRATION_CLEANUP_INELIGIBLE || inactive.ErrorCode != ERROR_CODE_CLEANUP_MANAGED_TRACK_MISSING {
		t.Fatalf("inactive migrated Track = %+v, want ineligible %s", inactive, ERROR_CODE_CLEANUP_MANAGED_TRACK_MISSING)
	}
	fixture.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{firstTrackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.firstContents))}, http.StatusConflict)
	fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)
}

func TestLegacySourceCleanupRefusesSourcesOutsideMusicPaths(t *testing.T) {
	fixture := newCutoverFixture(t)
	contents := []byte("cleanup outside audio")
	path, legacyID, _ := fixture.seedLegacyTrack(t, "outside.flac", "Outside", contents, 1)
	fixture.inspect(contents, "Outside", 1)
	fixture.stage(t)
	cutover := fixture.cutover(t)
	trackID := findCutoverFile(t, cutover, legacyID).CreatedTrackID
	cleanup := &cleanupFixture{cutoverFixture: fixture}

	preview := cleanup.preview(t)

	outside := findCleanupPreviewFile(t, preview, trackID)
	if outside.State != MIGRATION_CLEANUP_INELIGIBLE || outside.ErrorCode != ERROR_CODE_CLEANUP_SOURCE_OUTSIDE_MUSIC_PATHS {
		t.Fatalf("outside source = %+v, want ineligible %s", outside, ERROR_CODE_CLEANUP_SOURCE_OUTSIDE_MUSIC_PATHS)
	}
	cleanup.confirm(t, MigrationCleanupConfirmation{TrackIDs: []string{trackID}, FileCount: 1, TotalSizeBytes: int64(len(contents))}, http.StatusConflict)
	cleanup.assertSourcesIntact(t, path)
}

func TestLegacySourceCleanupRequiresExplicitConfirmation(t *testing.T) {
	fixture := newCleanupFixture(t)
	firstTrackID, _ := fixture.migrate(t)
	body, err := json.Marshal(MigrationCleanupConfirmation{TrackIDs: []string{firstTrackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.firstContents))})
	if err != nil {
		t.Fatalf("encode cleanup confirmation: %v", err)
	}

	withoutHeader := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cleanup", bytes.NewReader(body), nil)
	testutil.AssertErrorCode(t, withoutHeader, http.StatusForbidden, "migration_cleanup_forbidden")
	emptySelection := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cleanup", bytes.NewReader([]byte(`{"trackIds":[],"fileCount":0,"totalSizeBytes":0}`)), map[string]string{MIGRATION_CLEANUP_REQUEST_HEADER: "1"})
	testutil.AssertErrorCode(t, emptySelection, http.StatusBadRequest, "invalid_cleanup_confirmation")
	unknownField := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cleanup", bytes.NewReader([]byte(`{"trackIds":["x"],"fileCount":1,"totalSizeBytes":1,"force":true}`)), map[string]string{MIGRATION_CLEANUP_REQUEST_HEADER: "1"})
	testutil.AssertErrorCode(t, unknownField, http.StatusBadRequest, "invalid_cleanup_confirmation")

	fixture.assertSourcesIntact(t, fixture.firstPath, fixture.secondPath, fixture.rejectedPath)
}
