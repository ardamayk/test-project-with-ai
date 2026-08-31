package library

import (
	"context"
	"database/sql"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpsertFromScanCreatesExpandedLegacyTrack(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))
	metadata.Format = "FLAC"
	metadata.SizeBytes = 4096
	metadata.Title = "First Track"
	metadata.Artist = "Track Artist"
	metadata.AlbumArtist = "Album Artist"
	metadata.Album = "First Album"
	metadata.Year = 2026
	metadata.Genre = "Rock; Pop"

	added, updated, err := store.UpsertFromScan(context.Background(), metadata)
	if err != nil {
		t.Fatalf("upsert scanned Track: %v", err)
	}
	if !added || updated {
		t.Fatalf("upsert result = (%t, %t), want (true, false)", added, updated)
	}

	var trackID string
	var albumID string
	if err := database.QueryRow(`SELECT id, album_id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&trackID, &albumID); err != nil {
		t.Fatalf("read legacy Track: %v", err)
	}
	assertTextQuery(t, database, `SELECT identity_key FROM legacy_track_identities WHERE track_id = ?`, "first track", trackID)
	assertTextQuery(t, database, `SELECT source_kind || ':' || source_format FROM track_sources WHERE track_id = ?`, "legacy:flac", trackID)
	assertTextQuery(t, database, `SELECT artists.name FROM track_artists INNER JOIN artists ON artists.id = track_artists.artist_id WHERE track_id = ? AND position = 0`, "Track Artist", trackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM track_genres WHERE track_id = ?`, 2, trackID)
	assertTextQuery(t, database, `SELECT identity_key FROM legacy_album_identities WHERE album_id = ?`, "album artist\x1ffirst album\x1f2026", albumID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_genres WHERE album_id = ?`, 2, albumID)
	assertTrackPersistenceAgreement(t, database, trackID)
	assertAlbumPersistenceAgreement(t, database, albumID)
}

func TestUpsertFromScanUpdatesExpandedLegacyTrack(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))
	metadata.SizeBytes = 100
	metadata.Title = "Original Track"
	metadata.Artist = "Original Artist"
	metadata.AlbumArtist = "Original Album Artist"
	metadata.Album = "Original Album"
	metadata.Genre = "Rock"
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("create scanned Track: %v", err)
	}

	var trackID string
	var originalAlbumID string
	if err := database.QueryRow(`SELECT id, album_id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&trackID, &originalAlbumID); err != nil {
		t.Fatalf("read created Track: %v", err)
	}
	metadata.ModTime = time.Unix(101, 0)
	metadata.SizeBytes = 200
	metadata.Title = "Updated Track"
	metadata.Artist = "Updated Artist"
	metadata.AlbumArtist = "Updated Album Artist"
	metadata.Album = "Updated Album"
	metadata.Genre = "Jazz"

	added, updated, err := store.UpsertFromScan(context.Background(), metadata)
	if err != nil {
		t.Fatalf("update scanned Track: %v", err)
	}
	if added || !updated {
		t.Fatalf("upsert result = (%t, %t), want (false, true)", added, updated)
	}

	assertTextQuery(t, database, `SELECT identity_key FROM legacy_track_identities WHERE track_id = ?`, "updated track", trackID)
	assertIntegerQuery(t, database, `SELECT size_bytes FROM track_sources WHERE track_id = ?`, 200, trackID)
	assertTextQuery(t, database, `SELECT artists.name FROM track_artists INNER JOIN artists ON artists.id = track_artists.artist_id WHERE track_id = ?`, "Updated Artist", trackID)
	assertTextQuery(t, database, `SELECT genres.name FROM track_genres INNER JOIN genres ON genres.id = track_genres.genre_id WHERE track_id = ?`, "Jazz", trackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums WHERE id = ?`, 0, originalAlbumID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_identities WHERE album_id = ?`, 0, originalAlbumID)
	assertTrackPersistenceAgreement(t, database, trackID)
}

