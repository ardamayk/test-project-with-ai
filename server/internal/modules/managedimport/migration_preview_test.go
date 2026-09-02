package managedimport

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

type migrationInspectionResult struct {
	inspection library.MediaInspection
	err        error
}

type migrationInspector struct {
	results   map[string]migrationInspectionResult
	onInspect func(string)
}

var migrationPreviewHeaders = map[string]string{MIGRATION_PREVIEW_REQUEST_HEADER: "1"}

const SECOND_MIGRATION_SHA256 = "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func (inspector migrationInspector) Inspect(_ context.Context, path string, _ library.InspectionProgressReporter) (library.MediaInspection, error) {
	if inspector.onInspect != nil {
		inspector.onInspect(path)
	}
	result, ok := inspector.results[path]
	if !ok {
		return library.MediaInspection{}, errors.New("unexpected migration source")
	}
	return result.inspection, result.err
}

func TestLibraryMigrationPreviewReportsStableStrictResultsWithoutMutation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	acceptedPath, acceptedTrackID := seedLegacyMigrationTrack(t, database, "accepted.flac", []byte("accepted legacy audio"), 1)
	rejectedPath, rejectedTrackID := seedLegacyMigrationTrack(t, database, "rejected.flac", []byte("rejected legacy audio"), 2)
	acceptedInspection := strictMigrationInspection()
	inspector := migrationInspector{results: map[string]migrationInspectionResult{
		acceptedPath: {inspection: acceptedInspection},
		rejectedPath: {err: &library.InspectionError{
			Code:   library.INSPECTION_ERROR_MISSING_ARTWORK,
			Field:  "artwork",
			Reason: "embedded front-cover artwork is required",
			Err:    errors.New("missing artwork"),
		}},
	}}
	managedStoragePath := t.TempDir()
	configuration := config.Config{ManagedStoragePath: managedStoragePath, ManagedStorageReserveBytes: 1024}
	importModule := newModule(database, configuration, inspector, func(string) (int64, error) { return 1 << 40, nil })
	libraryModule := library.NewModule(database, configuration)
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)

	firstResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("migration preview status = %d, body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	var firstPreview MigrationPreview
	testutil.DecodeJSON(t, firstResponse, &firstPreview)
	if firstPreview.AcceptedCount != 1 || firstPreview.RejectedCount != 1 || len(firstPreview.Files) != 2 {
		t.Fatalf("migration preview = %+v", firstPreview)
	}
	acceptedFile := findMigrationFile(t, firstPreview, acceptedTrackID)
	rejectedFile := findMigrationFile(t, firstPreview, rejectedTrackID)
	assertMigrationFile(t, acceptedFile, acceptedTrackID, MIGRATION_FILE_ACCEPTED, "", "")
	assertMigrationFile(t, rejectedFile, rejectedTrackID, MIGRATION_FILE_REJECTED, "missing_artwork", "embedded front-cover artwork is required")
	if acceptedFile.Preview == nil || acceptedFile.Preview.Title != acceptedInspection.Metadata.Title {
		t.Fatalf("accepted migration file preview = %+v", acceptedFile.Preview)
	}

	secondResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var secondPreview MigrationPreview
	testutil.DecodeJSON(t, secondResponse, &secondPreview)
	if !reflect.DeepEqual(secondPreview, firstPreview) {
		t.Fatalf("repeated migration preview changed:\nfirst  = %+v\nsecond = %+v", firstPreview, secondPreview)
	}
	assertLegacySource(t, database, acceptedTrackID, acceptedPath)
	assertLegacySource(t, database, rejectedTrackID, rejectedPath)
	assertNoMigrationMutation(t, database, managedStoragePath)

	streamResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+acceptedTrackID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK || streamResponse.Body.String() != "accepted legacy audio" {
		t.Fatalf("legacy Track stream status = %d, body = %q", streamResponse.Code, streamResponse.Body.String())
	}
}

