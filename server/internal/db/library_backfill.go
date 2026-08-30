package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	_ "golang.org/x/image/webp"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MAX_LEGACY_ARTWORK_SIZE_BYTES = 20 * 1024 * 1024
	MAX_LEGACY_ARTWORK_PIXELS     = 50_000_000
)

// BackfillExpandedLibrary mirrors legacy library rows into the additive strict identity model.
func BackfillExpandedLibrary(ctx context.Context, sqlDB *sql.DB) (err error) {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin expanded library backfill: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back expanded library backfill: %w", rollbackErr))
		}
	}()

	if err := backfillLegacySourcesAndArtists(ctx, tx); err != nil {
		return err
	}
	if err := backfillLegacyArtwork(ctx, tx); err != nil {
		return err
	}
	if err := verifyExpandedLibraryBackfill(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit expanded library backfill: %w", err)
	}
	return nil
}

func backfillLegacySourcesAndArtists(ctx context.Context, tx *sql.Tx) error {
	if err := normalizeLegacyArtists(ctx, tx); err != nil {
		return err
	}
	if err := createMissingLegacyArtists(ctx, tx); err != nil {
		return err
	}
	if err := backfillLegacyIdentityKeys(ctx, tx); err != nil {
		return err
	}
	statements := []string{
		`INSERT OR IGNORE INTO track_sources (
			id, track_id, source_kind, file_path, source_format, size_bytes
		) SELECT 'legacy-source:' || id, id, 'legacy', file_path, lower(format), size_bytes
		FROM tracks
		WHERE NOT EXISTS (SELECT 1 FROM track_sources WHERE track_sources.track_id = tracks.id)`,
		`INSERT OR IGNORE INTO album_artists (album_id, artist_id, position)
		SELECT id, artist_id, 0 FROM albums`,
		`UPDATE tracks SET
			codec = COALESCE(codec, lower(format)),
			container = COALESCE(container, lower(format)),
			sample_format = COALESCE(sample_format,
				CASE WHEN bit_depth IS NOT NULL AND bit_depth > 0 THEN printf('s%d', bit_depth) END),
			bitrate_bps = COALESCE(bitrate_bps,
				CASE WHEN size_bytes > 0 AND duration_ms > 0
					THEN (size_bytes * 8000 + duration_ms / 2) / duration_ms END)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("backfill legacy source and Artist data: %w", err)
		}
	}
	return backfillLegacyGenres(ctx, tx)
}

func normalizeLegacyArtists(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM artists WHERE name_normalized IS NULL`)
	if err != nil {
		return fmt.Errorf("list legacy Artists: %w", err)
	}
	values, err := scanLegacyNames(rows)
	if err != nil {
		return fmt.Errorf("read legacy Artists: %w", err)
	}
	for _, value := range values {
		if _, err := tx.ExecContext(ctx, `UPDATE artists SET name_normalized = ? WHERE id = ?`, normalizeLegacyName(value.name), value.id); err != nil {
			return fmt.Errorf("normalize legacy Artist %q: %w", value.id, err)
		}
	}
	return nil
}

func createMissingLegacyArtists(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, artist_name FROM tracks`)
	if err != nil {
		return fmt.Errorf("list legacy Track Artists: %w", err)
	}
	artists, err := scanLegacyNames(rows)
	if err != nil {
		return fmt.Errorf("read legacy Track Artists: %w", err)
	}
	for _, artist := range artists {
		artistID, err := findOrCreateNormalizedEntity(ctx, tx, "artists", "legacy-artist", artist.name)
		if err != nil {
			return fmt.Errorf("backfill legacy Track Artist %q: %w", artist.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_artists (track_id, artist_id, position) VALUES (?, ?, 0)`, artist.id, artistID); err != nil {
			return fmt.Errorf("relate legacy Track %q to Artist: %w", artist.id, err)
		}
	}
	return nil
}

func backfillLegacyGenres(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, genre FROM tracks WHERE trim(COALESCE(genre, '')) != ''`)
	if err != nil {
		return fmt.Errorf("list legacy Genres: %w", err)
	}
	genres, err := scanLegacyNames(rows)
	if err != nil {
		return fmt.Errorf("read legacy Genres: %w", err)
	}
	for _, genre := range genres {
		genreID, err := findOrCreateNormalizedEntity(ctx, tx, "genres", "legacy-genre", genre.name)
		if err != nil {
			return fmt.Errorf("backfill legacy Genre %q: %w", genre.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_genres (track_id, genre_id, position) VALUES (?, ?, 0)`, genre.id, genreID); err != nil {
			return fmt.Errorf("relate legacy Track %q to Genre: %w", genre.id, err)
		}
	}
	return nil
}

