package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	LEGACY_ARTIST_ID_PREFIX   = "legacy-artist:"
	LEGACY_GENRE_ID_PREFIX    = "legacy-genre:"
	LEGACY_IDENTITY_SEPARATOR = "\x1f"
	LEGACY_SOURCE_ID_PREFIX   = "legacy-source:"
	LEGACY_SOURCE_KIND        = "legacy"
)

// SynchronizeLegacyTrack mirrors one scanner-owned Track into the expanded model.
func SynchronizeLegacyTrack(ctx context.Context, tx *sql.Tx, trackID string) error {
	var albumID string
	if err := tx.QueryRowContext(ctx, `SELECT album_id FROM tracks WHERE id = ?`, trackID).Scan(&albumID); err != nil {
		return fmt.Errorf("synchronize expanded Track %q: read Album: %w", trackID, err)
	}
	album, err := readLegacyAlbumForSync(ctx, tx, albumID)
	if err != nil {
		return fmt.Errorf("synchronize expanded Track %q: read Album identity: %w", trackID, err)
	}
	if identityErr := synchronizeLegacyAlbumIdentity(ctx, tx, album); identityErr != nil {
		return fmt.Errorf("synchronize expanded Track %q: prepare Album Artist: %w", trackID, identityErr)
	}
	albumID, err = synchronizeLegacyTrackRow(ctx, tx, trackID)
	if err != nil {
		return fmt.Errorf("synchronize expanded Track %q: %w", trackID, err)
	}
	if err := SynchronizeLegacyAlbum(ctx, tx, albumID); err != nil {
		return err
	}
	if err := populateUnambiguousArtistNames(ctx, tx); err != nil {
		return err
	}
	return CleanupLegacyTransitionEntities(ctx, tx)
}

// CleanupLegacyTransitionEntities removes expanded entities created only for legacy relationships.
func CleanupLegacyTransitionEntities(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM artists
		WHERE id LIKE ?
		AND NOT EXISTS (SELECT 1 FROM albums WHERE albums.artist_id = artists.id)
		AND NOT EXISTS (SELECT 1 FROM track_artists WHERE track_artists.artist_id = artists.id)
		AND NOT EXISTS (SELECT 1 FROM album_artists WHERE album_artists.artist_id = artists.id)`, LEGACY_ARTIST_ID_PREFIX+"%"); err != nil {
		return fmt.Errorf("clean orphaned legacy Artists: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM genres
		WHERE id LIKE ?
		AND NOT EXISTS (SELECT 1 FROM track_genres WHERE track_genres.genre_id = genres.id)
		AND NOT EXISTS (SELECT 1 FROM legacy_album_genres WHERE legacy_album_genres.genre_id = genres.id)`, LEGACY_GENRE_ID_PREFIX+"%"); err != nil {
		return fmt.Errorf("clean orphaned legacy Genres: %w", err)
	}
	return nil
}

