package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	database "github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	legacyMigrationVersion = 13
	strictIdentityVersion  = 14
)

func TestStrictTrackIdentityMigrationAppliesAndRollsBackOnEmptyDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "empty.db")
	sqlDB, err := database.OpenAndMigrate(context.Background(), databasePath, migrationsDir(t))
	if err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	assertMigrationVersion(t, sqlDB, strictIdentityVersion)
	assertStrictIdentitySchema(t, sqlDB)

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, legacyMigrationVersion)
	assertTableMissing(t, sqlDB, "track_sources")
	assertColumnMissing(t, sqlDB, "tracks", "disc_no")

	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("reapply strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, strictIdentityVersion)
	assertStrictIdentitySchema(t, sqlDB)
}

func TestStrictTrackIdentityMigrationPreservesPopulatedLegacyDatabase(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, legacyMigrationVersion)
	insertLegacyLibrary(t, sqlDB)

	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
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
	sqlDB := openDatabaseAtVersion(t, strictIdentityVersion)
	insertLegacyLibrary(t, sqlDB)

	assertExecFails(t, sqlDB, invalidTrackTotalsUpdate())
	storeStrictTrackIdentity(t, sqlDB)
	insertSecondAlbumTrack(t, sqlDB)
	assertDuplicateTrackHashRejected(t, sqlDB)
	assertExecFails(t, sqlDB, `
		UPDATE tracks SET album_id = 'album-1', disc_no = 1, track_no = 1 WHERE id = 'track-2'
	`)
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
	`)
}

func TestStrictTrackIdentitySchemaRequiresArtworkSourceFromSameAlbum(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, strictIdentityVersion)
	insertLegacyLibrary(t, sqlDB)
	insertSecondAlbumTrack(t, sqlDB)

	assertExecFails(t, sqlDB, albumArtworkInsert("track-2"))
	if _, err := sqlDB.Exec(albumArtworkInsert("track-1")); err != nil {
		t.Fatalf("store Album Artwork metadata: %v", err)
	}
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
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", databasePath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
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
	assertExecFails(t, sqlDB, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('track-1', 'artist-2', 0)`)
	assertExecFails(t, sqlDB, `INSERT INTO album_artists (album_id, artist_id, position) VALUES ('album-1', 'artist-2', 0)`)
	assertExecFails(t, sqlDB, `INSERT INTO genres (id, name, name_normalized) VALUES ('genre-2', 'ROCK', 'rock')`)
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
	`)
}

func assertExecFails(t *testing.T, sqlDB *sql.DB, statement string) {
	t.Helper()
	if _, err := sqlDB.Exec(statement); err == nil {
		t.Fatalf("statement unexpectedly succeeded: %s", statement)
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
	defer func() { _ = rows.Close() }()
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
