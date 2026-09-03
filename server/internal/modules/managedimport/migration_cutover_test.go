package managedimport

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

const cutoverAlbumIdentityKey = "cutover artist\x1fcutover album\x1f2020"
const cutoverArtworkData = "cutover album artwork"

type cutoverFixture struct {
	database   *sql.DB
	router     chi.Router
	storage    string
	inspector  *cutoverInspector
	queueEvent *playback.QueueEventBroker
}

// cutoverInspector resolves inspections by file contents so the source and its
// staged copy inspect identically.
type cutoverInspector struct {
	inspectionsByContents map[string]library.MediaInspection
	errorsByPath          map[string]error
}

func (inspector *cutoverInspector) Inspect(_ context.Context, path string, _ library.InspectionProgressReporter) (library.MediaInspection, error) {
	if inspectErr, ok := inspector.errorsByPath[path]; ok {
		return library.MediaInspection{}, inspectErr
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return library.MediaInspection{}, err
	}
	inspection, ok := inspector.inspectionsByContents[migrationTestSHA256(contents)]
	if !ok {
		return library.MediaInspection{}, errors.New("unexpected cutover source contents")
	}
	return inspection, nil
}

func newCutoverFixture(t *testing.T, queueEvents ...QueueInvalidationPublisher) *cutoverFixture {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close cutover test database: %v", err)
		}
	})
	storage := t.TempDir()
	inspector := &cutoverInspector{inspectionsByContents: make(map[string]library.MediaInspection), errorsByPath: make(map[string]error)}
	router := chi.NewRouter()
	importModule := newModule(database, config.Config{ManagedStoragePath: storage}, inspector, unlimitedStorageCapacity, queueEvents...)
	libraryModule := library.NewModule(database, config.Config{ManagedStoragePath: storage})
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess())
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)
	var broker *playback.QueueEventBroker
	if len(queueEvents) > 0 {
		broker, _ = queueEvents[0].(*playback.QueueEventBroker)
	}
	return &cutoverFixture{database: database, router: router, storage: storage, inspector: inspector, queueEvent: broker}
}

func (fixture *cutoverFixture) rejectInspection(path string, err error) {
	fixture.inspector.errorsByPath[path] = err
}

func (fixture *cutoverFixture) inspect(contents []byte, title string, trackNumber int) {
	inspection := cutoverInspection(contents, title, trackNumber)
	fixture.inspector.inspectionsByContents[migrationTestSHA256(contents)] = inspection
}

func (fixture *cutoverFixture) stage(t *testing.T) MigrationStage {
	t.Helper()
	response := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/stage", nil, map[string]string{MIGRATION_STAGE_REQUEST_HEADER: "1"})
	if response.Code != http.StatusOK {
		t.Fatalf("migration stage status = %d, body = %s", response.Code, response.Body.String())
	}
	var stage MigrationStage
	testutil.DecodeJSON(t, response, &stage)
	return stage
}

func (fixture *cutoverFixture) cutover(t *testing.T) MigrationCutover {
	t.Helper()
	response := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cutover", nil, map[string]string{MIGRATION_CUTOVER_REQUEST_HEADER: "1"})
	if response.Code != http.StatusOK {
		t.Fatalf("migration cutover status = %d, body = %s", response.Code, response.Body.String())
	}
	var cutover MigrationCutover
	testutil.DecodeJSON(t, response, &cutover)
	return cutover
}

func (fixture *cutoverFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func (fixture *cutoverFixture) requireNoForeignKeyViolations(t *testing.T) {
	t.Helper()
	rows, err := fixture.database.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("run foreign_key_check: %v", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close foreign_key_check rows: %v", closeErr)
		}
	}()
	if rows.Next() {
		t.Fatal("Library Migration cutover left broken foreign keys")
	}
}

