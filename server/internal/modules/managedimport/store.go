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

func (store *Store) CreateJob(ctx context.Context, batchID, clientFileID string) (_ Job, returnErr error) {
	job := Job{ID: uuid.NewString(), Status: STATUS_UPLOADING, Revision: 1}
	if batchID == "" {
		_, err := store.database.ExecContext(ctx, `
			INSERT INTO managed_import_jobs (id, status, revision, client_file_id)
			VALUES (?, ?, ?, NULLIF(?, ''))`, job.ID, job.Status, job.Revision, clientFileID)
		if err != nil {
			return Job{}, fmt.Errorf("create Managed Import Job: %w", err)
		}
		return job, nil
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin Managed Import Batch file creation: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import Batch file creation: %w", rollbackErr))
		}
	}()
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO managed_import_jobs (id, status, revision, batch_id, batch_position, client_file_id)
		SELECT ?, ?, ?, id, (SELECT COALESCE(MAX(batch_position), 0) + 1 FROM managed_import_jobs WHERE batch_id = ?), NULLIF(?, '')
		FROM managed_import_batches WHERE id = ? AND status = ?`,
		job.ID, job.Status, job.Revision, batchID, clientFileID, batchID, BATCH_STATUS_UPLOADING)
	if err != nil {
		return Job{}, fmt.Errorf("create Managed Import Job: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return Job{}, ErrInvalidState
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, batchID); err != nil {
		return Job{}, fmt.Errorf("revise Managed Import Batch: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit Managed Import Batch file creation: %w", err)
	}
	return job, nil
}

func (store *Store) CreateBatch(ctx context.Context) (Batch, error) {
	batch := Batch{ID: uuid.NewString(), Status: BATCH_STATUS_UPLOADING, Revision: 1, Files: []BatchFile{}}
	_, err := store.database.ExecContext(ctx, `INSERT INTO managed_import_batches (id, status, revision) VALUES (?, ?, ?)`, batch.ID, batch.Status, batch.Revision)
	if err != nil {
		return Batch{}, fmt.Errorf("create Managed Import Batch: %w", err)
	}
	return batch, nil
}

func (store *Store) GetJob(ctx context.Context, jobID string) (importJob, error) {
	return getImportJob(ctx, store.database, jobID)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getImportJob(ctx context.Context, queryer queryRower, jobID string) (importJob, error) {
	var job importJob
	var batchID, clientFileID, originalFilename, stagedFilePath, contentSHA256, errorCode, trackID sql.NullString
	var previewJSON, errorField, errorReason, outcome sql.NullString
	err := queryer.QueryRowContext(ctx, `
		SELECT id, status, revision, validation_progress, batch_id, client_file_id, original_filename, staged_file_path,
			content_sha256, error_code, track_id, preview_json, error_field, error_reason, outcome, selected
		FROM managed_import_jobs WHERE id = ?`, jobID,
	).Scan(&job.ID, &job.Status, &job.Revision, &job.ValidationProgress, &batchID, &clientFileID, &originalFilename, &stagedFilePath, &contentSHA256, &errorCode, &trackID, &previewJSON, &errorField, &errorReason, &outcome, &job.Selected)
	if errors.Is(err, sql.ErrNoRows) {
		return importJob{}, ErrNotFound
	}
	if err != nil {
		return importJob{}, fmt.Errorf("get Managed Import Job %q: %w", jobID, err)
	}
	job.BatchID = batchID.String
	job.ClientFileID = clientFileID.String
	job.OriginalFilename = originalFilename.String
	job.StagedFilePath = stagedFilePath.String
	job.ContentSHA256 = contentSHA256.String
	job.ErrorCode = errorCode.String
	job.TrackID = trackID.String
	job.PreviewJSON = previewJSON.String
	job.ErrorField = errorField.String
	job.ErrorReason = errorReason.String
	job.Outcome = ImportOutcome(outcome.String)
	return job, nil
}

func (store *Store) GetBatch(ctx context.Context, batchID string) (Batch, error) {
	var batch Batch
	err := store.database.QueryRowContext(ctx, `SELECT id, status, revision FROM managed_import_batches WHERE id = ?`, batchID).Scan(&batch.ID, &batch.Status, &batch.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("get Managed Import Batch %q: %w", batchID, err)
	}
	jobs, err := store.ListBatchJobs(ctx, batchID)
	if err != nil {
		return Batch{}, err
	}
	batch.Files = make([]BatchFile, 0, len(jobs))
	for _, job := range jobs {
		file, err := batchFileFromJob(job)
		if err != nil {
			return Batch{}, err
		}
		batch.Files = append(batch.Files, file)
	}
	return batch, nil
}

func (store *Store) GetBatchStatus(ctx context.Context, batchID string) (BatchStatus, error) {
	var status BatchStatus
	err := store.database.QueryRowContext(ctx, `SELECT status FROM managed_import_batches WHERE id = ?`, batchID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get Managed Import Batch %q status: %w", batchID, err)
	}
	return status, nil
}

func (store *Store) ListBatchJobs(ctx context.Context, batchID string) (jobs []importJob, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `SELECT id FROM managed_import_jobs WHERE batch_id = ? ORDER BY batch_position`, batchID)
	if err != nil {
		return nil, fmt.Errorf("list Managed Import Batch files: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return nil, fmt.Errorf("scan Managed Import Batch file: %w", err)
		}
		job, err := store.GetJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Managed Import Batch files: %w", err)
	}
	return jobs, nil
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

func (store *Store) ReserveBatchUpload(ctx context.Context, jobID string, uploadSize, batchLimit int64) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET upload_size_bytes = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND batch_id IS NOT NULL AND ? + COALESCE((
			SELECT SUM(sibling.upload_size_bytes) FROM managed_import_jobs AS sibling
			WHERE sibling.batch_id = managed_import_jobs.batch_id AND sibling.id != managed_import_jobs.id
		), 0) <= ?`, uploadSize, jobID, STATUS_UPLOADING, uploadSize, batchLimit)
	if err != nil {
		return fmt.Errorf("reserve Managed Import Batch upload bytes: %w", err)
	}
	if err := requireMutation(result); err == nil {
		return nil
	}
	job, getErr := store.GetJob(ctx, jobID)
	if getErr != nil {
		return getErr
	}
	if job.Status != STATUS_UPLOADING || job.BatchID == "" {
		return ErrInvalidState
	}
	return ErrBatchTooLarge
}

