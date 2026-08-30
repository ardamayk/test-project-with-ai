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
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/dhowden/tag"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MAX_LEGACY_ARTWORK_SIZE_BYTES = 20 * 1024 * 1024
	MAX_LEGACY_ARTWORK_PIXELS     = 50_000_000
)

var legacyGenreDelimiterPattern = regexp.MustCompile(`[;/|,]+`)

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

	if err := backfillLegacyLibraryRows(ctx, tx); err != nil {
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

func backfillLegacyLibraryRows(ctx context.Context, tx *sql.Tx) error {
	if err := normalizeLegacyArtists(ctx, tx); err != nil {
		return err
	}
	if err := createMissingLegacyArtists(ctx, tx); err != nil {
		return err
	}
	if err := backfillLegacyIdentityKeys(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_sources (
			id, track_id, source_kind, file_path, source_format, size_bytes
		) SELECT 'legacy-source:' || id, id, 'legacy', file_path, lower(format), size_bytes
		FROM tracks
		WHERE NOT EXISTS (SELECT 1 FROM track_sources WHERE track_sources.track_id = tracks.id)`); err != nil {
		return fmt.Errorf("backfill legacy Track sources: %w", err)
	}
	if err := backfillLegacyAlbumArtists(ctx, tx); err != nil {
		return err
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
		artistID, err := findOrCreateLegacyArtist(ctx, tx, artist.name)
		if err != nil {
			return fmt.Errorf("backfill legacy Track Artist %q: %w", artist.name, err)
		}
		if err := relateLegacyTrackArtist(ctx, tx, artist.id, artistID); err != nil {
			return fmt.Errorf("relate legacy Track %q to Artist: %w", artist.id, err)
		}
	}
	return nil
}

func backfillLegacyAlbumArtists(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, artist_id FROM albums`)
	if err != nil {
		return fmt.Errorf("list legacy Album Artists: %w", err)
	}
	albums, err := scanLegacyNames(rows)
	if err != nil {
		return fmt.Errorf("read legacy Album Artists: %w", err)
	}
	for _, album := range albums {
		if err := relateLegacyAlbumArtist(ctx, tx, album.id, album.name); err != nil {
			return fmt.Errorf("relate legacy Album %q to Artist: %w", album.id, err)
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
	for _, trackGenres := range genres {
		for position, genreName := range splitLegacyGenres(trackGenres.name) {
			genreID, err := findOrCreateLegacyGenre(ctx, tx, genreName)
			if err != nil {
				return fmt.Errorf("backfill legacy Genre %q: %w", genreName, err)
			}
			if err := relateLegacyTrackGenre(ctx, tx, trackGenres.id, genreID, position); err != nil {
				return fmt.Errorf("relate legacy Track %q to Genre: %w", trackGenres.id, err)
			}
		}
	}
	return nil
}

func findOrCreateLegacyArtist(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	normalizedName := normalizeLegacyName(name)
	var existingID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM artists WHERE name_normalized = ?`, normalizedName).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	entityID := deterministicLegacyID("legacy-artist", normalizedName)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO artists (id, name, name_sort, name_normalized) VALUES (?, ?, ?, ?)`,
		entityID, normalizeLegacyDisplayName(name), normalizedName, normalizedName,
	)
	return entityID, err
}

func findOrCreateLegacyGenre(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	normalizedName := normalizeLegacyName(name)
	var existingID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM genres WHERE name_normalized = ?`, normalizedName).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	entityID := deterministicLegacyID("legacy-genre", normalizedName)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO genres (id, name, name_normalized) VALUES (?, ?, ?)`,
		entityID, normalizeLegacyDisplayName(name), normalizedName,
	)
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
	return cases.Fold().String(normalizeLegacyDisplayName(value))
}

func normalizeLegacyDisplayName(value string) string {
	return strings.Join(strings.Fields(norm.NFC.String(value)), " ")
}

func splitLegacyGenres(value string) []string {
	parts := legacyGenreDelimiterPattern.Split(value, -1)
	genres := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		displayName := normalizeLegacyDisplayName(part)
		normalizedName := normalizeLegacyName(displayName)
		if displayName == "" {
			continue
		}
		if _, exists := seen[normalizedName]; exists {
			continue
		}
		seen[normalizedName] = struct{}{}
		genres = append(genres, displayName)
	}
	return genres
}

