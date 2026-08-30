package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	database "github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	LEGACY_MIGRATION_VERSION = 13
	STRICT_IDENTITY_VERSION  = 14
)

func TestStrictTrackIdentityMigrationAppliesAndRollsBackOnEmptyDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "empty.db")
	sqlDB, err := database.OpenAndMigrate(context.Background(), databasePath, migrationsDir(t))
	if err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}
	registerDatabaseCleanup(t, sqlDB)

	assertMigrationVersion(t, sqlDB, STRICT_IDENTITY_VERSION)
	assertStrictIdentitySchema(t, sqlDB)

	if err := goose.Down(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("roll back strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, LEGACY_MIGRATION_VERSION)
	assertTableMissing(t, sqlDB, "track_sources")
	assertColumnMissing(t, sqlDB, "tracks", "disc_no")

	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("reapply strict identity migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, STRICT_IDENTITY_VERSION)
	assertStrictIdentitySchema(t, sqlDB)
}

func TestStrictTrackIdentityMigrationPreservesPopulatedLegacyDatabase(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
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

func TestBackfillExpandedLibraryPopulatesSourcesAndArtistRelationships(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, sqlDB)

	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("expand populated database: %v", err)
	}
	if err := database.BackfillExpandedLibrary(context.Background(), sqlDB); err != nil {
		t.Fatalf("backfill expanded library: %v", err)
	}

	assertRowCount(t, sqlDB, "track_sources", 1)
	assertRowCount(t, sqlDB, "track_artists", 1)
	assertRowCount(t, sqlDB, "album_artists", 1)
	assertTextValue(t, sqlDB, `SELECT source_kind FROM track_sources WHERE track_id = 'track-1'`, "legacy")
	assertTextValue(t, sqlDB, `SELECT file_path FROM track_sources WHERE track_id = 'track-1'`, "/music/track.flac")
	assertTextValue(t, sqlDB, `SELECT name FROM artists
		WHERE id = (SELECT artist_id FROM track_artists WHERE track_id = 'track-1')`, "Artist")
	assertTextValue(t, sqlDB, `SELECT name FROM artists
		WHERE id = (SELECT artist_id FROM album_artists WHERE album_id = 'album-1')`, "Artist")
	assertLegacyTrack(t, sqlDB)
}

func TestBackfillExpandedLibraryPopulatesIdentityGenresAndTechnicalMetadata(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, sqlDB)
	_, err := sqlDB.Exec(`
		UPDATE albums SET year = 2024, genres = '["Rock"]' WHERE id = 'album-1';
		UPDATE tracks SET artist_name = 'Track Artist', genre = 'Rock',
			sample_rate_hz = 96000, bit_depth = 24
		WHERE id = 'track-1';
	`)
	if err != nil {
		t.Fatalf("enrich legacy library: %v", err)
	}
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("expand populated database: %v", err)
	}

	if err := database.BackfillExpandedLibrary(context.Background(), sqlDB); err != nil {
		t.Fatalf("backfill expanded library: %v", err)
	}

	assertRowCount(t, sqlDB, "artists", 2)
	assertRowCount(t, sqlDB, "genres", 1)
	assertRowCount(t, sqlDB, "track_genres", 1)
	assertTextValue(t, sqlDB, `SELECT name_normalized FROM artists WHERE name = 'Track Artist'`, "track artist")
	assertTextValue(t, sqlDB, `SELECT name FROM genres`, "Rock")
	assertTextValue(t, sqlDB, `SELECT identity_key FROM albums WHERE id = 'album-1'`, "artist\x1falbum\x1f2024")
	assertTextValue(t, sqlDB, `SELECT release_date FROM albums WHERE id = 'album-1'`, "2024")
	assertTextValue(t, sqlDB, `SELECT identity_key FROM tracks WHERE id = 'track-1'`, "track")
	assertTextValue(t, sqlDB, `SELECT codec || ':' || container || ':' || sample_format
		FROM tracks WHERE id = 'track-1'`, "flac:flac:s24")
	assertIntegerValue(t, sqlDB, `SELECT bitrate_bps FROM tracks WHERE id = 'track-1'`, 800)
}