func findOrCreateNormalizedEntity(ctx context.Context, tx *sql.Tx, tableName string, idPrefix string, name string) (string, error) {
	normalizedName := normalizeLegacyName(name)
	var existingID string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE name_normalized = ?`, tableName), normalizedName).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	entityID := deterministicLegacyID(idPrefix, normalizedName)
	if tableName == "artists" {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO artists (id, name, name_sort, name_normalized) VALUES (?, ?, ?, ?)`,
			entityID, strings.TrimSpace(name), normalizedName, normalizedName,
		)
		return entityID, err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, name, name_normalized) VALUES (?, ?, ?)`, tableName,
	), entityID, strings.TrimSpace(name), normalizedName)
	return entityID, err
}

type legacyName struct {
	id   string
	name string
}

func scanLegacyNames(rows *sql.Rows) (values []legacyName, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var value legacyName
		if err := rows.Scan(&value.id, &value.name); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func normalizeLegacyName(value string) string {
	value = strings.Join(strings.Fields(norm.NFC.String(value)), " ")
	return cases.Fold().String(value)
}

func deterministicLegacyID(prefix string, value string) string {
	hash := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s:%x", prefix, hash)
}

type legacyAlbumIdentity struct {
	id                   string
	artistNormalizedName string
	title                string
	year                 sql.NullInt64
}

func backfillLegacyIdentityKeys(ctx context.Context, tx *sql.Tx) error {
	albums, err := listLegacyAlbumIdentities(ctx, tx)
	if err != nil {
		return err
	}
	for _, album := range albums {
		releaseDate := ""
		if album.year.Valid && album.year.Int64 > 0 {
			releaseDate = fmt.Sprintf("%04d", album.year.Int64)
		}
		identityKey := strings.Join([]string{album.artistNormalizedName, normalizeLegacyName(album.title), releaseDate}, "\x1f")
		if _, err := tx.ExecContext(ctx, `UPDATE albums SET identity_key = ?, release_date = COALESCE(release_date, NULLIF(?, '')) WHERE id = ?`, identityKey, releaseDate, album.id); err != nil {
			return fmt.Errorf("backfill legacy Album identity %q: %w", album.id, err)
		}
	}
	return backfillLegacyTrackIdentityKeys(ctx, tx)
}

func listLegacyAlbumIdentities(ctx context.Context, tx *sql.Tx) (albums []legacyAlbumIdentity, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT albums.id, artists.name_normalized, albums.title, albums.year
		FROM albums INNER JOIN artists ON artists.id = albums.artist_id
		WHERE albums.identity_key IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("list legacy Album identities: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var album legacyAlbumIdentity
		if err := rows.Scan(&album.id, &album.artistNormalizedName, &album.title, &album.year); err != nil {
			return nil, fmt.Errorf("read legacy Album identity: %w", err)
		}
		albums = append(albums, album)
	}
	return albums, rows.Err()
}

func backfillLegacyTrackIdentityKeys(ctx context.Context, tx *sql.Tx) (err error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, title FROM tracks WHERE identity_key IS NULL`)
	if err != nil {
		return fmt.Errorf("list legacy Track identities: %w", err)
	}
	tracks, err := scanLegacyNames(rows)
	if err != nil {
		return fmt.Errorf("read legacy Track identities: %w", err)
	}
	for _, track := range tracks {
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET identity_key = ? WHERE id = ?`, normalizeLegacyName(track.name), track.id); err != nil {
			return fmt.Errorf("backfill legacy Track identity %q: %w", track.id, err)
		}
	}
	return nil
}

func verifyExpandedLibraryBackfill(ctx context.Context, tx *sql.Tx) error {
	checks := []struct {
		name  string
		query string
	}{
		{"Track source", `SELECT COUNT(*) FROM tracks LEFT JOIN track_sources ON track_sources.track_id = tracks.id WHERE track_sources.track_id IS NULL`},
		{"Track Artist relationship", `SELECT COUNT(*) FROM tracks LEFT JOIN track_artists ON track_artists.track_id = tracks.id WHERE track_artists.track_id IS NULL`},
		{"Album Artist relationship", `SELECT COUNT(*) FROM albums LEFT JOIN album_artists ON album_artists.album_id = albums.id WHERE album_artists.album_id IS NULL`},
		{"Track Genre relationship", `SELECT COUNT(*) FROM tracks LEFT JOIN track_genres ON track_genres.track_id = tracks.id WHERE trim(COALESCE(tracks.genre, '')) != '' AND track_genres.track_id IS NULL`},
		{"Album Artwork metadata", `SELECT COUNT(*) FROM albums LEFT JOIN album_artwork ON album_artwork.album_id = albums.id WHERE length(COALESCE(albums.cover_data, x'')) > 0 AND EXISTS (SELECT 1 FROM tracks WHERE tracks.album_id = albums.id) AND album_artwork.album_id IS NULL`},
	}
	for _, check := range checks {
		var missingCount int
		if err := tx.QueryRowContext(ctx, check.query).Scan(&missingCount); err != nil {
			return fmt.Errorf("verify %s: %w", check.name, err)
		}
		if missingCount != 0 {
			return fmt.Errorf("verify %s: %d legacy rows are incomplete", check.name, missingCount)
		}
	}
	return verifyForeignKeys(ctx, tx)
}

func verifyForeignKeys(ctx context.Context, tx *sql.Tx) (err error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("verify foreign keys: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if rows.Next() {
		return errors.New("verify foreign keys: integrity violation found")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("verify foreign keys: %w", err)
	}
	return nil
}

type legacyArtwork struct {
	albumID       string
	sourceTrackID string
	data          []byte
}

func backfillLegacyArtwork(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT albums.id,
		(SELECT tracks.id FROM tracks WHERE tracks.album_id = albums.id ORDER BY tracks.id LIMIT 1),
		albums.cover_data
		FROM albums
		WHERE length(COALESCE(albums.cover_data, x'')) > 0
		AND EXISTS (SELECT 1 FROM tracks WHERE tracks.album_id = albums.id)
		AND NOT EXISTS (SELECT 1 FROM album_artwork WHERE album_artwork.album_id = albums.id)`)
	if err != nil {
		return fmt.Errorf("list legacy Album artwork: %w", err)
	}
	artwork, err := scanLegacyArtwork(rows)
	if err != nil {
		return fmt.Errorf("read legacy Album artwork: %w", err)
	}
	for _, item := range artwork {
		if err := storeLegacyArtwork(ctx, tx, item); err != nil {
			return fmt.Errorf("backfill legacy Album artwork %q: %w", item.albumID, err)
		}
	}
	return nil
}