// RecomputeLegacyIdentityColumns promotes legacy identities that became unambiguous after deletion.
func RecomputeLegacyIdentityColumns(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE artists SET name_normalized = NULL
		WHERE id IN (SELECT artist_id FROM legacy_artist_identities)`); err != nil {
		return fmt.Errorf("clear legacy Artist identities: %w", err)
	}
	if err := populateUnambiguousArtistNames(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE albums SET identity_key = NULL
		WHERE id IN (SELECT album_id FROM legacy_album_identities)`); err != nil {
		return fmt.Errorf("clear legacy Album identities: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE albums SET identity_key = (
		SELECT identity_key FROM legacy_album_identities WHERE album_id = albums.id
	) WHERE id IN (SELECT album_id FROM legacy_album_identities) AND (
		SELECT COUNT(*) FROM legacy_album_identities AS matching
		WHERE matching.identity_key = (
			SELECT identity_key FROM legacy_album_identities WHERE album_id = albums.id
		)
	) = 1`); err != nil {
		return fmt.Errorf("promote unambiguous legacy Album identities: %w", err)
	}
	return nil
}

// FinalizeLegacyRemoval cleans transition-only rows and promotes identities after an entity disappears.
func FinalizeLegacyRemoval(ctx context.Context, tx *sql.Tx) error {
	if err := CleanupLegacyTransitionEntities(ctx, tx); err != nil {
		return err
	}
	return RecomputeLegacyIdentityColumns(ctx, tx)
}

func synchronizeLegacyTrackRow(ctx context.Context, tx *sql.Tx, trackID string) (string, error) {
	track, err := readLegacyTrackForSync(ctx, tx, trackID)
	if err != nil {
		return "", err
	}
	if err := storeLegacyIdentity(ctx, tx,
		`INSERT INTO legacy_track_identities (track_id, identity_key) VALUES (?, ?)
		ON CONFLICT(track_id) DO UPDATE SET identity_key = excluded.identity_key`,
		`SELECT identity_key FROM legacy_track_identities WHERE track_id = ?`,
		track.id, normalizeLegacyName(track.title)); err != nil {
		return "", fmt.Errorf("write Track identity: %w", err)
	}
	if err := synchronizeLegacyTrackSource(ctx, tx, track); err != nil {
		return "", err
	}
	if err := synchronizeLegacyTrackArtist(ctx, tx, track.id, track.artistName); err != nil {
		return "", err
	}
	if err := synchronizeLegacyTrackGenres(ctx, tx, track.id, track.genre); err != nil {
		return "", err
	}
	return track.albumID, nil
}

type legacyTrackSync struct {
	id         string
	albumID    string
	title      string
	artistName string
	filePath   string
	format     string
	sizeBytes  int64
	genre      sql.NullString
}

func readLegacyTrackForSync(ctx context.Context, tx *sql.Tx, trackID string) (legacyTrackSync, error) {
	var track legacyTrackSync
	err := tx.QueryRowContext(ctx, `SELECT id, album_id, title, artist_name, file_path, format, size_bytes, genre
		FROM tracks WHERE id = ?`, trackID).Scan(
		&track.id, &track.albumID, &track.title, &track.artistName,
		&track.filePath, &track.format, &track.sizeBytes, &track.genre,
	)
	if err != nil {
		return legacyTrackSync{}, fmt.Errorf("read legacy Track: %w", err)
	}
	return track, nil
}

func synchronizeLegacyTrackSource(ctx context.Context, tx *sql.Tx, track legacyTrackSync) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO track_sources (
		id, track_id, source_kind, file_path, source_format, size_bytes
	) VALUES (? || ?, ?, ?, ?, lower(?), ?)
	ON CONFLICT(track_id) DO UPDATE SET
		file_path = excluded.file_path,
		source_format = excluded.source_format,
		size_bytes = excluded.size_bytes,
		revision = track_sources.revision + 1,
		updated_at = CURRENT_TIMESTAMP
	WHERE track_sources.source_kind = ? AND (
		track_sources.file_path != excluded.file_path OR
		track_sources.source_format != excluded.source_format OR
		track_sources.size_bytes != excluded.size_bytes
	)`,
		LEGACY_SOURCE_ID_PREFIX, track.id, track.id, LEGACY_SOURCE_KIND, track.filePath, track.format, track.sizeBytes, LEGACY_SOURCE_KIND,
	)
	if err != nil {
		return fmt.Errorf("write Track source: %w", err)
	}
	return verifyLegacyTrackSource(ctx, tx, track)
}