func TestLibraryMigrationPreviewRejectsAcceptedFilesWhenCapacityIsInsufficient(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourceBytes := []byte("legacy audio")
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "track.flac", sourceBytes, 1)
	inspection := strictMigrationInspection()
	const reserveBytes int64 = 1024
	requiredBytes := reserveBytes + int64(len(sourceBytes))*2 + int64(len(inspection.AlbumArtwork.Data))
	configuration := config.Config{ManagedStoragePath: t.TempDir(), ManagedStorageReserveBytes: reserveBytes}
	module := newModule(database, configuration, migrationInspector{results: map[string]migrationInspectionResult{
		sourcePath: {inspection: inspection},
	}}, func(string) (int64, error) { return requiredBytes - 1, nil })
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	if response.Code != http.StatusOK {
		t.Fatalf("migration preview status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	if preview.AcceptedCount != 0 || preview.RejectedCount != 1 || len(preview.Files) != 1 {
		t.Fatalf("capacity migration preview = %+v", preview)
	}
	assertMigrationFile(t, preview.Files[0], trackID, MIGRATION_FILE_REJECTED, "insufficient_storage", "Managed Storage does not have enough capacity for this migration and its safety reserve")
}

func TestLibraryMigrationPreviewAppliesConfiguredFileLimitBeforeStrictInspection(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourceBytes := []byte("legacy audio")
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "track.flac", sourceBytes, 1)
	inspectionCount := 0
	configuration := config.Config{
		ManagedStoragePath:          t.TempDir(),
		ManagedImportFileLimitBytes: int64(len(sourceBytes) - 1),
	}
	inspector := migrationInspector{
		results:   map[string]migrationInspectionResult{sourcePath: {inspection: strictMigrationInspection()}},
		onInspect: func(string) { inspectionCount++ },
	}
	module := newModule(database, configuration, inspector, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	if response.Code != http.StatusOK {
		t.Fatalf("migration preview status = %d, body = %s", response.Code, response.Body.String())
	}
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	if preview.AcceptedCount != 0 || preview.RejectedCount != 1 {
		t.Fatalf("file-limit migration preview = %+v", preview)
	}
	assertMigrationFile(t, preview.Files[0], trackID, MIGRATION_FILE_REJECTED, "upload_too_large", "file exceeds the configured per-file byte limit")
	if inspectionCount != 0 {
		t.Fatalf("oversized migration source inspection count = %d, want 0", inspectionCount)
	}
}

func TestLibraryMigrationPreviewRequiresApplicationHeader(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, nil)
	testutil.AssertErrorCode(t, response, http.StatusForbidden, "migration_preview_forbidden")
}

func TestLibraryMigrationPreviewRejectsConcurrentAnalysis(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, _ := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	started := make(chan struct{})
	release := make(chan struct{})
	inspector := migrationInspector{
		results: map[string]migrationInspectionResult{sourcePath: {inspection: strictMigrationInspection()}},
		onInspect: func(string) {
			close(started)
			<-release
		},
	}
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: config.DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES, BatchBytes: config.DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES}, unlimitedStorageCapacity)
	service := NewService(NewStore(database), storage, inspector)
	firstResult := make(chan error, 1)
	go func() {
		_, err := service.PreviewMigration(context.Background())
		firstResult <- err
	}()
	<-started

	_, err := service.PreviewMigration(context.Background())
	close(release)
	firstErr := <-firstResult
	if !errors.Is(err, ErrMigrationInProgress) {
		t.Fatalf("concurrent migration preview error = %v", err)
	}
	if firstErr != nil {
		t.Fatalf("first migration preview: %v", firstErr)
	}
}

