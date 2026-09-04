package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	LEGACY_MIGRATION_VERSION           = 13
	STRICT_IDENTITY_VERSION            = 14
	BACKFILL_MIGRATION_VERSION         = 15
	MANAGED_IMPORT_VERSION             = 16
	VALIDATION_PROGRESS_VERSION        = 17
	IMPORT_CLIENT_FILE_VERSION         = 19
	COMMIT_JOURNAL_VERSION             = 20
	IMPORT_HISTORY_VERSION             = 21
	LEGACY_MIGRATION_COPY_VERSION      = 22
	TRACK_DELETION_VERSION             = 23
	TRACK_REPLACEMENT_VERSION          = 24
	MIGRATION_CUTOVER_RECOVERY_VERSION = 25
	LEGACY_MIGRATION_SOURCES_VERSION   = 26
	RETIRE_LEGACY_SCAN_JOBS_VERSION    = 27
	REMOVE_LEGACY_LIBRARY_VERSION      = 28
)

func TestPermanentTrackDeletionMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_COPY_VERSION)
	if err := goose.UpTo(sqlDB, migrationsDir(t), TRACK_DELETION_VERSION); err != nil {
		t.Fatalf("apply Permanent Track Deletion migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, TRACK_DELETION_VERSION)
	assertTableExists(t, sqlDB, "permanent_track_deletions")

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Permanent Track Deletion migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, LEGACY_MIGRATION_COPY_VERSION)
	assertTableMissing(t, sqlDB, "permanent_track_deletions")
}

func TestTrackReplacementMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, TRACK_DELETION_VERSION)
	if err := goose.UpTo(sqlDB, migrationsDir(t), TRACK_REPLACEMENT_VERSION); err != nil {
		t.Fatalf("apply Track Replacement migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, TRACK_REPLACEMENT_VERSION)
	assertTableExists(t, sqlDB, "managed_track_replacements")
	if _, err := sqlDB.Exec(`INSERT INTO managed_import_jobs (id, status, revision, replace_track_id) VALUES ('job-1', 'uploading', 1, 'track-1')`); err != nil {
		t.Fatalf("insert Track Replacement job: %v", err)
	}

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Track Replacement migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, TRACK_DELETION_VERSION)
	assertTableMissing(t, sqlDB, "managed_track_replacements")
}

func TestManagedImportCommitJournalMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, IMPORT_CLIENT_FILE_VERSION)
	if err := goose.UpTo(sqlDB, migrationsDir(t), COMMIT_JOURNAL_VERSION); err != nil {
		t.Fatalf("apply Managed Import commit journal migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, COMMIT_JOURNAL_VERSION)
	assertTableExists(t, sqlDB, "managed_import_commit_journal")
	assertColumnExists(t, sqlDB, "tracks", "is_pending_commit")
	insertLegacyLibrary(t, sqlDB)
	assertExecFails(t, sqlDB, `UPDATE tracks SET is_pending_commit = 2`, "CHECK constraint failed")

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Managed Import commit journal migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, IMPORT_CLIENT_FILE_VERSION)
	assertTableMissing(t, sqlDB, "managed_import_commit_journal")
	assertColumnMissing(t, sqlDB, "tracks", "is_pending_commit")
}

func TestManagedImportHistoryMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, COMMIT_JOURNAL_VERSION)
	insertLegacyLibrary(t, sqlDB)
	_, err := sqlDB.Exec(`
		INSERT INTO managed_import_jobs (
			id, status, revision, original_filename, content_sha256, track_id,
			created_at, updated_at
		) VALUES (
			'standalone-import', 'committed', 1, 'standalone.flac',
			'0000000000000000000000000000000000000000000000000000000000000000',
			'track-1', '2026-01-01 00:00:00', '2026-01-01 00:01:00'
		);
		INSERT INTO managed_import_batches (id, status, revision, created_at, updated_at)
		VALUES ('batch-import', 'completed', 2, '2026-01-02 00:00:00', '2026-01-02 00:01:00');
		INSERT INTO managed_import_jobs (
			id, status, revision, original_filename, error_code, batch_id, outcome,
			selected, batch_position, created_at, updated_at, client_file_id
		) VALUES (
			'batch-job', 'failed', 1, 'broken.flac', 'invalid_upload', 'batch-import',
			'rejected', 0, 1, '2026-01-02 00:00:00', '2026-01-02 00:00:30',
			'00000000-0000-4000-8000-000000000001'
		);
	`)
	if err != nil {
		t.Fatalf("store terminal Managed Import fixtures: %v", err)
	}
	if migrationErr := goose.UpTo(sqlDB, migrationsDir(t), IMPORT_HISTORY_VERSION); migrationErr != nil {
		t.Fatalf("apply Import History migration: %v", migrationErr)
	}
	assertRowCount(t, sqlDB, "managed_import_history", 2)
	assertRowCount(t, sqlDB, "managed_import_history_files", 2)
	assertTextValue(t, sqlDB, `SELECT result_code FROM managed_import_history WHERE import_id = 'standalone-import'`, "completed")
	assertTextValue(t, sqlDB, `SELECT created_track_id FROM managed_import_history_files WHERE import_id = 'standalone-import'`, "track-1")
	assertTextValue(t, sqlDB, `SELECT result_code FROM managed_import_history_files WHERE import_id = 'batch-import'`, "invalid_upload")

	_, err = sqlDB.Exec(`
		INSERT INTO managed_import_history (
			import_id, started_at, completed_at, result_code, total_count, imported_count,
			rejected_count, failed_count, replaced_count, not_attempted_count, canceled_count
		) VALUES ('import-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'completed', 1, 1, 0, 0, 0, 0, 0);
		INSERT INTO managed_import_history_files (
			import_id, file_id, job_id, started_at, completed_at, content_sha256, result_code, position
		) VALUES ('import-1', 'file-1', 'job-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
			'0000000000000000000000000000000000000000000000000000000000000000', 'imported', 0);
	`)
	if err != nil {
		t.Fatalf("store Import History fixture: %v", err)
	}
	assertRowCount(t, sqlDB, "managed_import_history", 3)
	assertRowCount(t, sqlDB, "managed_import_history_files", 3)
	assertExecFails(t, sqlDB, `UPDATE managed_import_history SET total_count = 2 WHERE import_id = 'import-1'`, "CHECK constraint failed")

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Import History migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, COMMIT_JOURNAL_VERSION)
	assertTableMissing(t, sqlDB, "managed_import_history")
	assertTableMissing(t, sqlDB, "managed_import_history_files")
	assertTableMissing(t, sqlDB, "managed_import_canceled_files")
}

func TestManagedImportValidationProgressMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, MANAGED_IMPORT_VERSION)
	if err := goose.UpTo(sqlDB, migrationsDir(t), VALIDATION_PROGRESS_VERSION); err != nil {
		t.Fatalf("apply Managed Import validation progress migration: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO managed_import_jobs (id, status, revision) VALUES ('import-1', 'uploading', 1)`); err != nil {
		t.Fatalf("create Managed Import Job: %v", err)
	}
	assertIntegerValue(t, sqlDB, `SELECT validation_progress FROM managed_import_jobs WHERE id = 'import-1'`, 0)
	assertExecFails(t, sqlDB, `UPDATE managed_import_jobs SET validation_progress = 101 WHERE id = 'import-1'`, "CHECK constraint failed")

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Managed Import validation progress migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, MANAGED_IMPORT_VERSION)
	assertColumnMissing(t, sqlDB, "managed_import_jobs", "validation_progress")
}

func TestManagedImportMigrationAppliesAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, BACKFILL_MIGRATION_VERSION)
	if err := goose.UpTo(sqlDB, migrationsDir(t), MANAGED_IMPORT_VERSION); err != nil {
		t.Fatalf("apply Managed Import migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, MANAGED_IMPORT_VERSION)
	if _, err := sqlDB.Exec(`INSERT INTO managed_import_jobs (id, status, revision) VALUES ('import-1', 'uploading', 1)`); err != nil {
		t.Fatalf("create Managed Import Job: %v", err)
	}
	assertRowCount(t, sqlDB, "managed_import_jobs", 1)
	assertExecFails(t, sqlDB, `UPDATE managed_import_jobs SET status = 'awaiting_confirmation' WHERE id = 'import-1'`, "CHECK constraint failed")

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back Managed Import migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, BACKFILL_MIGRATION_VERSION)
	assertTableMissing(t, sqlDB, "managed_import_jobs")
}