func verifyLegacyTrackSource(ctx context.Context, tx *sql.Tx, expected legacyTrackSync) error {
	var sourceKind string
	var filePath string
	var sourceFormat string
	var sizeBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT source_kind, file_path, source_format, size_bytes
		FROM track_sources WHERE track_id = ?`, expected.id).Scan(
		&sourceKind, &filePath, &sourceFormat, &sizeBytes,
	); err != nil {
		return fmt.Errorf("verify Track source: %w", err)
	}
	if sourceKind != LEGACY_SOURCE_KIND || filePath != expected.filePath || sourceFormat != strings.ToLower(expected.format) || sizeBytes != expected.sizeBytes {
		return fmt.Errorf("verify Track source: stored values do not match legacy Track %q", expected.id)
	}
	return nil
}

func synchronizeLegacyTrackArtist(ctx context.Context, tx *sql.Tx, trackID string, artistName string) error {
	artistID, err := findOrCreateLegacyArtist(ctx, tx, artistName)
	if err != nil {
		return fmt.Errorf("write Track Artist: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM track_artists WHERE track_id = ?`, trackID); err != nil {
		return fmt.Errorf("clear Track Artists: %w", err)
	}
	if err := relateLegacyTrackArtist(ctx, tx, trackID, artistID); err != nil {
		return fmt.Errorf("relate Track Artist: %w", err)
	}
	return nil
}

func synchronizeLegacyTrackGenres(ctx context.Context, tx *sql.Tx, trackID string, rawGenre sql.NullString) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM track_genres WHERE track_id = ?`, trackID); err != nil {
		return fmt.Errorf("clear Track Genres: %w", err)
	}
	for position, genreName := range splitLegacyGenres(rawGenre.String) {
		genreID, err := findOrCreateLegacyGenre(ctx, tx, genreName)
		if err != nil {
			return fmt.Errorf("write Track Genre %q: %w", genreName, err)
		}
		if err := relateLegacyTrackGenre(ctx, tx, trackID, genreID, position); err != nil {
			return fmt.Errorf("relate Track Genre %q: %w", genreName, err)
		}
	}
	return nil
}

// SynchronizeLegacyAlbum mirrors one scanner-owned Album into the expanded model.
func SynchronizeLegacyAlbum(ctx context.Context, tx *sql.Tx, albumID string) error {
	album, err := readLegacyAlbumForSync(ctx, tx, albumID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("synchronize expanded Album %q: %w", albumID, err)
	}
	if err := synchronizeLegacyAlbumIdentity(ctx, tx, album); err != nil {
		return fmt.Errorf("synchronize expanded Album %q: %w", albumID, err)
	}
	if err := synchronizeLegacyAlbumRelationships(ctx, tx, album); err != nil {
		return fmt.Errorf("synchronize expanded Album %q: %w", albumID, err)
	}
	if err := synchronizeLegacyAlbumArtwork(ctx, tx, album); err != nil {
		return fmt.Errorf("synchronize expanded Album %q: %w", albumID, err)
	}
	return recomputeLegacyTrackIdentities(ctx, tx, album.id)
}

type legacyAlbumSync struct {
	id         string
	artistID   string
	artistName string
	title      string
	year       sql.NullInt64
	genres     sql.NullString
	coverData  []byte
}

func readLegacyAlbumForSync(ctx context.Context, tx *sql.Tx, albumID string) (legacyAlbumSync, error) {
	var album legacyAlbumSync
	err := tx.QueryRowContext(ctx, `SELECT albums.id, artists.id, artists.name, albums.title,
		albums.year, albums.genres, albums.cover_data
		FROM albums INNER JOIN artists ON artists.id = albums.artist_id
		WHERE albums.id = ?`, albumID).Scan(
		&album.id, &album.artistID, &album.artistName, &album.title,
		&album.year, &album.genres, &album.coverData,
	)
	return album, err
}

func synchronizeLegacyAlbumIdentity(ctx context.Context, tx *sql.Tx, album legacyAlbumSync) error {
	normalizedArtist := normalizeLegacyName(album.artistName)
	if err := storeLegacyArtistIdentity(ctx, tx, album.artistID, normalizedArtist); err != nil {
		return fmt.Errorf("write Album Artist identity: %w", err)
	}
	releaseDate := ""
	if album.year.Valid && album.year.Int64 > 0 {
		releaseDate = fmt.Sprintf("%04d", album.year.Int64)
	}
	identityKey := strings.Join([]string{normalizedArtist, normalizeLegacyName(album.title), releaseDate}, LEGACY_IDENTITY_SEPARATOR)
	if err := storeLegacyIdentity(ctx, tx,
		`INSERT INTO legacy_album_identities (album_id, identity_key) VALUES (?, ?)
		ON CONFLICT(album_id) DO UPDATE SET identity_key = excluded.identity_key`,
		`SELECT identity_key FROM legacy_album_identities WHERE album_id = ?`,
		album.id, identityKey); err != nil {
		return fmt.Errorf("write Album identity: %w", err)
	}
	return recomputeLegacyAlbumIdentity(ctx, tx, identityKey, releaseDate, album.id)
}

func recomputeLegacyAlbumIdentity(ctx context.Context, tx *sql.Tx, identityKey string, releaseDate string, albumID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE albums SET identity_key = NULL
		WHERE id IN (SELECT album_id FROM legacy_album_identities WHERE identity_key = ?)`, identityKey); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE albums SET
		identity_key = CASE WHEN (SELECT COUNT(*) FROM legacy_album_identities WHERE identity_key = ?) = 1 THEN ? END,
		release_date = NULLIF(?, '')
		WHERE id = ?`, identityKey, identityKey, releaseDate, albumID)
	return err
}

func synchronizeLegacyAlbumRelationships(ctx context.Context, tx *sql.Tx, album legacyAlbumSync) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM album_artists WHERE album_id = ?`, album.id); err != nil {
		return fmt.Errorf("clear Album Artists: %w", err)
	}
	if err := relateLegacyAlbumArtist(ctx, tx, album.id, album.artistID); err != nil {
		return fmt.Errorf("relate Album Artist: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM legacy_album_genres WHERE album_id = ?`, album.id); err != nil {
		return fmt.Errorf("clear Album Genres: %w", err)
	}
	for position, genreName := range decodeLegacyAlbumGenres(album.genres.String) {
		genreID, err := findOrCreateLegacyGenre(ctx, tx, genreName)
		if err != nil {
			return fmt.Errorf("write Album Genre %q: %w", genreName, err)
		}
		if err := relateLegacyAlbumGenre(ctx, tx, album.id, genreID, position); err != nil {
			return fmt.Errorf("relate Album Genre %q: %w", genreName, err)
		}
	}
	return nil
}