func TestLibraryMigrationPreviewIgnoresUnrelatedAwaitingImportPreviews(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	awaitingPath := filepath.Join(t.TempDir(), "awaiting.flac")
	if _, err := database.Exec(`INSERT INTO managed_import_jobs (
		id, status, revision, original_filename, staged_file_path, content_sha256
	) VALUES ('awaiting-job', 'awaiting_confirmation', 2, 'awaiting.flac', ?, ?)`, awaitingPath, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("seed unrelated awaiting Import Preview: %v", err)
	}
	legacyInspection := strictMigrationInspection()
	legacyInspection.Metadata.HasDiscNumber = false
	awaitingInspection := strictMigrationInspection()
	awaitingInspection.Metadata.HasDiscNumber = true
	awaitingInspection.Metadata.DiscPosition = library.MediaPosition{Number: 2, Total: 2}
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		sourcePath:   {inspection: legacyInspection},
		awaitingPath: {inspection: awaitingInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	assertMigrationFile(t, preview.Files[0], trackID, MIGRATION_FILE_ACCEPTED, "", "")
}

func TestLibraryMigrationPreviewExcludesMissingLegacyTracks(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	_, trackID := seedLegacyMigrationTrack(t, database, "missing.flac", []byte("legacy audio"), 1)
	if _, err := database.Exec(`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, trackID); err != nil {
		t.Fatalf("mark legacy Track missing: %v", err)
	}
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	if preview.AcceptedCount != 0 || preview.RejectedCount != 0 || len(preview.Files) != 0 {
		t.Fatalf("missing Legacy Track preview = %+v", preview)
	}
}

func TestLibraryMigrationPreviewRejectsExistingManagedHash(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "legacy.flac", []byte("legacy audio"), 1)
	_, managedTrackID := seedLegacyMigrationTrack(t, database, "managed.flac", []byte("managed audio"), 2)
	inspection := strictMigrationInspection()
	if _, err := database.Exec(`UPDATE track_sources SET source_kind = 'managed', content_sha256 = ? WHERE track_id = ?`, inspection.FileSHA256, managedTrackID); err != nil {
		t.Fatalf("seed existing Managed Track hash: %v", err)
	}
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		sourcePath: {inspection: inspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	assertMigrationFile(t, preview.Files[0], trackID, MIGRATION_FILE_REJECTED, "exact_duplicate", "file bytes already belong to an existing Managed Track")
}

func TestLibraryMigrationPreviewRejectsSourceChangedDuringInspection(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	inspector := migrationInspector{
		results: map[string]migrationInspectionResult{sourcePath: {inspection: strictMigrationInspection()}},
		onInspect: func(path string) {
			if err := os.WriteFile(path, []byte("changed and larger legacy audio"), 0o640); err != nil {
				t.Fatalf("replace legacy source during inspection: %v", err)
			}
		},
	}
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, inspector, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	assertMigrationFile(t, preview.Files[0], trackID, MIGRATION_FILE_REJECTED, "legacy_source_changed", "legacy source changed during migration analysis")
}

func TestLibraryMigrationPreviewValidatesAlbumRulesAcrossAcceptedFiles(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("first legacy audio"), 1)
	secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("second legacy audio"), 2)
	firstInspection := strictMigrationInspection()
	firstInspection.Metadata.HasDiscNumber = false
	firstInspection.Metadata.TrackPosition.Total = 10
	firstInspection.AlbumArtwork.SHA256 = "first-artwork"
	secondInspection := strictMigrationInspection()
	secondInspection.FileSHA256 = SECOND_MIGRATION_SHA256
	secondInspection.Metadata.HasDiscNumber = true
	secondInspection.Metadata.DiscPosition = library.MediaPosition{Number: 2, Total: 2}
	secondInspection.Metadata.TrackPosition = library.MediaPosition{Number: 2, Total: 11}
	secondInspection.AlbumArtwork.SHA256 = "second-artwork"
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		firstPath:  {inspection: firstInspection},
		secondPath: {inspection: secondInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	firstFile := findMigrationFile(t, preview, firstTrackID)
	secondFile := findMigrationFile(t, preview, secondTrackID)
	assertMigrationFile(t, firstFile, firstTrackID, MIGRATION_FILE_REJECTED, "invalid_metadata", "DISCNUMBER is required for a known multi-disc Album")
	if secondFile.State != MIGRATION_FILE_REJECTED || secondFile.ErrorCode == "" {
		t.Fatalf("conflicting migration Album sibling = %+v", secondFile)
	}
}

func TestLibraryMigrationPreviewRejectsConflictingArtworkAcrossAcceptedFiles(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("first legacy audio"), 1)
	secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("second legacy audio"), 2)
	firstInspection := strictMigrationInspection()
	firstInspection.AlbumArtwork.SHA256 = "first-artwork"
	secondInspection := strictMigrationInspection()
	secondInspection.FileSHA256 = SECOND_MIGRATION_SHA256
	secondInspection.Metadata.TrackPosition.Number = 2
	secondInspection.AlbumArtwork.SHA256 = "second-artwork"
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		firstPath:  {inspection: firstInspection},
		secondPath: {inspection: secondInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	for _, trackID := range []string{firstTrackID, secondTrackID} {
		file := findMigrationFile(t, preview, trackID)
		assertMigrationFile(t, file, trackID, MIGRATION_FILE_REJECTED, "invalid_artwork", "embedded artwork differs from another Track in the Album")
	}
}

func TestLibraryMigrationPreviewRejectsConflictingTotalsAcrossAcceptedFiles(t *testing.T) {
	testCases := []struct {
		name      string
		reason    string
		configure func(*library.MediaInspection, *library.MediaInspection)
	}{
		{name: "disc total", reason: "DISCNUMBER total conflicts with another Track in the Album", configure: func(first, second *library.MediaInspection) {
			first.Metadata.DiscPosition.Total = 2
			second.Metadata.DiscPosition.Total = 3
		}},
		{name: "track total", reason: "TRACKNUMBER total conflicts with another Track in the Album", configure: func(first, second *library.MediaInspection) {
			first.Metadata.TrackPosition.Total = 10
			second.Metadata.TrackPosition.Total = 11
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := testutil.OpenMigratedDB(t)
			firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("first legacy audio"), 1)
			secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("second legacy audio"), 2)
			firstInspection, secondInspection := strictMigrationInspection(), strictMigrationInspection()
			secondInspection.FileSHA256 = SECOND_MIGRATION_SHA256
			firstInspection.Metadata.HasDiscNumber = true
			secondInspection.Metadata.HasDiscNumber = true
			secondInspection.Metadata.TrackPosition.Number = 2
			testCase.configure(&firstInspection, &secondInspection)
			module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
				firstPath: {inspection: firstInspection}, secondPath: {inspection: secondInspection},
			}}, unlimitedStorageCapacity)
			router := chi.NewRouter()
			module.RegisterRoutes(router)
			response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
			var preview MigrationPreview
			testutil.DecodeJSON(t, response, &preview)
			for _, trackID := range []string{firstTrackID, secondTrackID} {
				file := findMigrationFile(t, preview, trackID)
				assertMigrationFile(t, file, trackID, MIGRATION_FILE_REJECTED, "invalid_metadata", testCase.reason)
			}
		})
	}
}

func TestLibraryMigrationPreviewRejectsDuplicateHashesWithinMigration(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("same bytes"), 1)
	secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("same bytes"), 2)
	firstInspection, secondInspection := strictMigrationInspection(), strictMigrationInspection()
	secondInspection.Metadata.TrackPosition.Number = 2
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		firstPath: {inspection: firstInspection}, secondPath: {inspection: secondInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	for _, trackID := range []string{firstTrackID, secondTrackID} {
		file := findMigrationFile(t, preview, trackID)
		assertMigrationFile(t, file, trackID, MIGRATION_FILE_REJECTED, "exact_duplicate", "file bytes duplicate another Legacy Track in the migration")
	}
}

func TestLibraryMigrationPreviewRejectsDuplicateAlbumPositionsWithinMigration(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("first bytes"), 1)
	secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("second bytes"), 2)
	firstInspection, secondInspection := strictMigrationInspection(), strictMigrationInspection()
	secondInspection.FileSHA256 = SECOND_MIGRATION_SHA256
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		firstPath: {inspection: firstInspection}, secondPath: {inspection: secondInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	for _, trackID := range []string{firstTrackID, secondTrackID} {
		file := findMigrationFile(t, preview, trackID)
		assertMigrationFile(t, file, trackID, MIGRATION_FILE_REJECTED, "invalid_metadata", "Track position conflicts with another Track in the Album")
	}
}

func TestLibraryMigrationPreviewRejectsDiscNumberAboveMigrationTotal(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	firstPath, firstTrackID := seedLegacyMigrationTrack(t, database, "first.flac", []byte("first bytes"), 1)
	secondPath, secondTrackID := seedLegacyMigrationTrack(t, database, "second.flac", []byte("second bytes"), 2)
	firstInspection, secondInspection := strictMigrationInspection(), strictMigrationInspection()
	firstInspection.Metadata.HasDiscNumber = true
	firstInspection.Metadata.DiscPosition.Total = 2
	secondInspection.FileSHA256 = SECOND_MIGRATION_SHA256
	secondInspection.Metadata.HasDiscNumber = true
	secondInspection.Metadata.DiscPosition.Number = 3
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		firstPath: {inspection: firstInspection}, secondPath: {inspection: secondInspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	for _, trackID := range []string{firstTrackID, secondTrackID} {
		file := findMigrationFile(t, preview, trackID)
		assertMigrationFile(t, file, trackID, MIGRATION_FILE_REJECTED, "invalid_metadata", "DISCNUMBER total conflicts with another Track in the Album")
	}
}

func TestLibraryMigrationPreviewRejectsExistingAlbumArtworkConflict(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	candidatePath, candidateTrackID := seedLegacyMigrationTrack(t, database, "candidate.flac", []byte("candidate bytes"), 1)
	_, existingTrackID := seedLegacyMigrationTrack(t, database, "existing.flac", []byte("existing bytes"), 2)
	inspection := strictMigrationInspection()
	inspection.AlbumArtwork.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var albumID string
	if err := database.QueryRow(`SELECT album_id FROM tracks WHERE id = ?`, existingTrackID).Scan(&albumID); err != nil {
		t.Fatalf("read existing Album ID: %v", err)
	}
	if _, err := database.Exec(`UPDATE albums SET identity_key = ? WHERE id = ?`, albumIdentityKey(inspection.Metadata), albumID); err != nil {
		t.Fatalf("seed existing Album identity: %v", err)
	}
	if _, err := database.Exec(`UPDATE track_sources SET source_kind = 'managed', content_sha256 = ? WHERE track_id = ?`, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", existingTrackID); err != nil {
		t.Fatalf("seed existing Managed Track source: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO album_artwork (id, album_id, source_track_id, content_sha256, media_type, width, height, encoded_size_bytes, file_path) VALUES (?, ?, ?, ?, 'image/png', 1, 1, 1, ?)`,
		"existing-artwork", albumID, existingTrackID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", filepath.Join(t.TempDir(), "cover.png")); err != nil {
		t.Fatalf("seed existing Album Artwork: %v", err)
	}
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		candidatePath: {inspection: inspection},
	}}, unlimitedStorageCapacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	var preview MigrationPreview
	testutil.DecodeJSON(t, response, &preview)
	assertMigrationFile(t, preview.Files[0], candidateTrackID, MIGRATION_FILE_REJECTED, "album_artwork_conflict", "embedded Album Artwork differs from the existing Album")
}