func TestStrictTrackIdentityMigrationAppliesAndRollsBackOnEmptyDatabase(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)

	assertMigrationVersion(t, sqlDB, STRICT_IDENTITY_VERSION)
	assertStrictIdentitySchema(t, sqlDB)

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, LEGACY_MIGRATION_VERSION)
	assertTableMissing(t, sqlDB, "track_sources")
	assertColumnMissing(t, sqlDB, "tracks", "disc_no")

	if err := goose.UpTo(sqlDB, migrationsDir(t), STRICT_IDENTITY_VERSION); err != nil {
		t.Fatalf("reapply strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, STRICT_IDENTITY_VERSION)
	assertStrictIdentitySchema(t, sqlDB)
}

func TestStrictTrackIdentityMigrationPreservesPopulatedLegacyDatabase(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, sqlDB)

	if err := goose.UpTo(sqlDB, migrationsDir(t), STRICT_IDENTITY_VERSION); err != nil {
		t.Fatalf("expand populated database: %v", err)
	}
	assertLegacyTrack(t, sqlDB)
	assertStrictIdentitySchema(t, sqlDB)

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back populated database: %v", err)
	}
	assertLegacyTrack(t, sqlDB)
	assertTableMissing(t, sqlDB, "track_sources")
}

func TestStrictTrackIdentitySchemaEnforcesIdentityAndConcurrencyConstraints(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)
	insertLegacyLibrary(t, sqlDB)

	assertExecFails(t, sqlDB, invalidTrackTotalsUpdate(), "invalid strict Track position totals")
	storeStrictTrackIdentity(t, sqlDB)
	insertSecondAlbumTrack(t, sqlDB)
	assertDuplicateTrackHashRejected(t, sqlDB)
	assertExecFails(t, sqlDB, `
		UPDATE tracks
		SET album_id = 'album-1', disc_no = NULL, track_no = 1, identity_key = 'track-two'
		WHERE id = 'track-2'
	`, "idx_tracks_active_album_position")
	assertOrderedRelationshipsRejectPositionConflicts(t, sqlDB)
	assertReleaseIdentifiersRejectConcurrentMatches(t, sqlDB)
}

func invalidTrackTotalsUpdate() string {
	return `
		UPDATE tracks
		SET disc_no = 2, track_total = 1, disc_total = 1
		WHERE id = 'track-1'
	`
}

func storeStrictTrackIdentity(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(`
		UPDATE tracks
		SET disc_no = 1, track_total = 1, disc_total = 1,
			channel_count = 2, bitrate_bps = 800000, codec = 'flac',
			container = 'flac', sample_format = 's24', identity_key = 'track'
		WHERE id = 'track-1';
		INSERT INTO track_sources (
			id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes
		) VALUES (
			'source-1', 'track-1', 'managed', '/managed/track-1.flac',
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'flac', 100
		);
	`)
	if err != nil {
		t.Fatalf("store strict Track identity: %v", err)
	}
}

func assertDuplicateTrackHashRejected(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	assertExecFails(t, sqlDB, `
		INSERT INTO track_sources (
			id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes
		) VALUES (
			'source-2', 'track-2', 'managed', '/managed/track-2.flac',
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'flac', 100
		)
	`, "track_sources.content_sha256")
	assertExecFails(t, sqlDB, `
		INSERT INTO track_sources (
			id, track_id, source_kind, file_path, source_format, size_bytes
		) VALUES (
			'source-2', 'track-2', 'managed', '/managed/track-2.flac', 'flac', 100
		)
	`, "managed_source_hash_required")
}

func TestStrictTrackIdentitySchemaRequiresArtworkSourceFromSameAlbum(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)
	insertLegacyLibrary(t, sqlDB)
	insertSecondAlbumTrack(t, sqlDB)

	assertExecFails(t, sqlDB, albumArtworkInsert("track-2"), "Album Artwork source Track must belong to the Album")
	if _, err := sqlDB.Exec(albumArtworkInsert("track-1")); err != nil {
		t.Fatalf("store Album Artwork metadata: %v", err)
	}
	assertExecFails(t, sqlDB, `UPDATE tracks SET album_id = 'album-2' WHERE id = 'track-1'`, "Album Artwork source Track must remain in the Album")
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migrations test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations"))
}

func openDatabaseAtVersion(t *testing.T, version int64) *sql.DB {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "populated.db")
	return openDatabasePathAtVersion(t, databasePath, version)
}