func TestBackfillExpandedLibraryPopulatesAlbumArtworkMetadata(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, sqlDB)
	coverData := createLegacyCover(t)
	if _, err := sqlDB.Exec(`UPDATE albums SET cover_mime = 'image/png', cover_data = ? WHERE id = 'album-1'`, coverData); err != nil {
		t.Fatalf("store legacy Album artwork: %v", err)
	}
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("expand populated database: %v", err)
	}

	if err := database.BackfillExpandedLibrary(context.Background(), sqlDB); err != nil {
		t.Fatalf("backfill expanded library: %v", err)
	}

	expectedHash := fmt.Sprintf("%x", sha256.Sum256(coverData))
	assertRowCount(t, sqlDB, "album_artwork", 1)
	assertTextValue(t, sqlDB, `SELECT content_sha256 FROM album_artwork WHERE album_id = 'album-1'`, expectedHash)
	assertTextValue(t, sqlDB, `SELECT media_type FROM album_artwork WHERE album_id = 'album-1'`, "image/png")
	assertTextValue(t, sqlDB, `SELECT file_path FROM album_artwork WHERE album_id = 'album-1'`, "legacy-db://albums/album-1/cover")
	assertTextValue(t, sqlDB, `SELECT source_track_id FROM album_artwork WHERE album_id = 'album-1'`, "track-1")
	assertIntegerValue(t, sqlDB, `SELECT width * height FROM album_artwork WHERE album_id = 'album-1'`, 1)
	assertIntegerValue(t, sqlDB, `SELECT encoded_size_bytes FROM album_artwork WHERE album_id = 'album-1'`, int64(len(coverData)))
}

func TestOpenAndMigrateBackfillsPopulatedLegacyDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB := openDatabasePathAtVersion(t, databasePath, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, legacyDB)
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	sqlDB, err := database.OpenAndMigrate(context.Background(), databasePath, migrationsDir(t))
	if err != nil {
		t.Fatalf("open and migrate populated legacy database: %v", err)
	}
	registerDatabaseCleanup(t, sqlDB)

	assertRowCount(t, sqlDB, "track_sources", 1)
	assertRowCount(t, sqlDB, "track_artists", 1)
	assertRowCount(t, sqlDB, "album_artists", 1)
}

func TestBackfillExpandedLibraryIsIdempotentAndPreservesLegacyReads(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, LEGACY_MIGRATION_VERSION)
	insertLegacyLibrary(t, sqlDB)
	coverData := createLegacyCover(t)
	_, err := sqlDB.Exec(`UPDATE albums SET cover_mime = 'image/png', cover_data = ? WHERE id = 'album-1'`, coverData)
	if err != nil {
		t.Fatalf("store legacy Album artwork: %v", err)
	}
	if _, err := sqlDB.Exec(`UPDATE tracks SET artist_name = '  Track   Artist ', genre = 'Alt   Rock' WHERE id = 'track-1'`); err != nil {
		t.Fatalf("store legacy Track metadata: %v", err)
	}
	readsBefore := captureLegacyLibraryReads(t, sqlDB)
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("expand populated database: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := database.BackfillExpandedLibrary(context.Background(), sqlDB); err != nil {
			t.Fatalf("backfill expanded library run %d: %v", run, err)
		}
	}

	assertRowCount(t, sqlDB, "artists", 2)
	assertRowCount(t, sqlDB, "track_sources", 1)
	assertRowCount(t, sqlDB, "track_artists", 1)
	assertRowCount(t, sqlDB, "album_artists", 1)
	assertRowCount(t, sqlDB, "genres", 1)
	assertRowCount(t, sqlDB, "track_genres", 1)
	assertRowCount(t, sqlDB, "album_artwork", 1)
	assertForeignKeyIntegrity(t, sqlDB)
	if readsAfter := captureLegacyLibraryReads(t, sqlDB); readsAfter != readsBefore {
		t.Fatalf("legacy library reads changed after backfill\nbefore: %s\nafter:  %s", readsBefore, readsAfter)
	}
}

func TestBackfillExpandedLibraryRollsBackWhenExpandedRowsAreIncomplete(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)
	insertLegacyLibrary(t, sqlDB)
	insertSecondAlbumTrack(t, sqlDB)
	_, err := sqlDB.Exec(`INSERT INTO track_sources (
		id, track_id, source_kind, file_path, source_format, size_bytes
	) VALUES (
		'legacy-source:track-1', 'track-2', 'legacy', '/music/track-2.flac', 'flac', 100
	)`)
	if err != nil {
		t.Fatalf("store conflicting expanded Track source: %v", err)
	}

	err = database.BackfillExpandedLibrary(context.Background(), sqlDB)
	if err == nil || !strings.Contains(err.Error(), "Track source") {
		t.Fatalf("backfill error = %v, want incomplete Track source error", err)
	}

	assertIntegerValue(t, sqlDB, `SELECT COUNT(*) FROM track_sources WHERE track_id = 'track-1'`, 0)
	assertIntegerValue(t, sqlDB, `SELECT COUNT(*) FROM artists WHERE name_normalized IS NOT NULL`, 0)
}