func TestLibraryMigrationPreviewPropagatesUnsafeStorage(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, _ := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	module := newModule(database, config.Config{ManagedStoragePath: t.TempDir()}, migrationInspector{results: map[string]migrationInspectionResult{
		sourcePath: {inspection: strictMigrationInspection()},
	}}, func(string) (int64, error) { return 0, ErrUnsafeStoragePath })
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, migrationPreviewHeaders)
	testutil.AssertErrorCode(t, response, http.StatusConflict, "unsafe_storage_path")
}

func TestLibraryMigrationPreviewReleasesArtworkPayloadAfterInspection(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, _ := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	inspection := strictMigrationInspection()
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: config.DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES, BatchBytes: config.DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES}, unlimitedStorageCapacity)
	service := NewService(NewStore(database), storage, migrationInspector{results: map[string]migrationInspectionResult{sourcePath: {inspection: inspection}}})
	sources, err := service.store.ListLegacyMigrationSources(context.Background())
	if err != nil {
		t.Fatalf("list migration sources: %v", err)
	}

	_, candidates, err := service.inspectMigrationSources(context.Background(), sources)
	if err != nil {
		t.Fatalf("inspect migration sources: %v", err)
	}
	if len(candidates) != 1 || candidates[0].artworkBytes != int64(len(inspection.AlbumArtwork.Data)) || candidates[0].inspection.AlbumArtwork.Data != nil {
		t.Fatalf("retained migration candidate artwork = %+v", candidates)
	}
}