func (store *Store) MarkPreview(ctx context.Context, jobID, originalFilename, stagedFilePath, contentSHA256, previewJSON string, uploadSize, batchLimit int64) (_ importJob, returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return importJob{}, fmt.Errorf("begin Import Preview transition: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Import Preview transition: %w", rollbackErr))
		}
	}()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, revision = revision + 1, original_filename = ?, staged_file_path = ?,
			content_sha256 = ?, preview_json = ?, upload_size_bytes = ?, error_code = NULL,
			error_field = NULL, error_reason = NULL, outcome = NULL,
			validation_progress = 100, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND (
			batch_id IS NULL OR ? + COALESCE((
				SELECT SUM(sibling.upload_size_bytes) FROM managed_import_jobs AS sibling
				WHERE sibling.batch_id = managed_import_jobs.batch_id AND sibling.id != managed_import_jobs.id
			), 0) <= ?
		)`,
		STATUS_AWAITING_CONFIRMATION, originalFilename, stagedFilePath, contentSHA256, previewJSON, uploadSize,
		jobID, STATUS_UPLOADING, uploadSize, batchLimit,
	)
	if err != nil {
		return importJob{}, fmt.Errorf("mark Import Preview ready: %w", err)
	}
	if err := requireMutation(result); err != nil {
		job, getErr := getImportJob(ctx, transaction, jobID)
		if getErr != nil {
			return importJob{}, getErr
		}
		if job.Status == STATUS_UPLOADING && job.BatchID != "" {
			return importJob{}, ErrBatchTooLarge
		}
		return importJob{}, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = (SELECT batch_id FROM managed_import_jobs WHERE id = ?)`, jobID); err != nil {
		return importJob{}, fmt.Errorf("revise Managed Import Batch preview: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return importJob{}, fmt.Errorf("commit Import Preview transition: %w", err)
	}
	return store.GetJob(ctx, jobID)
}

func (store *Store) MarkUploadInterrupted(ctx context.Context, jobID, originalFilename string) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin interrupted Managed Import transition: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback interrupted Managed Import transition: %w", rollbackErr))
		}
	}()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET revision = revision + 1, original_filename = COALESCE(NULLIF(?, ''), original_filename),
			error_code = ?, error_field = 'file', error_reason = 'upload stream was interrupted; retry this file',
			outcome = NULL, selected = 0, staged_file_path = NULL, content_sha256 = NULL,
			preview_json = NULL, upload_size_bytes = 0, validation_progress = 0, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND batch_id IS NOT NULL`, originalFilename, UPLOAD_INTERRUPTED_ERROR_CODE, jobID, STATUS_UPLOADING)
	if err != nil {
		return fmt.Errorf("mark Managed Import upload interrupted: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("mark Managed Import upload interrupted: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = (SELECT batch_id FROM managed_import_jobs WHERE id = ?)`, jobID); err != nil {
		return fmt.Errorf("revise interrupted Managed Import Batch file: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit interrupted Managed Import transition: %w", err)
	}
	return nil
}