func TestMissingAndPresentReconciliationUpdatesExpandedLegacyTrack(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	musicRoot := t.TempDir()
	first := legacyFileMetadata(filepath.Join(musicRoot, "first.flac"))
	first.Title = "First"
	first.Genre = "Rock"
	second := first
	second.Path = filepath.Join(musicRoot, "second.flac")
	second.Title = "Second"
	second.Genre = "Jazz"
	for _, metadata := range []FileMetadata{first, second} {
		if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
			t.Fatalf("create scanned Track: %v", err)
		}
	}
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks WHERE identity_key IS NOT NULL`, 0)

	removed, err := store.MarkSeenPaths(context.Background(), map[string]struct{}{first.Path: {}})
	if err != nil {
		t.Fatalf("mark missing Track: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks WHERE identity_key IS NOT NULL`, 2)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_genres`, 1)

	if _, err := store.BeginScan(context.Background()); err != nil {
		t.Fatalf("reset Track presence: %v", err)
	}
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks WHERE identity_key IS NOT NULL`, 0)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_genres`, 2)
}

func TestUpsertFromScanReconcilesExpandedAlbumAndGenreWithoutMtimeChange(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))
	metadata.AlbumArtist = "Original Artist"
	metadata.Album = "Original Album"
	metadata.Genre = "Rock"
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("create scanned Track: %v", err)
	}
	var originalAlbumID string
	if err := database.QueryRow(`SELECT album_id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&originalAlbumID); err != nil {
		t.Fatalf("read original Album: %v", err)
	}

	metadata.AlbumArtist = "Moved Artist"
	metadata.Album = "Moved Album"
	metadata.Genre = "Electronic"
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("reconcile scanned Track: %v", err)
	}

	var movedAlbumID string
	if err := database.QueryRow(`SELECT album_id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&movedAlbumID); err != nil {
		t.Fatalf("read moved Album: %v", err)
	}
	if movedAlbumID == originalAlbumID {
		t.Fatal("moved Album ID did not change")
	}
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_identities WHERE album_id = ?`, 0, originalAlbumID)
	assertTextQuery(t, database, `SELECT identity_key FROM legacy_album_identities WHERE album_id = ?`, "moved artist\x1fmoved album\x1f", movedAlbumID)
	assertTextQuery(t, database, `SELECT genres.name FROM track_genres INNER JOIN genres ON genres.id = track_genres.genre_id`, "Electronic")
	assertTextQuery(t, database, `SELECT genres.name FROM legacy_album_genres INNER JOIN genres ON genres.id = legacy_album_genres.genre_id`, "Electronic")
	var trackID string
	if err := database.QueryRow(`SELECT id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&trackID); err != nil {
		t.Fatalf("read reconciled Track: %v", err)
	}
	assertTrackPersistenceAgreement(t, database, trackID)
	assertAlbumPersistenceAgreement(t, database, movedAlbumID)
	assertSingleGenrePersistenceAgreement(t, database, trackID)
}

func TestUpsertFromScanUpdatesExpandedAlbumArtwork(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("create scanned Track: %v", err)
	}
	coverData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode test artwork: %v", err)
	}
	metadata.CoverMime = "image/png"
	metadata.CoverData = coverData
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("update scanned Album Artwork: %v", err)
	}

	assertTextQuery(t, database, `SELECT content_sha256 FROM legacy_album_artwork_metadata`, "431ced6916a2a21a156e38701afe55bbd7f88969fbbfc56d7fe099d47f265460")
	assertTextQuery(t, database, `SELECT media_type FROM legacy_album_artwork_metadata`, "image/png")
	assertIntegerQuery(t, database, `SELECT width * height FROM legacy_album_artwork_metadata`, 1)
	assertIntegerQuery(t, database, `SELECT encoded_size_bytes FROM legacy_album_artwork_metadata`, 68)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums
		INNER JOIN legacy_album_artwork_metadata ON legacy_album_artwork_metadata.album_id = albums.id
		WHERE length(albums.cover_data) = legacy_album_artwork_metadata.encoded_size_bytes
		AND albums.cover_mime = legacy_album_artwork_metadata.media_type`, 1)
}

