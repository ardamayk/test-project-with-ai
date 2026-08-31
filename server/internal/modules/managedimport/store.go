package managedimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type Store struct {
	database *sql.DB
}

type commitData struct {
	Job        importJob
	Identity   commitIdentity
	Placement  placedFiles
	Inspection library.MediaInspection
}

func NewStore(database *sql.DB) *Store {
	return &Store{database: database}
}

func (store *Store) CreateJob(ctx context.Context) (Job, error) {
	job := Job{ID: uuid.NewString(), Status: STATUS_UPLOADING, Revision: 1}
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO managed_import_jobs (id, status, revision)
		VALUES (?, ?, ?)`, job.ID, job.Status, job.Revision)
	if err != nil {
		return Job{}, fmt.Errorf("create Managed Import Job: %w", err)
	}
	return job, nil
}

func (store *Store) GetJob(ctx context.Context, jobID string) (importJob, error) {
	var job importJob
	var originalFilename, stagedFilePath, contentSHA256, errorCode, trackID sql.NullString
	err := store.database.QueryRowContext(ctx, `
		SELECT id, status, revision, validation_progress, original_filename, staged_file_path,
			content_sha256, error_code, track_id
		FROM managed_import_jobs WHERE id = ?`, jobID,
	).Scan(&job.ID, &job.Status, &job.Revision, &job.ValidationProgress, &originalFilename, &stagedFilePath, &contentSHA256, &errorCode, &trackID)
	if errors.Is(err, sql.ErrNoRows) {
		return importJob{}, ErrNotFound
	}
	if err != nil {
		return importJob{}, fmt.Errorf("get Managed Import Job %q: %w", jobID, err)
	}
	job.OriginalFilename = originalFilename.String
	job.StagedFilePath = stagedFilePath.String
	job.ContentSHA256 = contentSHA256.String
	job.ErrorCode = errorCode.String
	job.TrackID = trackID.String
	return job, nil
}

func (store *Store) UpdateValidationProgress(ctx context.Context, jobID string, progress int) error {
	if progress < 0 || progress > 100 {
		return fmt.Errorf("managed import validation progress must be between 0 and 100: got %d", progress)
	}
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET validation_progress = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND validation_progress < ?`,
		progress, jobID, STATUS_UPLOADING, progress,
	)
	if err != nil {
		return fmt.Errorf("update Managed Import validation progress: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Managed Import validation progress result: %w", err)
	}
	if affected == 0 {
		job, getErr := store.GetJob(ctx, jobID)
		if getErr != nil {
			return getErr
		}
		if job.Status != STATUS_UPLOADING {
			return ErrInvalidState
		}
	}
	return nil
}

func (store *Store) MarkPreview(ctx context.Context, jobID, originalFilename, stagedFilePath, contentSHA256 string) (importJob, error) {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, revision = revision + 1, original_filename = ?, staged_file_path = ?,
			content_sha256 = ?, error_code = NULL, validation_progress = 100, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ?`,
		STATUS_AWAITING_CONFIRMATION, originalFilename, stagedFilePath, contentSHA256, jobID, STATUS_UPLOADING,
	)
	if err != nil {
		return importJob{}, fmt.Errorf("mark Import Preview ready: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return importJob{}, err
	}
	return store.GetJob(ctx, jobID)
}

func (store *Store) MarkFailed(ctx context.Context, jobID, errorCode string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, error_code = ?, staged_file_path = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ?`, STATUS_FAILED, errorCode, jobID, STATUS_UPLOADING)
	if err != nil {
		return fmt.Errorf("mark Managed Import failed: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("mark Managed Import failed: %w", err)
	}
	return nil
}

func (store *Store) AlbumRequiresDiscNumber(ctx context.Context, metadata library.NormalizedMediaMetadata) (bool, error) {
	var requiresDiscNumber bool
	err := store.database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM albums
			JOIN tracks ON tracks.album_id = albums.id
			WHERE albums.identity_key = ? AND tracks.missing_at IS NULL
				AND (tracks.disc_no > 1 OR tracks.disc_total > 1)
		)`, albumIdentityKey(metadata),
	).Scan(&requiresDiscNumber)
	if err != nil {
		return false, fmt.Errorf("inspect existing Album disc positions: %w", err)
	}
	return requiresDiscNumber, nil
}

func (store *Store) AwaitingPreviewPaths(ctx context.Context, excludedJobID string) (paths []string, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT staged_file_path FROM managed_import_jobs
		WHERE status = ? AND id != ? AND staged_file_path IS NOT NULL`, STATUS_AWAITING_CONFIRMATION, excludedJobID)
	if err != nil {
		return nil, fmt.Errorf("list awaiting Import Preview files: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan awaiting Import Preview file: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate awaiting Import Preview files: %w", err)
	}
	return paths, nil
}