func openDatabasePathAtVersion(t *testing.T, databasePath string, version int64) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", databasePath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	registerDatabaseCleanup(t, sqlDB)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.UpTo(sqlDB, migrationsDir(t), version); err != nil {
		t.Fatalf("migrate database to version %d: %v", version, err)
	}
	return sqlDB
}

func insertLegacyLibrary(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(`
		INSERT INTO artists (id, name, name_sort) VALUES ('artist-1', 'Artist', 'artist');
		INSERT INTO albums (id, artist_id, title, title_sort) VALUES ('album-1', 'artist-1', 'Album', 'album');
		INSERT INTO tracks (
			id, album_id, title, title_sort, artist_name, track_no, duration_ms,
			format, size_bytes, file_path, file_mtime
		) VALUES (
			'track-1', 'album-1', 'Track', 'track', 'Artist', 1, 1000,
			'flac', 100, '/music/track.flac', 1
		);
	`)
	if err != nil {
		t.Fatalf("insert legacy library: %v", err)
	}
}

func insertSecondAlbumTrack(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(`
		INSERT INTO albums (id, artist_id, title, title_sort) VALUES ('album-2', 'artist-1', 'Album Two', 'album two');
		INSERT INTO tracks (
			id, album_id, title, title_sort, artist_name, track_no, disc_no,
			duration_ms, format, size_bytes, file_path, file_mtime
		) VALUES (
			'track-2', 'album-2', 'Track Two', 'track two', 'Artist', 1, 1,
			1000, 'flac', 100, '/music/track-2.flac', 1
		);
	`)
	if err != nil {
		t.Fatalf("insert second Album and Track: %v", err)
	}
}

