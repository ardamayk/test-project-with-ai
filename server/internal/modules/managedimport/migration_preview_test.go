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
	results map[string]migrationInspectionResult
}

func (inspector migrationInspector) Inspect(_ context.Context, path string, _ library.InspectionProgressReporter) (library.MediaInspection, error) {
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

	firstResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, nil)
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

	secondResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, nil)
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

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/library-migrations/preview", nil, nil)
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