func (store *Store) AlbumPositionTotalConflict(ctx context.Context, metadata library.NormalizedMediaMetadata) (string, error) {
	var hasDiscConflict, hasTrackConflict bool
	err := store.database.QueryRowContext(ctx, `
		WITH matching_tracks AS (
			SELECT tracks.disc_no, tracks.disc_total, tracks.track_no, tracks.track_total
			FROM albums JOIN tracks ON tracks.album_id = albums.id
			WHERE albums.identity_key = ? AND tracks.missing_at IS NULL
		)
		SELECT
			EXISTS (
				SELECT 1 FROM matching_tracks
				WHERE ? > 0 AND ((disc_total IS NOT NULL AND disc_total != ?) OR disc_no > ?)
			),
			EXISTS (
				SELECT 1 FROM matching_tracks
				WHERE disc_no = ? AND ? > 0
					AND ((track_total IS NOT NULL AND track_total != ?) OR track_no > ?)
			)`,
		albumIdentityKey(metadata),
		metadata.DiscPosition.Total,
		metadata.DiscPosition.Total,
		metadata.DiscPosition.Total,
		metadata.DiscPosition.Number,
		metadata.TrackPosition.Total,
		metadata.TrackPosition.Total,
		metadata.TrackPosition.Total,
	).Scan(&hasDiscConflict, &hasTrackConflict)
	if err != nil {
		return "", fmt.Errorf("inspect existing Album position totals: %w", err)
	}
	if hasDiscConflict {
		return "TOTALDISCS", nil
	}
	if hasTrackConflict {
		return "TOTALTRACKS", nil
	}
	return "", nil
}

func (store *Store) ResolveCommitIdentity(ctx context.Context, metadata library.NormalizedMediaMetadata) (commitIdentity, error) {
	identity := commitIdentity{TrackID: uuid.NewString()}
	var artworkPath, artworkSHA256 sql.NullString
	err := store.database.QueryRowContext(ctx,
		`SELECT albums.id, album_artwork.file_path, album_artwork.content_sha256
		FROM albums
		LEFT JOIN album_artwork ON album_artwork.album_id = albums.id
		WHERE albums.identity_key = ?`, albumIdentityKey(metadata),
	).Scan(&identity.AlbumID, &artworkPath, &artworkSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		identity.AlbumID = uuid.NewString()
		return identity, nil
	}
	if err != nil {
		return commitIdentity{}, fmt.Errorf("resolve Managed Import Album identity: %w", err)
	}
	identity.ExistingArtworkPath = artworkPath.String
	identity.ExistingArtworkSHA256 = artworkSHA256.String
	return identity, nil
}

func (store *Store) Commit(ctx context.Context, data commitData) (result Result, returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin Managed Import commit: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import transaction: %w", rollbackErr))
		}
	}()
	artistIDs, err := upsertArtists(ctx, transaction, data.Inspection.Metadata)
	if err != nil {
		return Result{}, err
	}
	err = upsertAlbum(ctx, transaction, data, artistIDs)
	if err != nil {
		return Result{}, err
	}
	err = insertTrack(ctx, transaction, data)
	if err != nil {
		return Result{}, err
	}
	err = insertRelationships(ctx, transaction, data, artistIDs)
	if err != nil {
		return Result{}, err
	}
	err = insertArtwork(ctx, transaction, data)
	if err != nil {
		return Result{}, err
	}
	result, err = markCommitted(ctx, transaction, data.Job, data.Identity.TrackID)
	if err != nil {
		return Result{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit Managed Import database transaction: %w", err)
	}
	return result, nil
}

func upsertArtists(ctx context.Context, transaction *sql.Tx, metadata library.NormalizedMediaMetadata) (map[string]string, error) {
	artistIDs := make(map[string]string)
	for _, name := range append(append([]string{}, metadata.AlbumArtists...), metadata.Artists...) {
		normalizedName := normalizeIdentity(name)
		if _, exists := artistIDs[normalizedName]; exists {
			continue
		}
		artistID, err := upsertArtist(ctx, transaction, name, normalizedName)
		if err != nil {
			return nil, err
		}
		artistIDs[normalizedName] = artistID
	}
	return artistIDs, nil
}