func (fixture *cutoverFixture) requireActiveTrackCount(t *testing.T, albumID string, trackNo, want int) {
	t.Helper()
	count := fixture.count(t, `
		SELECT COUNT(*) FROM tracks
		WHERE album_id = ? AND missing_at IS NULL AND COALESCE(disc_no, 1) = 1 AND track_no = ?`, albumID, trackNo)
	if count != want {
		t.Fatalf("active Track count at position 1/%d = %d, want %d", trackNo, count, want)
	}
}

func (fixture *cutoverFixture) pendingAudioPath(t *testing.T, sourceTrackID string) string {
	t.Helper()
	var pendingPath string
	if err := fixture.database.QueryRow(`SELECT pending_audio_path FROM legacy_migration_copies WHERE source_track_id = ?`, sourceTrackID).Scan(&pendingPath); err != nil {
		t.Fatalf("read pending migration audio path: %v", err)
	}
	return pendingPath
}

func (fixture *cutoverFixture) seedLegacyTrack(t *testing.T, filename, title string, contents []byte, trackNumber int) (string, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, contents, 0o640); err != nil {
		t.Fatalf("write legacy source: %v", err)
	}
	store := library.NewStore(fixture.database)
	metadata := library.FileMetadata{
		Path:         path,
		Format:       "flac",
		SizeBytes:    int64(len(contents)),
		ModTime:      time.Unix(100, 0),
		Title:        title,
		Artist:       "Cutover Artist",
		AlbumArtist:  "Cutover Artist",
		Album:        "Cutover Album",
		TrackNo:      trackNumber,
		Year:         2020,
		DurationMs:   1000,
		Genre:        "Rock",
		SampleRateHz: 44100,
		BitDepth:     16,
	}
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("seed legacy Track: %v", err)
	}
	var trackID, albumID string
	if err := fixture.database.QueryRow(`
		SELECT track_sources.track_id, tracks.album_id
		FROM track_sources JOIN tracks ON tracks.id = track_sources.track_id
		WHERE track_sources.file_path = ?`, path).Scan(&trackID, &albumID); err != nil {
		t.Fatalf("read legacy Track ID: %v", err)
	}
	var identityKey sql.NullString
	if err := fixture.database.QueryRow(`SELECT identity_key FROM albums WHERE id = ?`, albumID).Scan(&identityKey); err != nil {
		t.Fatalf("read legacy Album identity: %v", err)
	}
	if !identityKey.Valid || identityKey.String != cutoverAlbumIdentityKey {
		t.Fatalf("legacy Album identity key = %v, want %q", identityKey, cutoverAlbumIdentityKey)
	}
	return path, trackID, albumID
}

func cutoverInspection(contents []byte, title string, trackNumber int) library.MediaInspection {
	return library.MediaInspection{
		Metadata: library.NormalizedMediaMetadata{
			Title: title, Artists: []string{"Cutover Artist"}, AlbumArtists: []string{"Cutover Artist"},
			Album: "Cutover Album", Genres: []string{"Rock"}, Year: 2020,
			TrackPosition: library.MediaPosition{Number: trackNumber, Total: 2}, DiscPosition: library.MediaPosition{Number: 1},
		},
		AlbumArtwork: library.AlbumArtwork{MIMEType: "image/png", Width: 1, Height: 1, Data: []byte(cutoverArtworkData), SHA256: migrationTestSHA256([]byte(cutoverArtworkData))},
		Audio: library.TechnicalAudioProperties{
			Format: "flac", Container: "flac", Codec: "flac", DurationMs: 1000,
			SampleRateHz: 44100, ChannelCount: 2, BitDepth: 16, BitrateKbps: 800,
		},
		FileSHA256: migrationTestSHA256(contents),
	}
}