func TestDeleteTrackUpdatesExpandedLegacyAlbum(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	musicRoot := t.TempDir()
	first := legacyFileMetadata(filepath.Join(musicRoot, "first.flac"))
	first.Title = "First"
	first.Genre = "Rock"
	second := first
	second.Path = filepath.Join(musicRoot, "second.flac")
	second.Title = "Second"
	second.Artist = "Guest Artist"
	second.TrackNo = 2
	second.Genre = "Jazz"
	for _, metadata := range []FileMetadata{first, second} {
		if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
			t.Fatalf("create scanned Track: %v", err)
		}
	}
	var deletedTrackID string
	if err := database.QueryRow(`SELECT id FROM tracks WHERE file_path = ?`, second.Path).Scan(&deletedTrackID); err != nil {
		t.Fatalf("read Track to delete: %v", err)
	}

	if _, err := store.DeleteTrack(context.Background(), deletedTrackID, nil); err != nil {
		t.Fatalf("delete Track: %v", err)
	}

	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_track_identities WHERE track_id = ?`, 0, deletedTrackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM track_sources WHERE track_id = ?`, 0, deletedTrackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM track_artists WHERE track_id = ?`, 0, deletedTrackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM track_genres WHERE track_id = ?`, 0, deletedTrackID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_genres`, 1)
	assertTextQuery(t, database, `SELECT genres.name FROM legacy_album_genres INNER JOIN genres ON genres.id = legacy_album_genres.genre_id`, "Rock")
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM artists WHERE name = 'Guest Artist'`, 0)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM genres WHERE name = 'Jazz'`, 0)
}

func TestUpsertFromScanRollsBackWhenExpandedWriteFails(t *testing.T) {
	database := openMemoryDB(t)
	if _, err := database.Exec(`DROP TABLE track_sources`); err != nil {
		t.Fatalf("remove expanded Track source table: %v", err)
	}
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))

	_, _, err := store.UpsertFromScan(context.Background(), metadata)
	if err == nil {
		t.Fatal("upsert scanned Track succeeded without expanded Track source storage")
	}
	if !strings.Contains(err.Error(), "synchronize expanded Track") || !strings.Contains(err.Error(), "write Track source") {
		t.Fatalf("upsert error = %q, want expanded Track source context", err)
	}
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks`, 0)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums`, 0)
}

func TestDeleteAlbumPromotesSurvivingExpandedLegacyIdentities(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	if _, err := database.Exec(`
		INSERT INTO artists (id, name, name_sort) VALUES
			('artist-1', 'Same Artist', 'same-artist-1'),
			('artist-2', 'Same Artist', 'same-artist-2');
		INSERT INTO legacy_artist_identities (artist_id, normalized_name) VALUES
			('artist-1', 'same artist'),
			('artist-2', 'same artist');
		INSERT INTO albums (id, artist_id, title, title_sort, genres) VALUES
			('album-1', 'artist-1', 'Same Album', 'same-album-1', '[]'),
			('album-2', 'artist-2', 'Same Album', 'same-album-2', '[]');
		INSERT INTO legacy_album_identities (album_id, identity_key) VALUES
			('album-1', 'same artist' || char(31) || 'same album' || char(31)),
			('album-2', 'same artist' || char(31) || 'same album' || char(31));
		INSERT INTO album_artists (album_id, artist_id, position) VALUES
			('album-1', 'artist-1', 0),
			('album-2', 'artist-2', 0);
	`); err != nil {
		t.Fatalf("seed ambiguous legacy identities: %v", err)
	}

	if _, err := store.DeleteAlbum(context.Background(), "album-2", nil); err != nil {
		t.Fatalf("delete ambiguous Album: %v", err)
	}

	assertTextQuery(t, database, `SELECT name_normalized FROM artists WHERE id = 'artist-1'`, "same artist")
	assertTextQuery(t, database, `SELECT identity_key FROM albums WHERE id = 'album-1'`, "same artist\x1fsame album\x1f")
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_artist_identities WHERE artist_id = 'artist-2'`, 0)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM legacy_album_identities WHERE album_id = 'album-2'`, 0)
}

func TestMoveLikeReconciliationPromotesSurvivingExpandedAlbumIdentity(t *testing.T) {
	database := openMemoryDB(t)
	store := NewStore(database)
	metadata := legacyFileMetadata(filepath.Join(t.TempDir(), "track.flac"))
	metadata.AlbumArtist = "Original Artist"
	metadata.Album = "Original Album"
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("create scanned Track: %v", err)
	}
	var originalAlbumID string
	if err := database.QueryRow(`SELECT album_id FROM tracks WHERE file_path = ?`, metadata.Path).Scan(&originalAlbumID); err != nil {
		t.Fatalf("read original Album: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO albums (id, artist_id, title, title_sort, genres)
		SELECT 'surviving-album', artist_id, title, 'surviving-album', genres FROM albums WHERE id = ?;
		INSERT INTO legacy_album_identities (album_id, identity_key)
		SELECT 'surviving-album', identity_key FROM legacy_album_identities WHERE album_id = ?;
		INSERT INTO album_artists (album_id, artist_id, position)
		SELECT 'surviving-album', artist_id, 0 FROM albums WHERE id = 'surviving-album';
		UPDATE albums SET identity_key = NULL WHERE id IN (?, 'surviving-album');
	`, originalAlbumID, originalAlbumID, originalAlbumID); err != nil {
		t.Fatalf("seed ambiguous source Album: %v", err)
	}

	metadata.Album = "Moved Album"
	if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
		t.Fatalf("move scanned Track: %v", err)
	}

	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums WHERE id = ?`, 0, originalAlbumID)
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums
		INNER JOIN legacy_album_identities ON legacy_album_identities.album_id = albums.id
		WHERE albums.id = 'surviving-album'
		AND albums.identity_key = legacy_album_identities.identity_key`, 1)
}