func upsertArtist(ctx context.Context, transaction *sql.Tx, name, normalizedName string) (string, error) {
	var artistID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM artists WHERE name_normalized = ?`, normalizedName).Scan(&artistID)
	if err == nil {
		return artistID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup Artist %q: %w", name, err)
	}
	artistID = uuid.NewString()
	_, err = transaction.ExecContext(ctx,
		`INSERT INTO artists (id, name, name_sort, name_normalized) VALUES (?, ?, ?, ?)`,
		artistID, name, normalizedName, normalizedName,
	)
	if err != nil {
		return "", fmt.Errorf("create Artist %q: %w", name, err)
	}
	return artistID, nil
}

func upsertAlbum(ctx context.Context, transaction *sql.Tx, data commitData, artistIDs map[string]string) error {
	metadata := data.Inspection.Metadata
	primaryArtistID := artistIDs[normalizeIdentity(metadata.AlbumArtists[0])]
	var existingID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM albums WHERE identity_key = ?`, albumIdentityKey(metadata)).Scan(&existingID)
	if err == nil {
		if existingID != data.Identity.AlbumID {
			return fmt.Errorf("resolved Album identity changed during Managed Import commit")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lookup Managed Import Album: %w", err)
	}
	genres, err := json.Marshal(metadata.Genres)
	if err != nil {
		return fmt.Errorf("encode Managed Import Album Genres: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO albums (id, artist_id, title, title_sort, year, release_date, genres, identity_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		data.Identity.AlbumID, primaryArtistID, metadata.Album, normalizeIdentity(metadata.Album), nullablePositive(metadata.Year), releaseDate(metadata.Year), string(genres), albumIdentityKey(metadata),
	)
	if err != nil {
		return fmt.Errorf("create Managed Import Album: %w", err)
	}
	return nil
}

func insertTrack(ctx context.Context, transaction *sql.Tx, data commitData) error {
	metadata := data.Inspection.Metadata
	audio := data.Inspection.Audio
	fileInfo, err := os.Stat(data.Placement.AudioPath)
	if err != nil {
		return fmt.Errorf("stat canonical Managed Track: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO tracks (
			id, album_id, title, title_sort, artist_name, track_no, duration_ms, format,
			size_bytes, file_path, file_mtime, genre, sample_rate_hz, bit_depth, disc_no,
			track_total, disc_total, channel_count, bitrate_bps, codec, container, identity_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		data.Identity.TrackID, data.Identity.AlbumID, metadata.Title, normalizeIdentity(metadata.Title), strings.Join(metadata.Artists, ", "),
		metadata.TrackPosition.Number, audio.DurationMs, audio.Format, fileInfo.Size(), data.Placement.AudioPath, fileInfo.ModTime().Unix(),
		metadata.Genres[0], audio.SampleRateHz, audio.BitDepth, metadata.DiscPosition.Number,
		nullablePositive(metadata.TrackPosition.Total), nullablePositive(metadata.DiscPosition.Total), audio.ChannelCount,
		audio.BitrateKbps*BITS_PER_KILOBIT, audio.Codec, audio.Container, trackIdentityKey(metadata),
	)
	if err != nil {
		return fmt.Errorf("create Managed Track: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO track_sources (
			id, track_id, source_kind, file_path, content_sha256, source_format, size_bytes
		) VALUES (?, ?, 'managed', ?, ?, ?, ?)`,
		uuid.NewString(), data.Identity.TrackID, data.Placement.AudioPath, data.Inspection.FileSHA256, audio.Format, fileInfo.Size(),
	)
	if err != nil {
		return fmt.Errorf("create Managed Track source: %w", err)
	}
	return nil
}

func insertRelationships(ctx context.Context, transaction *sql.Tx, data commitData, artistIDs map[string]string) error {
	metadata := data.Inspection.Metadata
	if err := insertAlbumArtistCredits(ctx, transaction, data.Identity.AlbumID, metadata.AlbumArtists, artistIDs); err != nil {
		return err
	}
	if err := insertTrackArtistCredits(ctx, transaction, data.Identity.TrackID, metadata.Artists, artistIDs); err != nil {
		return err
	}
	for position, genreName := range metadata.Genres {
		genreID, err := upsertGenre(ctx, transaction, genreName)
		if err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx,
			`INSERT INTO track_genres (track_id, genre_id, position) VALUES (?, ?, ?)`,
			data.Identity.TrackID, genreID, position,
		); err != nil {
			return fmt.Errorf("create Track Genre %q: %w", genreName, err)
		}
	}
	return nil
}

func insertAlbumArtistCredits(ctx context.Context, transaction *sql.Tx, albumID string, names []string, artistIDs map[string]string) error {
	return insertArtistCredits(ctx, transaction, `INSERT OR IGNORE INTO album_artists (album_id, artist_id, position) VALUES (?, ?, ?)`, albumID, names, artistIDs)
}

func insertTrackArtistCredits(ctx context.Context, transaction *sql.Tx, trackID string, names []string, artistIDs map[string]string) error {
	return insertArtistCredits(ctx, transaction, `INSERT INTO track_artists (track_id, artist_id, position) VALUES (?, ?, ?)`, trackID, names, artistIDs)
}

func insertArtistCredits(ctx context.Context, transaction *sql.Tx, query, ownerID string, names []string, artistIDs map[string]string) error {
	for position, name := range names {
		if _, err := transaction.ExecContext(ctx, query, ownerID, artistIDs[normalizeIdentity(name)], position); err != nil {
			return fmt.Errorf("create ordered Artist credit %q: %w", name, err)
		}
	}
	return nil
}

func upsertGenre(ctx context.Context, transaction *sql.Tx, name string) (string, error) {
	normalizedName := normalizeIdentity(name)
	var genreID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM genres WHERE name_normalized = ?`, normalizedName).Scan(&genreID)
	if err == nil {
		return genreID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup Genre %q: %w", name, err)
	}
	genreID = uuid.NewString()
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO genres (id, name, name_normalized) VALUES (?, ?, ?)`, genreID, name, normalizedName,
	); err != nil {
		return "", fmt.Errorf("create Genre %q: %w", name, err)
	}
	return genreID, nil
}

func insertArtwork(ctx context.Context, transaction *sql.Tx, data commitData) error {
	if data.Identity.ExistingArtworkPath != "" {
		return nil
	}
	artwork := data.Inspection.AlbumArtwork
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type, width, height,
			encoded_size_bytes, file_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), data.Identity.AlbumID, data.Identity.TrackID, artwork.SHA256, artwork.MIMEType,
		artwork.Width, artwork.Height, len(artwork.Data), data.Placement.ArtworkPath,
	)
	if err != nil {
		return fmt.Errorf("create Album Artwork: %w", err)
	}
	return nil
}

func markCommitted(ctx context.Context, transaction *sql.Tx, job importJob, trackID string) (Result, error) {
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, revision = revision + 1, track_id = ?, staged_file_path = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND revision = ?`,
		STATUS_COMMITTED, trackID, job.ID, STATUS_AWAITING_CONFIRMATION, job.Revision,
	)
	if err != nil {
		return Result{}, fmt.Errorf("mark Managed Import committed: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return Result{}, err
	}
	return Result{JobID: job.ID, Status: STATUS_COMMITTED, Revision: job.Revision + 1, TrackID: trackID}, nil
}

func requireMutation(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrInvalidState
	}
	return nil
}

func normalizeIdentity(value string) string {
	return cases.Fold().String(strings.Join(strings.Fields(norm.NFC.String(value)), " "))
}

func albumIdentityKey(metadata library.NormalizedMediaMetadata) string {
	credits := make([]string, len(metadata.AlbumArtists))
	for index, name := range metadata.AlbumArtists {
		credits[index] = normalizeIdentity(name)
	}
	return strings.Join(credits, "\x1e") + "\x1f" + normalizeIdentity(metadata.Album) + "\x1f" + fmt.Sprint(metadata.Year)
}

func trackIdentityKey(metadata library.NormalizedMediaMetadata) string {
	return fmt.Sprintf("%d\x1f%d\x1f%s", metadata.DiscPosition.Number, metadata.TrackPosition.Number, normalizeIdentity(metadata.Title))
}

func nullablePositive(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func releaseDate(year int) any {
	if year <= 0 {
		return nil
	}
	return fmt.Sprint(year)
}