func TestBackfillExpandedLibraryRollsBackInvalidLegacyArtwork(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)
	insertLegacyLibrary(t, sqlDB)
	if _, err := sqlDB.Exec(`UPDATE albums SET cover_mime = 'image/png', cover_data = x'010203' WHERE id = 'album-1'`); err != nil {
		t.Fatalf("store invalid legacy Album artwork: %v", err)
	}

	err := database.BackfillExpandedLibrary(context.Background(), sqlDB)
	if err == nil || !strings.Contains(err.Error(), "decode image metadata") {
		t.Fatalf("backfill error = %v, want invalid Album artwork error", err)
	}

	assertRowCount(t, sqlDB, "track_sources", 0)
	assertIntegerValue(t, sqlDB, `SELECT COUNT(*) FROM artists WHERE name_normalized IS NOT NULL`, 0)
	assertLegacyTrack(t, sqlDB)
}

func TestBackfillExpandedLibraryPreservesExistingExpandedMetadata(t *testing.T) {
	sqlDB := openDatabaseAtVersion(t, STRICT_IDENTITY_VERSION)
	insertLegacyLibrary(t, sqlDB)
	_, err := sqlDB.Exec(`
		UPDATE albums SET identity_key = 'existing-album', release_date = '2020-01-02' WHERE id = 'album-1';
		UPDATE tracks SET identity_key = 'existing-track', codec = 'existing-codec',
			container = 'existing-container', sample_format = 'existing-format', bitrate_bps = 123
		WHERE id = 'track-1';
		INSERT INTO track_sources (
			id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes
		) VALUES (
			'existing-source', 'track-1', 'managed', '/managed/track.flac',
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'flac', 100
		);
	`)
	if err != nil {
		t.Fatalf("store expanded metadata: %v", err)
	}

	if err := database.BackfillExpandedLibrary(context.Background(), sqlDB); err != nil {
		t.Fatalf("backfill expanded library: %v", err)
	}

	assertTextValue(t, sqlDB, `SELECT identity_key || ':' || release_date FROM albums WHERE id = 'album-1'`, "existing-album:2020-01-02")
	assertTextValue(t, sqlDB, `SELECT identity_key || ':' || codec || ':' || container || ':' || sample_format
		FROM tracks WHERE id = 'track-1'`, "existing-track:existing-codec:existing-container:existing-format")
	assertIntegerValue(t, sqlDB, `SELECT bitrate_bps FROM tracks WHERE id = 'track-1'`, 123)
	assertRowCount(t, sqlDB, "track_sources", 1)
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

func createLegacyCover(t *testing.T) []byte {
	t.Helper()
	cover := image.NewRGBA(image.Rect(0, 0, 1, 1))
	cover.Set(0, 0, color.RGBA{R: 20, G: 40, B: 60, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, cover); err != nil {
		t.Fatalf("encode legacy Album artwork: %v", err)
	}
	return encoded.Bytes()
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
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatal("foreign key check reported an integrity violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign key violations: %v", err)
	}
}

func captureLegacyLibraryReads(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	store := library.NewStore(sqlDB)
	artists, err := store.ListArtists(ctx, 10, 0, "")
	if err != nil {
		t.Fatalf("list legacy Artists: %v", err)
	}
	albums, err := store.ListAlbums(ctx, 10, 0, "", "")
	if err != nil {
		t.Fatalf("list legacy Albums: %v", err)
	}
	tracks, err := store.ListTracks(ctx, 10, 0, "")
	if err != nil {
		t.Fatalf("list legacy Tracks: %v", err)
	}
	album, err := store.GetAlbum(ctx, "album-1")
	if err != nil {
		t.Fatalf("get legacy Album: %v", err)
	}
	track, err := store.GetTrack(ctx, "track-1")
	if err != nil {
		t.Fatalf("get legacy Track: %v", err)
	}
	coverMime, coverData, err := store.GetAlbumCover(ctx, "album-1")
	if err != nil {
		t.Fatalf("get legacy Album artwork: %v", err)
	}
	readModel := struct {
		Artists     library.ArtistList  `json:"artists"`
		Albums      library.AlbumList   `json:"albums"`
		Tracks      library.TrackList   `json:"tracks"`
		Album       library.AlbumDetail `json:"album"`
		Track       library.Track       `json:"track"`
		CoverMime   string              `json:"coverMime"`
		CoverSHA256 string              `json:"coverSha256"`
	}{artists, albums, tracks, album, track, coverMime, fmt.Sprintf("%x", sha256.Sum256(coverData))}
	encoded, err := json.Marshal(readModel)
	if err != nil {
		t.Fatalf("encode legacy library reads: %v", err)
	}
	return string(encoded)
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