func deterministicLegacyID(prefix string, value string) string {
	hash := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s:%x", prefix, hash)
}

func relateLegacyTrackArtist(ctx context.Context, tx *sql.Tx, trackID string, artistID string) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_artists (track_id, artist_id, position) VALUES (?, ?, 0)`, trackID, artistID); err != nil {
		return err
	}
	var relatedArtistID string
	if err := tx.QueryRowContext(ctx, `SELECT artist_id FROM track_artists WHERE track_id = ? AND position = 0`, trackID).Scan(&relatedArtistID); err != nil {
		return err
	}
	if relatedArtistID != artistID {
		return fmt.Errorf("position 0 references Artist %q, want %q", relatedArtistID, artistID)
	}
	return nil
}

func relateLegacyAlbumArtist(ctx context.Context, tx *sql.Tx, albumID string, artistID string) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO album_artists (album_id, artist_id, position) VALUES (?, ?, 0)`, albumID, artistID); err != nil {
		return err
	}
	var relatedArtistID string
	if err := tx.QueryRowContext(ctx, `SELECT artist_id FROM album_artists WHERE album_id = ? AND position = 0`, albumID).Scan(&relatedArtistID); err != nil {
		return err
	}
	if relatedArtistID != artistID {
		return fmt.Errorf("position 0 references Artist %q, want %q", relatedArtistID, artistID)
	}
	return nil
}

func relateLegacyTrackGenre(ctx context.Context, tx *sql.Tx, trackID string, genreID string, position int) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_genres (track_id, genre_id, position) VALUES (?, ?, ?)`, trackID, genreID, position); err != nil {
		return err
	}
	var relatedGenreID string
	if err := tx.QueryRowContext(ctx, `SELECT genre_id FROM track_genres WHERE track_id = ? AND position = ?`, trackID, position).Scan(&relatedGenreID); err != nil {
		return err
	}
	if relatedGenreID != genreID {
		return fmt.Errorf("position %d references Genre %q, want %q", position, relatedGenreID, genreID)
	}
	return nil
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
	rows, err := tx.QueryContext(ctx, `SELECT tracks.id, tracks.title
		FROM tracks
		WHERE tracks.identity_key IS NULL
		AND (
			tracks.missing_at IS NOT NULL OR tracks.track_no IS NULL OR NOT EXISTS (
				SELECT 1 FROM tracks AS conflicting
				WHERE conflicting.id != tracks.id
				AND conflicting.album_id = tracks.album_id
				AND conflicting.missing_at IS NULL
				AND COALESCE(conflicting.disc_no, 1) = COALESCE(tracks.disc_no, 1)
				AND conflicting.track_no = tracks.track_no
			)
		)`)
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
	albumID string
	data    []byte
}

type legacyTrackFile struct {
	id       string
	filePath string
}

type legacyArtworkMetadata struct {
	contentHash string
	mediaType   string
	width       int
	height      int
	sizeBytes   int
}

func backfillLegacyArtwork(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT albums.id, albums.cover_data
		FROM albums
		WHERE length(COALESCE(albums.cover_data, x'')) > 0
		AND NOT EXISTS (SELECT 1 FROM album_artwork WHERE album_artwork.album_id = albums.id)`)
	if err != nil {
		return fmt.Errorf("list legacy Album artwork: %w", err)
	}
	artwork, err := scanLegacyArtwork(rows)
	if err != nil {
		return fmt.Errorf("read legacy Album artwork: %w", err)
	}
	for _, item := range artwork {
		isStored, err := storeLegacyArtwork(ctx, tx, item)
		if err != nil {
			return fmt.Errorf("backfill legacy Album artwork %q: %w", item.albumID, err)
		}
		if !isStored {
			slog.Warn("legacy Album artwork metadata could not be verified; preserving legacy artwork only", "albumId", item.albumID)
		}
	}
	return nil
}