func TestLibraryMigrationPreviewPropagatesCanceledAlbumValidation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	sourcePath, _ := seedLegacyMigrationTrack(t, database, "track.flac", []byte("legacy audio"), 1)
	ctx, cancel := context.WithCancel(context.Background())
	inspector := migrationInspector{
		results:   map[string]migrationInspectionResult{sourcePath: {inspection: strictMigrationInspection()}},
		onInspect: func(string) { cancel() },
	}
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: config.DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES, BatchBytes: config.DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES}, unlimitedStorageCapacity)
	service := NewService(NewStore(database), storage, inspector)

	_, err := service.PreviewMigration(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration preview error = %v", err)
	}
}

func seedLegacyMigrationTrack(t *testing.T, database *sql.DB, filename string, contents []byte, trackNumber int) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, contents, 0o640); err != nil {
		t.Fatalf("write legacy source: %v", err)
	}
	store := library.NewStore(database)
	metadata := library.FileMetadata{
		Path:         path,
		Format:       "flac",
		SizeBytes:    int64(len(contents)),
		ModTime:      time.Unix(100, 0),
		Title:        filename,
		Artist:       "Legacy Artist",
		AlbumArtist:  "Legacy Album Artist",
		Album:        "Legacy Album",
		TrackNo:      trackNumber,
		DurationMs:   1000,
		Genre:        "Rock",
		SampleRateHz: 44100,
		BitDepth:     16,
	}
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("seed legacy Track: %v", err)
	}
	var trackID string
	if err := database.QueryRow(`SELECT track_id FROM track_sources WHERE file_path = ?`, path).Scan(&trackID); err != nil {
		t.Fatalf("read legacy Track ID: %v", err)
	}
	return path, trackID
}