func assertTextQuery(t *testing.T, database queryRower, query string, expected string, arguments ...any) {
	t.Helper()
	var actual string
	if err := database.QueryRow(query, arguments...).Scan(&actual); err != nil {
		t.Fatalf("query text value: %v", err)
	}
	if actual != expected {
		t.Fatalf("query text value = %q, want %q", actual, expected)
	}
}

func legacyFileMetadata(path string) FileMetadata {
	return FileMetadata{
		Path:        path,
		Format:      "flac",
		ModTime:     time.Unix(100, 0),
		Title:       "Track",
		Artist:      "Artist",
		AlbumArtist: "Artist",
		Album:       "Album",
		TrackNo:     1,
	}
}

func assertIntegerQuery(t *testing.T, database queryRower, query string, expected int, arguments ...any) {
	t.Helper()
	var actual int
	if err := database.QueryRow(query, arguments...).Scan(&actual); err != nil {
		t.Fatalf("query integer value: %v", err)
	}
	if actual != expected {
		t.Fatalf("query integer value = %d, want %d", actual, expected)
	}
}

type queryRower interface {
	QueryRow(query string, arguments ...any) *sql.Row
}

func assertTrackPersistenceAgreement(t *testing.T, database queryRower, trackID string) {
	t.Helper()
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks
		INNER JOIN legacy_track_identities ON legacy_track_identities.track_id = tracks.id
		INNER JOIN track_sources ON track_sources.track_id = tracks.id
		INNER JOIN track_artists ON track_artists.track_id = tracks.id AND track_artists.position = 0
		INNER JOIN artists ON artists.id = track_artists.artist_id
		WHERE tracks.id = ?
		AND legacy_track_identities.identity_key = lower(tracks.title)
		AND track_sources.file_path = tracks.file_path
		AND track_sources.source_format = lower(tracks.format)
		AND track_sources.size_bytes = tracks.size_bytes
		AND artists.name = tracks.artist_name`, 1, trackID)
}

func assertAlbumPersistenceAgreement(t *testing.T, database queryRower, albumID string) {
	t.Helper()
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM albums
		INNER JOIN legacy_album_identities ON legacy_album_identities.album_id = albums.id
		INNER JOIN album_artists ON album_artists.album_id = albums.id AND album_artists.position = 0
		WHERE albums.id = ? AND album_artists.artist_id = albums.artist_id`, 1, albumID)
}

func assertSingleGenrePersistenceAgreement(t *testing.T, database queryRower, trackID string) {
	t.Helper()
	assertIntegerQuery(t, database, `SELECT COUNT(*) FROM tracks
		INNER JOIN track_genres ON track_genres.track_id = tracks.id
		INNER JOIN genres AS track_genre ON track_genre.id = track_genres.genre_id
		INNER JOIN albums ON albums.id = tracks.album_id
		INNER JOIN json_each(albums.genres) AS legacy_album_genre
		INNER JOIN legacy_album_genres ON legacy_album_genres.album_id = albums.id
		INNER JOIN genres AS album_genre ON album_genre.id = legacy_album_genres.genre_id
		WHERE tracks.id = ?
		AND tracks.genre = track_genre.name
		AND legacy_album_genre.value = album_genre.name`, 1, trackID)
}