func seedCutoverReferences(t *testing.T, database *sql.DB, playlistTrackID, queueTrackID string) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO playlists (id, user_id, name) VALUES ('playlist-1', 'user-1', 'Cutover Playlist')`); err != nil {
		t.Fatalf("seed Playlist: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES ('playlist-1', ?, 1)`, playlistTrackID); err != nil {
		t.Fatalf("seed Playlist reference: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO playback_queue (id, user_id, position, track_id) VALUES ('queue-item-1', 'user-1', 1, ?)`, queueTrackID); err != nil {
		t.Fatalf("seed Queue reference: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO playback_queue_state (user_id, revision, event_sequence) VALUES ('user-1', 1, 1)
		ON CONFLICT(user_id) DO NOTHING`); err != nil {
		t.Fatalf("seed Queue state: %v", err)
	}
}

func assertMigratedCutoverTrack(t *testing.T, database *sql.DB, storagePath, trackID, contentSHA256 string, contents []byte) {
	t.Helper()
	var missingAt sql.NullTime
	var isPendingCommit int
	var sourceKind, filePath, actualContentSHA256 string
	if err := database.QueryRow(`
		SELECT tracks.missing_at, tracks.is_pending_commit, track_sources.source_kind, track_sources.file_path, track_sources.content_sha256
		FROM tracks JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ?`, trackID).Scan(&missingAt, &isPendingCommit, &sourceKind, &filePath, &actualContentSHA256); err != nil {
		t.Fatalf("read migrated Track %q: %v", trackID, err)
	}
	if missingAt.Valid || isPendingCommit != 0 {
		t.Fatalf("migrated Track %q is not active: missing_at = %+v, is_pending_commit = %d", trackID, missingAt, isPendingCommit)
	}
	if sourceKind != "managed" || actualContentSHA256 != contentSHA256 {
		t.Fatalf("migrated Track source = (%q, %q), want (managed, %q)", sourceKind, actualContentSHA256, contentSHA256)
	}
	canonicalRoot := filepath.Join(storagePath, "library")
	if !isPathWithin(canonicalRoot, filePath) {
		t.Fatalf("migrated Track path %q is outside %q", filePath, canonicalRoot)
	}
	managedBytes, err := os.ReadFile(filePath)
	if err != nil || string(managedBytes) != string(contents) {
		t.Fatalf("managed bytes = %q, error = %v", managedBytes, err)
	}
}

func findCutoverFile(t *testing.T, cutover MigrationCutover, trackID string) MigrationCutoverFile {
	t.Helper()
	for _, file := range cutover.Files {
		if file.TrackID == trackID {
			return file
		}
	}
	t.Fatalf("migration cutover does not contain Track %q", trackID)
	return MigrationCutoverFile{}
}

func TestLibraryMigrationCutoverActivatesVerifiedCopiesAndDropsLegacyReferences(t *testing.T) {
	firstContents := []byte("cutover legacy audio one")
	secondContents := []byte("cutover legacy audio two")
	fixture := newCutoverFixture(t)
	firstPath, firstTrackID, albumID := fixture.seedLegacyTrack(t, "one.flac", "Cutover One", firstContents, 1)
	secondPath, secondTrackID, _ := fixture.seedLegacyTrack(t, "two.flac", "Cutover Two", secondContents, 2)
	fixture.inspect(firstContents, "Cutover One", 1)
	fixture.inspect(secondContents, "Cutover Two", 2)
	seedCutoverReferences(t, fixture.database, firstTrackID, secondTrackID)

	stage := fixture.stage(t)
	if stage.VerifiedCount != 2 || stage.RejectedCount != 0 || stage.FailedCount != 0 {
		t.Fatalf("migration stage = %+v", stage)
	}
	firstPendingID := findMigrationStageFile(t, stage, firstTrackID).PendingTrackID
	secondPendingID := findMigrationStageFile(t, stage, secondTrackID).PendingTrackID

	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 2 || cutover.RejectedCount != 0 || cutover.FailedCount != 0 || cutover.NotAttemptedCount != 0 || len(cutover.Files) != 2 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	firstFile := findCutoverFile(t, cutover, firstTrackID)
	secondFile := findCutoverFile(t, cutover, secondTrackID)
	if firstFile.State != MIGRATION_CUTOVER_MIGRATED || firstFile.CreatedTrackID != firstPendingID || firstFile.ContentSHA256 != migrationTestSHA256(firstContents) {
		t.Fatalf("first cutover file = %+v", firstFile)
	}
	if secondFile.State != MIGRATION_CUTOVER_MIGRATED || secondFile.CreatedTrackID != secondPendingID || secondFile.ContentSHA256 != migrationTestSHA256(secondContents) {
		t.Fatalf("second cutover file = %+v", secondFile)
	}

	assertMigratedCutoverTrack(t, fixture.database, fixture.storage, firstPendingID, migrationTestSHA256(firstContents), firstContents)
	assertMigratedCutoverTrack(t, fixture.database, fixture.storage, secondPendingID, migrationTestSHA256(secondContents), secondContents)
	if count := fixture.count(t, `SELECT COUNT(*) FROM tracks WHERE id IN (?, ?)`, firstTrackID, secondTrackID); count != 0 {
		t.Fatalf("legacy Track rows survived the cutover: %d", count)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM track_sources WHERE track_id IN (?, ?)`, firstTrackID, secondTrackID); count != 0 {
		t.Fatalf("legacy Track sources survived the cutover: %d", count)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM playlist_tracks WHERE track_id = ?`, firstTrackID); count != 0 {
		t.Fatalf("Playlist references survived the cutover: %d", count)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM playback_queue WHERE track_id = ?`, secondTrackID); count != 0 {
		t.Fatalf("Queue references survived the cutover: %d", count)
	}
	var revision, sequence int
	if err := fixture.database.QueryRow(`SELECT revision, event_sequence FROM playback_queue_state WHERE user_id = 'user-1'`).Scan(&revision, &sequence); err != nil {
		t.Fatalf("read Queue state: %v", err)
	}
	if revision != 2 || sequence != 2 {
		t.Fatalf("Queue state = (revision %d, sequence %d), want (2, 2)", revision, sequence)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM legacy_migration_copies`); count != 0 {
		t.Fatalf("migration copies survived the cutover: %d", count)
	}

	fixture.requireActiveTrackCount(t, albumID, 1, 1)
	fixture.requireActiveTrackCount(t, albumID, 2, 1)
	if count := fixture.count(t, `SELECT COUNT(*) FROM tracks WHERE album_id = ?`, albumID); count != 2 {
		t.Fatalf("cutover Album Track count = %d, want 2", count)
	}
	var artworkTrackID, artworkSHA256, artworkPath string
	if err := fixture.database.QueryRow(`SELECT source_track_id, content_sha256, file_path FROM album_artwork WHERE album_id = ?`, albumID).Scan(&artworkTrackID, &artworkSHA256, &artworkPath); err != nil {
		t.Fatalf("read migrated Album Artwork: %v", err)
	}
	if artworkTrackID != firstPendingID && artworkTrackID != secondPendingID {
		t.Fatalf("Album Artwork source Track %q is not one of the migrated Tracks", artworkTrackID)
	}
	if artworkSHA256 != migrationTestSHA256([]byte(cutoverArtworkData)) {
		t.Fatalf("Album Artwork hash = %q", artworkSHA256)
	}
	if !isPathWithin(filepath.Join(fixture.storage, "library"), artworkPath) {
		t.Fatalf("Album Artwork path %q is not canonical", artworkPath)
	}
	if artworkBytes, err := os.ReadFile(artworkPath); err != nil || string(artworkBytes) != cutoverArtworkData {
		t.Fatalf("Album Artwork bytes = %q, error = %v", artworkBytes, err)
	}

	for _, sourcePath := range []string{firstPath, secondPath} {
		if sourceBytes, err := os.ReadFile(sourcePath); err != nil {
			t.Fatalf("legacy source disappeared: %v", err)
		} else if sourcePath == firstPath && string(sourceBytes) != string(firstContents) {
			t.Fatalf("legacy source bytes = %q", sourceBytes)
		}
	}
	migrationEntries, err := os.ReadDir(filepath.Join(fixture.storage, ".migration"))
	if err == nil && len(migrationEntries) > 0 {
		t.Fatalf("pending migration artifacts survived the cutover: %+v", migrationEntries)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read pending migration directory: %v", err)
	}

	fixture.requireNoForeignKeyViolations(t)

	if response := testutil.ServeRequest(t, fixture.router, http.MethodGet, "/api/v1/library/tracks/"+firstPendingID, nil, nil); response.Code != http.StatusOK {
		t.Fatalf("migrated Track detail status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := testutil.ServeRequest(t, fixture.router, http.MethodGet, "/api/v1/library/tracks/"+firstTrackID, nil, nil); response.Code != http.StatusNotFound {
		t.Fatalf("legacy Track detail status = %d, body = %s", response.Code, response.Body.String())
	}
	streamResponse := testutil.ServeRequest(t, fixture.router, http.MethodGet, "/api/v1/tracks/"+firstPendingID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK || streamResponse.Body.String() != string(firstContents) {
		t.Fatalf("migrated Track stream status = %d, body = %q", streamResponse.Code, streamResponse.Body.String())
	}
}

func TestLibraryMigrationCutoverPublishesAffectedQueueInvalidation(t *testing.T) {
	contents := []byte("cutover legacy audio queue")
	fixture := newCutoverFixture(t, playback.NewQueueEventBroker())
	_, queueTrackID, _ := fixture.seedLegacyTrack(t, "queue.flac", "Cutover Queue", contents, 1)
	fixture.inspect(contents, "Cutover Queue", 1)
	seedCutoverReferences(t, fixture.database, queueTrackID, queueTrackID)
	fixture.stage(t)
	if fixture.queueEvent == nil {
		t.Fatal("queue event broker was not wired into the cutover fixture")
	}
	events, unsubscribe := fixture.queueEvent.Subscribe("user-1")
	t.Cleanup(unsubscribe)
	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 1 || len(cutover.Files) != 1 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	select {
	case event := <-events:
		if event.Revision != "2" || event.Sequence != "2" {
			t.Fatalf("Queue invalidation = %+v", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("affected Queue invalidation was not published")
	}
}

func TestLibraryMigrationCutoverIsIdempotent(t *testing.T) {
	contents := []byte("cutover legacy audio idempotent")
	fixture := newCutoverFixture(t)
	_, _, _ = fixture.seedLegacyTrack(t, "idempotent.flac", "Cutover Once", contents, 1)
	fixture.inspect(contents, "Cutover Once", 1)
	fixture.stage(t)
	first := fixture.cutover(t)
	if first.MigratedCount != 1 {
		t.Fatalf("first migration cutover = %+v", first)
	}
	second := fixture.cutover(t)
	if second.MigratedCount != 0 || second.RejectedCount != 0 || second.FailedCount != 0 || second.NotAttemptedCount != 0 || len(second.Files) != 0 {
		t.Fatalf("second migration cutover = %+v", second)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM track_sources WHERE source_kind = 'managed'`); count != 1 {
		t.Fatalf("managed Track count after repeated cutover = %d, want 1", count)
	}
	fixture.requireNoForeignKeyViolations(t)
}

func TestLibraryMigrationCutoverReportsRejectedSources(t *testing.T) {
	acceptedContents := []byte("cutover accepted audio")
	rejectedContents := []byte("cutover rejected audio")
	fixture := newCutoverFixture(t)
	_, acceptedTrackID, _ := fixture.seedLegacyTrack(t, "accepted.flac", "Cutover Accepted", acceptedContents, 1)
	rejectedPath, rejectedTrackID, _ := fixture.seedLegacyTrack(t, "rejected.flac", "Cutover Rejected", rejectedContents, 2)
	fixture.inspect(acceptedContents, "Cutover Accepted", 1)
	fixture.rejectInspection(rejectedPath, &library.InspectionError{
		Code:   library.INSPECTION_ERROR_MISSING_ARTWORK,
		Field:  "artwork",
		Reason: "embedded front-cover artwork is required",
		Err:    errors.New("missing artwork"),
	})

	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 1 || cutover.RejectedCount != 1 || cutover.FailedCount != 0 || cutover.NotAttemptedCount != 0 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	rejectedFile := findCutoverFile(t, cutover, rejectedTrackID)
	if rejectedFile.State != MIGRATION_CUTOVER_REJECTED || rejectedFile.ErrorCode != string(library.INSPECTION_ERROR_MISSING_ARTWORK) {
		t.Fatalf("rejected cutover file = %+v", rejectedFile)
	}
	assertLegacySource(t, fixture.database, rejectedTrackID, rejectedPath)
	if sourceBytes, err := os.ReadFile(rejectedPath); err != nil || string(sourceBytes) != string(rejectedContents) {
		t.Fatalf("rejected source bytes = %q, error = %v", sourceBytes, err)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM legacy_migration_copies WHERE source_track_id = ?`, rejectedTrackID); count != 0 {
		t.Fatalf("rejected source produced a migration copy")
	}
	assertMigratedCutoverTrack(t, fixture.database, fixture.storage, findCutoverFile(t, cutover, acceptedTrackID).CreatedTrackID, migrationTestSHA256(acceptedContents), acceptedContents)
}

func TestLibraryMigrationCutoverSkipsInactiveLegacySource(t *testing.T) {
	contents := []byte("cutover legacy audio inactive")
	fixture := newCutoverFixture(t)
	_, trackID, _ := fixture.seedLegacyTrack(t, "inactive.flac", "Cutover Inactive", contents, 1)
	fixture.inspect(contents, "Cutover Inactive", 1)
	fixture.stage(t)
	if _, err := fixture.database.Exec(`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, trackID); err != nil {
		t.Fatalf("hide legacy Track: %v", err)
	}

	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 0 || cutover.RejectedCount != 0 || cutover.FailedCount != 0 || cutover.NotAttemptedCount != 1 || len(cutover.Files) != 1 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	file := findCutoverFile(t, cutover, trackID)
	if file.State != MIGRATION_CUTOVER_NOT_ATTEMPTED || file.ErrorCode != ERROR_CODE_MIGRATION_SOURCE_INACTIVE || file.CreatedTrackID != "" {
		t.Fatalf("not-attempted cutover file = %+v", file)
	}
	var status string
	if err := fixture.database.QueryRow(`SELECT status FROM legacy_migration_copies WHERE source_track_id = ?`, trackID).Scan(&status); err != nil {
		t.Fatalf("read migration copy: %v", err)
	}
	if status != "verified" {
		t.Fatalf("migration copy status = %q, want verified", status)
	}
	if _, err := os.Stat(fixture.pendingAudioPath(t, trackID)); err != nil {
		t.Fatalf("pending migration copy changed: %v", err)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM track_sources WHERE source_kind = 'managed'`); count != 0 {
		t.Fatalf("inactive cutover activated managed content: %d", count)
	}
}

func TestLibraryMigrationCutoverReportsFailedActivationAndKeepsLegacyTrack(t *testing.T) {
	contents := []byte("cutover legacy audio failure")
	fixture := newCutoverFixture(t)
	path, trackID, _ := fixture.seedLegacyTrack(t, "failure.flac", "Cutover Failure", contents, 1)
	fixture.inspect(contents, "Cutover Failure", 1)
	fixture.stage(t)
	if err := os.Remove(fixture.pendingAudioPath(t, trackID)); err != nil {
		t.Fatalf("remove pending migration audio: %v", err)
	}

	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 0 || cutover.FailedCount != 1 || cutover.NotAttemptedCount != 0 || len(cutover.Files) != 1 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	file := findCutoverFile(t, cutover, trackID)
	if file.State != MIGRATION_CUTOVER_FAILED || file.ErrorReason == "" || file.CreatedTrackID != "" {
		t.Fatalf("failed cutover file = %+v", file)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM tracks WHERE id = ? AND missing_at IS NULL`, trackID); count != 1 {
		t.Fatalf("legacy Track did not survive the failed cutover intact")
	}
	var status string
	if err := fixture.database.QueryRow(`SELECT status FROM legacy_migration_copies WHERE source_track_id = ?`, trackID).Scan(&status); err != nil {
		t.Fatalf("read migration copy: %v", err)
	}
	if status != "verified" {
		t.Fatalf("migration copy status = %q, want verified", status)
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM track_sources WHERE source_kind = 'managed'`); count != 0 {
		t.Fatalf("failed cutover activated managed content: %d", count)
	}
	assertLegacySource(t, fixture.database, trackID, path)
}

func TestLibraryMigrationCutoverKeepsSiblingWhenOneActivationFails(t *testing.T) {
	firstContents := []byte("cutover legacy sibling one")
	secondContents := []byte("cutover legacy sibling two")
	fixture := newCutoverFixture(t)
	_, firstTrackID, albumID := fixture.seedLegacyTrack(t, "sibling-one.flac", "Cutover Sibling One", firstContents, 1)
	_, secondTrackID, _ := fixture.seedLegacyTrack(t, "sibling-two.flac", "Cutover Sibling Two", secondContents, 2)
	fixture.inspect(firstContents, "Cutover Sibling One", 1)
	fixture.inspect(secondContents, "Cutover Sibling Two", 2)
	fixture.stage(t)
	if err := os.Remove(fixture.pendingAudioPath(t, secondTrackID)); err != nil {
		t.Fatalf("remove sibling pending audio: %v", err)
	}

	cutover := fixture.cutover(t)
	if cutover.MigratedCount != 1 || cutover.FailedCount != 1 || len(cutover.Files) != 2 {
		t.Fatalf("migration cutover = %+v", cutover)
	}
	firstFile := findCutoverFile(t, cutover, firstTrackID)
	secondFile := findCutoverFile(t, cutover, secondTrackID)
	if firstFile.State != MIGRATION_CUTOVER_MIGRATED || firstFile.CreatedTrackID == "" {
		t.Fatalf("first sibling cutover file = %+v", firstFile)
	}
	if secondFile.State != MIGRATION_CUTOVER_FAILED {
		t.Fatalf("second sibling cutover file = %+v", secondFile)
	}
	assertMigratedCutoverTrack(t, fixture.database, fixture.storage, firstFile.CreatedTrackID, migrationTestSHA256(firstContents), firstContents)
	if count := fixture.count(t, `SELECT COUNT(*) FROM tracks WHERE id = ? AND missing_at IS NULL`, secondTrackID); count != 1 {
		t.Fatalf("failed sibling legacy Track did not remain active")
	}
	if count := fixture.count(t, `SELECT COUNT(*) FROM tracks WHERE album_id = ?`, albumID); count != 2 {
		t.Fatalf("Album Track count after partial cutover = %d, want 2", count)
	}
	fixture.requireNoForeignKeyViolations(t)
}

func TestLibraryMigrationCutoverRequiresApplicationHeader(t *testing.T) {
	fixture := newCutoverFixture(t)
	response := testutil.ServeRequest(t, fixture.router, http.MethodPost, "/api/v1/library-migrations/cutover", nil, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("migration cutover status without header = %d, body = %s", response.Code, response.Body.String())
	}
	assertNoMigrationMutation(t, fixture.database, fixture.storage)
}