func (store *Store) MarkFailed(ctx context.Context, jobID, originalFilename, errorCode, errorField, errorReason string) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failed Managed Import transition: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback failed Managed Import transition: %w", rollbackErr))
		}
	}()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, original_filename = COALESCE(NULLIF(?, ''), original_filename), error_code = ?,
			error_field = ?, error_reason = ?, outcome = CASE WHEN batch_id IS NULL THEN NULL ELSE ? END,
			selected = 0, staged_file_path = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?)`, STATUS_FAILED, originalFilename, errorCode, errorField, errorReason,
		OUTCOME_REJECTED, jobID, STATUS_UPLOADING, STATUS_AWAITING_CONFIRMATION)
	if err != nil {
		return fmt.Errorf("mark Managed Import failed: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("mark Managed Import failed: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_batches
		SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT batch_id FROM managed_import_jobs WHERE id = ?) AND status = ?`, jobID, BATCH_STATUS_UPLOADING); err != nil {
		return fmt.Errorf("revise rejected Managed Import Batch file: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit failed Managed Import transition: %w", err)
	}
	return nil
}

func (store *Store) StartBatchConfirmation(ctx context.Context, batchID string, revision int, selectedIDs map[string]bool) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Managed Import Batch confirmation: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import Batch confirmation: %w", rollbackErr))
		}
	}()
	if err := ensureBatchResolved(ctx, transaction, batchID); err != nil {
		return err
	}
	if err := transitionBatchConfirmation(ctx, transaction, batchID, revision); err != nil {
		return err
	}
	if err := updateBatchSelection(ctx, transaction, batchID, selectedIDs); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Managed Import Batch confirmation start: %w", err)
	}
	return nil
}

func ensureBatchResolved(ctx context.Context, transaction *sql.Tx, batchID string) error {
	var unresolvedCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_import_jobs WHERE batch_id = ? AND status = ?`, batchID, STATUS_UPLOADING).Scan(&unresolvedCount); err != nil {
		return fmt.Errorf("count unresolved Managed Import Batch files: %w", err)
	}
	if unresolvedCount > 0 {
		return fmt.Errorf("%w: Managed Import Batch has unresolved files", ErrInvalidState)
	}
	return nil
}

func transitionBatchConfirmation(ctx context.Context, transaction *sql.Tx, batchID string, revision int) error {
	result, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET status = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ? AND revision = ?`,
		BATCH_STATUS_CONFIRMING, batchID, BATCH_STATUS_UPLOADING, revision)
	if err != nil {
		return fmt.Errorf("start Managed Import Batch confirmation: %w", err)
	}
	if err := requireMutation(result); err != nil {
		var currentStatus BatchStatus
		var currentRevision int
		getErr := transaction.QueryRowContext(ctx, `SELECT status, revision FROM managed_import_batches WHERE id = ?`, batchID).Scan(&currentStatus, &currentRevision)
		if errors.Is(getErr, sql.ErrNoRows) {
			return ErrNotFound
		}
		if getErr != nil {
			return fmt.Errorf("inspect Managed Import Batch confirmation conflict: %w", getErr)
		}
		if currentRevision != revision {
			return ErrRevisionConflict
		}
		if currentStatus != BATCH_STATUS_UPLOADING {
			return ErrInvalidState
		}
		return ErrInvalidState
	}
	return nil
}

func updateBatchSelection(ctx context.Context, transaction *sql.Tx, batchID string, selectedIDs map[string]bool) error {
	if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_jobs SET selected = 0 WHERE batch_id = ?`, batchID); err != nil {
		return fmt.Errorf("clear Managed Import Batch selection: %w", err)
	}
	for jobID := range selectedIDs {
		result, err := transaction.ExecContext(ctx, `UPDATE managed_import_jobs SET selected = 1 WHERE id = ? AND batch_id = ? AND status = ?`, jobID, batchID, STATUS_AWAITING_CONFIRMATION)
		if err != nil {
			return fmt.Errorf("select Managed Import Batch file: %w", err)
		}
		if err := requireMutation(result); err != nil {
			return fmt.Errorf("%w: selected file %q is not accepted", ErrInvalidUpload, jobID)
		}
	}
	return nil
}

func (store *Store) MarkBatchFileOutcome(ctx context.Context, jobID string, outcome ImportOutcome, errorCode, errorReason string) error {
	status := STATUS_FAILED
	if outcome == OUTCOME_IMPORTED || outcome == OUTCOME_REPLACED {
		status = STATUS_COMMITTED
	}
	result, err := store.database.ExecContext(ctx, `UPDATE managed_import_jobs SET status = ?, outcome = ?, error_code = NULLIF(?, ''), error_reason = NULLIF(?, ''), staged_file_path = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ? AND outcome IS NULL`,
		status, outcome, errorCode, errorReason, jobID, STATUS_AWAITING_CONFIRMATION)
	if err != nil {
		return fmt.Errorf("record Managed Import Batch file outcome: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("record Managed Import Batch file outcome: %w", err)
	}
	return nil
}

func (store *Store) CompleteBatch(ctx context.Context, batchID string) error {
	result, err := store.database.ExecContext(ctx, `UPDATE managed_import_batches SET status = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		BATCH_STATUS_COMPLETED, batchID, BATCH_STATUS_CONFIRMING)
	if err != nil {
		return fmt.Errorf("complete Managed Import Batch: %w", err)
	}
	return requireMutation(result)
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
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close awaiting Import Preview rows: %w", err))
		}
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