func synchronizeLegacyAlbumArtwork(ctx context.Context, tx *sql.Tx, album legacyAlbumSync) error {
	if len(album.coverData) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM legacy_album_artwork_metadata WHERE album_id = ?`, album.id)
		return err
	}
	if _, err := storeLegacyArtwork(ctx, tx, legacyArtwork{albumID: album.id, data: album.coverData}); err != nil {
		return fmt.Errorf("write Album Artwork metadata: %w", err)
	}
	return nil
}

func recomputeLegacyTrackIdentities(ctx context.Context, tx *sql.Tx, albumID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE tracks SET identity_key = NULL
		WHERE album_id = ? AND id IN (SELECT track_id FROM legacy_track_identities)`, albumID); err != nil {
		return fmt.Errorf("clear Track identities: %w", err)
	}
	_, err := tx.ExecContext(ctx, `UPDATE tracks SET identity_key = (
		SELECT identity_key FROM legacy_track_identities WHERE track_id = tracks.id
	) WHERE album_id = ? AND id IN (SELECT track_id FROM legacy_track_identities) AND (
		missing_at IS NOT NULL OR track_no IS NULL OR NOT EXISTS (
			SELECT 1 FROM tracks AS conflicting
			WHERE conflicting.id != tracks.id
			AND conflicting.album_id = tracks.album_id
			AND conflicting.missing_at IS NULL
			AND COALESCE(conflicting.disc_no, 1) = COALESCE(tracks.disc_no, 1)
			AND conflicting.track_no = tracks.track_no
		)
	)`, albumID)
	if err != nil {
		return fmt.Errorf("write Track identities: %w", err)
	}
	return nil
}