func assertOrderedRelationshipsRejectPositionConflicts(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(`
		INSERT INTO artists (id, name, name_sort) VALUES ('artist-2', 'Artist Two', 'artist two');
		INSERT INTO track_artists (track_id, artist_id, position) VALUES ('track-1', 'artist-1', 0);
		INSERT INTO album_artists (album_id, artist_id, position) VALUES ('album-1', 'artist-1', 0);
		INSERT INTO genres (id, name, name_normalized) VALUES ('genre-1', 'Rock', 'rock');
		INSERT INTO track_genres (track_id, genre_id, position) VALUES ('track-1', 'genre-1', 0);
	`)
	if err != nil {
		t.Fatalf("store ordered relationships: %v", err)
	}
	assertExecFails(t, sqlDB, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('track-1', 'artist-2', 0)`, "track_artists.track_id, track_artists.position")
	assertExecFails(t, sqlDB, `INSERT INTO album_artists (album_id, artist_id, position) VALUES ('album-1', 'artist-2', 0)`, "album_artists.album_id, album_artists.position")
	assertExecFails(t, sqlDB, `INSERT INTO genres (id, name, name_normalized) VALUES ('genre-2', 'ROCK', 'rock')`, "genres.name_normalized")
}

func assertReleaseIdentifiersRejectConcurrentMatches(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(`
		INSERT INTO album_release_identifiers (album_id, scheme, value)
		VALUES ('album-1', 'musicbrainz_release', 'release-1')
	`)
	if err != nil {
		t.Fatalf("store Album release identifier: %v", err)
	}
	assertExecFails(t, sqlDB, `
		INSERT INTO album_release_identifiers (album_id, scheme, value)
		VALUES ('album-2', 'musicbrainz_release', 'release-1')
	`, "album_release_identifiers.scheme, album_release_identifiers.value")
}

func assertExecFails(t *testing.T, sqlDB *sql.DB, statement string, expectedError string) {
	t.Helper()
	_, err := sqlDB.Exec(statement)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded: %s", statement)
	}
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("statement error = %q, want error containing %q", err, expectedError)
	}
}

func albumArtworkInsert(sourceTrackID string) string {
	return fmt.Sprintf(`
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type,
			width, height, encoded_size_bytes, file_path
		) VALUES (
			'artwork-1', 'album-1', '%s',
			'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			'image/jpeg', 1000, 1000, 100000, '/managed/album-1/cover.jpg'
		)
	`, sourceTrackID)
}

func assertLegacyTrack(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	var title string
	var filePath string
	if err := sqlDB.QueryRow(`SELECT title, file_path FROM tracks WHERE id = 'track-1'`).Scan(&title, &filePath); err != nil {
		t.Fatalf("read legacy track: %v", err)
	}
	if title != "Track" || filePath != "/music/track.flac" {
		t.Fatalf("legacy track = (%q, %q), want (%q, %q)", title, filePath, "Track", "/music/track.flac")
	}
}

func assertMigrationVersion(t *testing.T, sqlDB *sql.DB, expected int64) {
	t.Helper()
	version, err := goose.GetDBVersion(sqlDB)
	if err != nil {
		t.Fatalf("get migration version: %v", err)
	}
	if version != expected {
		t.Fatalf("migration version = %d, want %d", version, expected)
	}
}

func assertRowCount(t *testing.T, sqlDB *sql.DB, tableName string, expected int) {
	t.Helper()
	var actual int
	if err := sqlDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&actual); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if actual != expected {
		t.Fatalf("%s row count = %d, want %d", tableName, actual, expected)
	}
}

func assertTextValue(t *testing.T, sqlDB *sql.DB, query string, expected string) {
	t.Helper()
	var actual string
	if err := sqlDB.QueryRow(query).Scan(&actual); err != nil {
		t.Fatalf("query text value: %v", err)
	}
	if actual != expected {
		t.Fatalf("text value = %q, want %q", actual, expected)
	}
}

func assertIntegerValue(t *testing.T, sqlDB *sql.DB, query string, expected int64) {
	t.Helper()
	var actual int64
	if err := sqlDB.QueryRow(query).Scan(&actual); err != nil {
		t.Fatalf("query integer value: %v", err)
	}
	if actual != expected {
		t.Fatalf("integer value = %d, want %d", actual, expected)
	}
}

func assertForeignKeyIntegrity(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("check foreign keys: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close foreign key violations: %v", err)
		}
	}()
	if rows.Next() {
		t.Fatal("foreign key check reported an integrity violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign key violations: %v", err)
	}
}

func assertStrictIdentitySchema(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	for _, tableName := range []string{
		"track_sources",
		"track_artists",
		"album_artists",
		"genres",
		"track_genres",
		"album_release_identifiers",
		"album_artwork",
	} {
		assertTableExists(t, sqlDB, tableName)
	}
	for _, columnName := range []string{
		"disc_no",
		"track_total",
		"disc_total",
		"channel_count",
		"bitrate_bps",
		"codec",
		"container",
		"sample_format",
		"identity_key",
		"revision",
	} {
		assertColumnExists(t, sqlDB, "tracks", columnName)
	}
	for _, columnName := range []string{"identity_key", "release_date", "revision"} {
		assertColumnExists(t, sqlDB, "albums", columnName)
	}
	assertColumnExists(t, sqlDB, "artists", "name_normalized")
}

func assertTableExists(t *testing.T, sqlDB *sql.DB, tableName string) {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count); err != nil {
		t.Fatalf("find table %s: %v", tableName, err)
	}
	if count != 1 {
		t.Fatalf("table %s does not exist", tableName)
	}
}

func assertTableMissing(t *testing.T, sqlDB *sql.DB, tableName string) {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count); err != nil {
		t.Fatalf("find table %s: %v", tableName, err)
	}
	if count != 0 {
		t.Fatalf("table %s still exists", tableName)
	}
}

func assertColumnExists(t *testing.T, sqlDB *sql.DB, tableName string, columnName string) {
	t.Helper()
	if !hasColumn(t, sqlDB, tableName, columnName) {
		t.Fatalf("column %s.%s does not exist", tableName, columnName)
	}
}

func assertColumnMissing(t *testing.T, sqlDB *sql.DB, tableName string, columnName string) {
	t.Helper()
	if hasColumn(t, sqlDB, tableName, columnName) {
		t.Fatalf("column %s.%s still exists", tableName, columnName)
	}
}

func hasColumn(t *testing.T, sqlDB *sql.DB, tableName string, columnName string) bool {
	t.Helper()
	rows, err := sqlDB.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		t.Fatalf("list columns for %s: %v", tableName, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close columns for %s: %v", tableName, err)
		}
	}()
	for rows.Next() {
		var columnID int
		var name string
		var columnType string
		var isNotNull int
		var defaultValue any
		var isPrimaryKey int
		if err := rows.Scan(&columnID, &name, &columnType, &isNotNull, &defaultValue, &isPrimaryKey); err != nil {
			t.Fatalf("scan column for %s: %v", tableName, err)
		}
		if name == columnName {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns for %s: %v", tableName, err)
	}
	return false
}

func registerDatabaseCleanup(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
}

func TestRemoveLegacyLibraryMigrationDeletesLegacyRowsAndRollsBack(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, RETIRE_LEGACY_SCAN_JOBS_VERSION)
	for _, statement := range []string{
		`INSERT INTO artists (id, name, name_sort, name_normalized) VALUES ('artist-managed', 'Managed Artist', 'managed artist', 'managed artist'), ('artist-legacy', 'Legacy Artist', 'legacy artist', 'legacy artist')`,
		`INSERT INTO albums (id, artist_id, title, title_sort, genres) VALUES ('album-managed', 'artist-managed', 'Managed Album', 'managed album', '[]'), ('album-legacy', 'artist-legacy', 'Legacy Album', 'legacy album', '[]')`,
		`INSERT INTO album_artists (album_id, artist_id, position) VALUES ('album-managed', 'artist-managed', 0), ('album-legacy', 'artist-legacy', 0)`,
		`INSERT INTO tracks (id, album_id, title, title_sort, artist_name, duration_ms, format, size_bytes, file_path, file_mtime) VALUES ('track-managed', 'album-managed', 'Kept', 'kept', 'Managed Artist', 1000, 'flac', 10, '/managed/kept.flac', 1)`,
		`INSERT INTO tracks (id, album_id, title, title_sort, artist_name, duration_ms, format, size_bytes, file_path, file_mtime, missing_at) VALUES ('track-legacy', 'album-legacy', 'Retired', 'retired', 'Legacy Artist', 1000, 'flac', 10, '/music/retired.flac', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO track_sources (id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes) VALUES ('source-managed', 'track-managed', 'managed', '/managed/kept.flac', '` + strings.Repeat("a", 64) + `', 'flac', 10)`,
		`INSERT INTO track_sources (id, track_id, source_kind, file_path, source_format, size_bytes) VALUES ('source-legacy', 'track-legacy', 'legacy', '/music/retired.flac', 'flac', 10)`,
		`INSERT INTO track_artists (track_id, artist_id, position) VALUES ('track-managed', 'artist-managed', 0), ('track-legacy', 'artist-legacy', 0)`,
		`INSERT INTO genres (id, name, name_normalized) VALUES ('genre-kept', 'Pop', 'pop'), ('genre-orphan', 'Retired Genre', 'retired genre')`,
		`INSERT INTO track_genres (track_id, genre_id, position) VALUES ('track-managed', 'genre-kept', 0), ('track-legacy', 'genre-orphan', 0)`,
		`INSERT INTO legacy_migration_sources (track_id, source_track_id, source_file_path, source_sha256) VALUES ('track-managed', 'track-legacy', '/music/retired.flac', '` + strings.Repeat("b", 64) + `')`,
	} {
		if _, err := sqlDB.Exec(statement); err != nil {
			t.Fatalf("seed pre-removal library: %v\n%s", err, statement)
		}
	}

	if err := goose.UpTo(sqlDB, migrationsDir(t), REMOVE_LEGACY_LIBRARY_VERSION); err != nil {
		t.Fatalf("apply remove legacy library migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, REMOVE_LEGACY_LIBRARY_VERSION)
	assertRowCount(t, sqlDB, "tracks", 1)
	assertTextValue(t, sqlDB, `SELECT id FROM tracks`, "track-managed")
	assertRowCount(t, sqlDB, "track_sources", 1)
	assertRowCount(t, sqlDB, "albums", 1)
	assertRowCount(t, sqlDB, "artists", 1)
	assertRowCount(t, sqlDB, "genres", 1)
	assertTextValue(t, sqlDB, `SELECT name FROM genres`, "Pop")
	for _, table := range []string{"legacy_migration_copies", "legacy_migration_sources", "legacy_library_backfill_state", "legacy_artist_identities", "legacy_album_identities", "legacy_track_identities", "legacy_album_genres", "legacy_album_artwork_metadata"} {
		assertTableMissing(t, sqlDB, table)
	}
	assertForeignKeyIntegrity(t, sqlDB)

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back remove legacy library migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, RETIRE_LEGACY_SCAN_JOBS_VERSION)
	// The Down step is documented as non-reversible: nothing is recreated.
	assertTableMissing(t, sqlDB, "legacy_migration_sources")
	assertRowCount(t, sqlDB, "tracks", 1)
}