func strictMigrationInspection() library.MediaInspection {
	return library.MediaInspection{
		Metadata: library.NormalizedMediaMetadata{
			Title: "Strict Legacy Track", Artists: []string{"Strict Artist"}, AlbumArtists: []string{"Strict Album Artist"},
			Album: "Strict Album", Genres: []string{"Rock"}, TrackPosition: library.MediaPosition{Number: 1}, DiscPosition: library.MediaPosition{Number: 1},
		},
		AlbumArtwork: library.AlbumArtwork{MIMEType: "image/png", Width: 1, Height: 1, Data: []byte("artwork")},
		Audio: library.TechnicalAudioProperties{
			Format: "flac", Container: "flac", Codec: "flac", DurationMs: 1000, SampleRateHz: 44100, ChannelCount: 2, BitDepth: 16, BitrateKbps: 800,
		},
		FileSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func assertMigrationFile(t *testing.T, file MigrationPreviewFile, trackID string, state MigrationFileState, errorCode, errorReason string) {
	t.Helper()
	if file.TrackID != trackID || file.State != state || file.ErrorCode != errorCode || file.ErrorReason != errorReason {
		t.Fatalf("migration file = %+v", file)
	}
}

func findMigrationFile(t *testing.T, preview MigrationPreview, trackID string) MigrationPreviewFile {
	t.Helper()
	for _, file := range preview.Files {
		if file.TrackID == trackID {
			return file
		}
	}
	t.Fatalf("migration preview does not contain Track %q", trackID)
	return MigrationPreviewFile{}
}

func assertLegacySource(t *testing.T, database *sql.DB, trackID, path string) {
	t.Helper()
	var sourceKind, filePath string
	var contentHash *string
	if err := database.QueryRow(`SELECT source_kind, file_path, content_sha256 FROM track_sources WHERE track_id = ?`, trackID).Scan(&sourceKind, &filePath, &contentHash); err != nil {
		t.Fatalf("read legacy source: %v", err)
	}
	if sourceKind != "legacy" || filePath != path || contentHash != nil {
		t.Fatalf("legacy source = (%q, %q, %v)", sourceKind, filePath, contentHash)
	}
}

func assertNoMigrationMutation(t *testing.T, database *sql.DB, managedStoragePath string) {
	t.Helper()
	var managedCount, importCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM track_sources WHERE source_kind = 'managed'`).Scan(&managedCount); err != nil {
		t.Fatalf("count Managed Tracks: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM managed_import_jobs`).Scan(&importCount); err != nil {
		t.Fatalf("count Managed Import Jobs: %v", err)
	}
	entries, err := os.ReadDir(managedStoragePath)
	if err != nil {
		t.Fatalf("read Managed Storage: %v", err)
	}
	if managedCount != 0 || importCount != 0 || len(entries) != 0 {
		t.Fatalf("preview mutation: managed Tracks = %d, Import Jobs = %d, storage entries = %v", managedCount, importCount, entries)
	}
}