func scanLegacyArtwork(rows *sql.Rows) (artwork []legacyArtwork, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var item legacyArtwork
		if err := rows.Scan(&item.albumID, &item.data); err != nil {
			return nil, err
		}
		artwork = append(artwork, item)
	}
	return artwork, rows.Err()
}

func storeLegacyArtwork(ctx context.Context, tx *sql.Tx, artwork legacyArtwork) (bool, error) {
	tracks, err := listLegacyArtworkTracks(ctx, tx, artwork.albumID)
	if err != nil {
		return false, err
	}
	sourceTrackID, err := verifyLegacyArtworkFiles(tracks, artwork.data)
	if err != nil {
		slog.Warn("legacy Album artwork source could not be verified", "albumId", artwork.albumID, "error", err)
		return false, nil
	}
	metadata, isValid := inspectLegacyArtwork(artwork.data)
	if sourceTrackID == "" || !isValid {
		return false, nil
	}
	return insertLegacyArtwork(ctx, tx, artwork.albumID, sourceTrackID, metadata)
}

func inspectLegacyArtwork(data []byte) (legacyArtworkMetadata, bool) {
	if len(data) == 0 || len(data) > MAX_LEGACY_ARTWORK_SIZE_BYTES {
		return legacyArtworkMetadata{}, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return legacyArtworkMetadata{}, false
	}
	mediaType, isSupported := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format]
	if !isSupported || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MAX_LEGACY_ARTWORK_PIXELS {
		return legacyArtworkMetadata{}, false
	}
	return legacyArtworkMetadata{
		contentHash: fmt.Sprintf("%x", sha256.Sum256(data)),
		mediaType:   mediaType,
		width:       config.Width,
		height:      config.Height,
		sizeBytes:   len(data),
	}, true
}

func insertLegacyArtwork(ctx context.Context, tx *sql.Tx, albumID string, sourceTrackID string, metadata legacyArtworkMetadata) (bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO album_artwork (
		id, album_id, source_track_id, content_sha256, media_type,
		width, height, encoded_size_bytes, file_path
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(album_id) DO NOTHING`,
		deterministicLegacyID("legacy-artwork", albumID), albumID, sourceTrackID,
		metadata.contentHash, metadata.mediaType, metadata.width, metadata.height, metadata.sizeBytes,
		"legacy-db://albums/"+url.PathEscape(albumID)+"/cover",
	)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read inserted artwork row count: %w", err)
	}
	if rowsAffected != 1 {
		return false, errors.New("deterministic artwork identity conflicts with an existing row")
	}
	return true, nil
}

func listLegacyArtworkTracks(ctx context.Context, tx *sql.Tx, albumID string) (tracks []legacyTrackFile, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, file_path FROM tracks
		WHERE album_id = ? AND missing_at IS NULL ORDER BY id`, albumID)
	if err != nil {
		return nil, fmt.Errorf("list Album Tracks: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var track legacyTrackFile
		if err := rows.Scan(&track.id, &track.filePath); err != nil {
			return nil, fmt.Errorf("read Album Track: %w", err)
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
}

func verifyLegacyArtworkFiles(tracks []legacyTrackFile, expectedArtwork []byte) (string, error) {
	if len(tracks) == 0 {
		return "", errors.New("Album has no active Tracks")
	}
	for _, track := range tracks {
		artwork, err := readLegacyTrackArtwork(track.filePath)
		if err != nil {
			return "", fmt.Errorf("read Track %q artwork: %w", track.id, err)
		}
		if !bytes.Equal(artwork, expectedArtwork) {
			return "", fmt.Errorf("Track %q artwork differs from the Album artwork", track.id)
		}
	}
	return tracks[0].id, nil
}

func readLegacyTrackArtwork(filePath string) (artwork []byte, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	metadata, err := tag.ReadFrom(file)
	if err != nil {
		return nil, err
	}
	picture := metadata.Picture()
	if picture == nil || len(picture.Data) == 0 {
		return nil, errors.New("embedded artwork is missing")
	}
	return picture.Data, nil
}