func (store *Store) FindExactDuplicateTrackID(ctx context.Context, contentSHA256 string) (string, error) {
	var trackID string
	err := store.database.QueryRowContext(ctx, `
		SELECT tracks.id
		FROM track_sources
		JOIN tracks ON tracks.id = track_sources.track_id
		WHERE track_sources.content_sha256 = ?`, contentSHA256,
	).Scan(&trackID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect exact Managed Import duplicate: %w", err)
	}
	return trackID, nil
}

func (store *Store) ResolveCommitIdentity(ctx context.Context, metadata library.NormalizedMediaMetadata) (commitIdentity, error) {
	identity := commitIdentity{TrackID: uuid.NewString()}
	albumArtistID, err := store.resolveAlbumArtistID(ctx, metadata.AlbumArtists[0])
	if err != nil {
		return commitIdentity{}, err
	}
	identity.AlbumArtistID = albumArtistID
	var artworkPath, artworkSHA256 sql.NullString
	err = store.database.QueryRowContext(ctx,
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

func (store *Store) resolveAlbumArtistID(ctx context.Context, name string) (string, error) {
	var artistID string
	err := store.database.QueryRowContext(ctx, `SELECT id FROM artists WHERE name_normalized = ?`, normalizeIdentity(name)).Scan(&artistID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.NewString(), nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve primary Album Artist identity: %w", err)
	}
	return artistID, nil
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
	artistIDs, err := upsertArtists(ctx, transaction, data.Inspection.Metadata, data.Identity)
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

func upsertArtists(ctx context.Context, transaction *sql.Tx, metadata library.NormalizedMediaMetadata, identity commitIdentity) (map[string]string, error) {
	artistIDs := make(map[string]string)
	primaryAlbumArtist := normalizeIdentity(metadata.AlbumArtists[0])
	for _, name := range append(append([]string{}, metadata.AlbumArtists...), metadata.Artists...) {
		normalizedName := normalizeIdentity(name)
		if _, exists := artistIDs[normalizedName]; exists {
			continue
		}
		preferredID := ""
		if normalizedName == primaryAlbumArtist {
			preferredID = identity.AlbumArtistID
		}
		artistID, err := upsertArtist(ctx, transaction, name, normalizedName, preferredID)
		if err != nil {
			return nil, err
		}
		artistIDs[normalizedName] = artistID
	}
	return artistIDs, nil
}

func upsertArtist(ctx context.Context, transaction *sql.Tx, name, normalizedName, preferredID string) (string, error) {
	var artistID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM artists WHERE name_normalized = ?`, normalizedName).Scan(&artistID)
	if err == nil {
		if preferredID != "" && artistID != preferredID {
			return "", fmt.Errorf("resolved Artist identity changed during Managed Import commit")
		}
		return artistID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup Artist %q: %w", name, err)
	}
	artistID = preferredID
	if artistID == "" {
		artistID = uuid.NewString()
	}
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
			track_total, disc_total, channel_count, bitrate_bps, codec, container,
			replaygain_track_gain_db, replaygain_track_peak, replaygain_album_gain_db,
			replaygain_album_peak, identity_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		data.Identity.TrackID, data.Identity.AlbumID, metadata.Title, normalizeIdentity(metadata.Title), strings.Join(metadata.Artists, ", "),
		metadata.TrackPosition.Number, audio.DurationMs, audio.Format, fileInfo.Size(), data.Placement.AudioPath, fileInfo.ModTime().Unix(),
		metadata.Genres[0], audio.SampleRateHz, nullablePositive(audio.BitDepth), metadata.DiscPosition.Number,
		nullablePositive(metadata.TrackPosition.Total), nullablePositive(metadata.DiscPosition.Total), audio.ChannelCount,
		audio.BitrateKbps*BITS_PER_KILOBIT, audio.Codec, audio.Container, metadata.ReplayGain.TrackGainDB,
		metadata.ReplayGain.TrackPeak, metadata.ReplayGain.AlbumGainDB, metadata.ReplayGain.AlbumPeak, trackIdentityKey(metadata),
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
			outcome = CASE WHEN batch_id IS NULL THEN NULL ELSE ? END, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND revision = ?`,
		STATUS_COMMITTED, trackID, OUTCOME_IMPORTED, job.ID, STATUS_AWAITING_CONFIRMATION, job.Revision,
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