func scanLegacyArtwork(rows *sql.Rows) (artwork []legacyArtwork, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var item legacyArtwork
		if err := rows.Scan(&item.albumID, &item.sourceTrackID, &item.data); err != nil {
			return nil, err
		}
		artwork = append(artwork, item)
	}
	return artwork, rows.Err()
}

func storeLegacyArtwork(ctx context.Context, tx *sql.Tx, artwork legacyArtwork) error {
	if len(artwork.data) > MAX_LEGACY_ARTWORK_SIZE_BYTES {
		return fmt.Errorf("encoded data exceeds %d bytes", MAX_LEGACY_ARTWORK_SIZE_BYTES)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(artwork.data))
	if err != nil {
		return fmt.Errorf("decode image metadata: %w", err)
	}
	mediaType, isSupported := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format]
	if !isSupported || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MAX_LEGACY_ARTWORK_PIXELS {
		return fmt.Errorf("unsupported image format or dimensions: %s %dx%d", format, config.Width, config.Height)
	}
	contentHash := fmt.Sprintf("%x", sha256.Sum256(artwork.data))
	_, err = tx.ExecContext(ctx, `INSERT INTO album_artwork (
		id, album_id, source_track_id, content_sha256, media_type,
		width, height, encoded_size_bytes, file_path
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(album_id) DO NOTHING`,
		deterministicLegacyID("legacy-artwork", artwork.albumID), artwork.albumID, artwork.sourceTrackID,
		contentHash, mediaType, config.Width, config.Height, len(artwork.data),
		"legacy-db://albums/"+url.PathEscape(artwork.albumID)+"/cover",
	)
	return err
}
