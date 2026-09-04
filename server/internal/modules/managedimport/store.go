package managedimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type Store struct {
	database *sql.DB
}

type historyQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const (
	UNCOMMITTED_BATCH_PREDICATE          = `status != ?`
	UNCOMMITTED_STANDALONE_JOB_PREDICATE = `batch_id IS NULL AND status != ?`
)

type commitData struct {
	Job        importJob
	Identity   commitIdentity
	Placement  placedFiles
	Inspection library.MediaInspection
	AlbumKey   string
}

func NewStore(database *sql.DB) *Store {
	return &Store{database: database}
}

func (store *Store) FindManagedTrackByHash(ctx context.Context, contentSHA256 string) (string, error) {
	var trackID string
	err := store.database.QueryRowContext(ctx, `
		SELECT track_id FROM track_sources
		WHERE source_kind = 'managed' AND content_sha256 = ?`, contentSHA256,
	).Scan(&trackID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find Managed Track by content hash: %w", err)
	}
	return trackID, nil
}

func (store *Store) FindAlbumArtworkHash(ctx context.Context, metadata library.NormalizedMediaMetadata) (string, error) {
	var contentSHA256 string
	err := store.database.QueryRowContext(ctx, `
		SELECT album_artwork.content_sha256
		FROM albums JOIN album_artwork ON album_artwork.album_id = albums.id
		WHERE albums.identity_key = ?`, albumIdentityKey(metadata),
	).Scan(&contentSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find existing Album Artwork hash: %w", err)
	}
	return contentSHA256, nil
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
		SELECT ?, ?, ?, id, (
			SELECT COALESCE(MAX(position), 0) + 1 FROM (
				SELECT batch_position AS position FROM managed_import_jobs WHERE batch_id = ?
				UNION ALL
				SELECT position FROM managed_import_canceled_files WHERE batch_id = ?
			)
		), NULLIF(?, '')
		FROM managed_import_batches
		WHERE id = ? AND status = ? AND NOT EXISTS (
			SELECT 1 FROM managed_import_canceled_files WHERE batch_id = ? AND file_id = NULLIF(?, '')
		)`, job.ID, job.Status, job.Revision, batchID, batchID, clientFileID, batchID,
		BATCH_STATUS_UPLOADING, batchID, clientFileID)
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
	var previewJSON, errorField, errorReason, outcome, replaceTrackID sql.NullString
	err := queryer.QueryRowContext(ctx, `
		SELECT id, status, revision, validation_progress, batch_id, client_file_id, original_filename, staged_file_path,
			content_sha256, error_code, track_id, preview_json, error_field, error_reason, outcome, selected, replace_track_id
		FROM managed_import_jobs WHERE id = ?`, jobID,
	).Scan(&job.ID, &job.Status, &job.Revision, &job.ValidationProgress, &batchID, &clientFileID, &originalFilename, &stagedFilePath, &contentSHA256, &errorCode, &trackID, &previewJSON, &errorField, &errorReason, &outcome, &job.Selected, &replaceTrackID)
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
	job.ReplaceTrackID = replaceTrackID.String
	job.ReplacesTrackID = replaceTrackID.String
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

func (store *Store) ListHistory(ctx context.Context) (_ HistoryList, returnErr error) {
	transaction, err := store.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return HistoryList{}, fmt.Errorf("begin Import History snapshot: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Import History snapshot: %w", rollbackErr))
		}
	}()
	history, err := readHistorySnapshot(ctx, transaction)
	if err != nil {
		return HistoryList{}, err
	}
	if err := transaction.Commit(); err != nil {
		return HistoryList{}, fmt.Errorf("commit Import History snapshot: %w", err)
	}
	return history, nil
}

func readHistorySnapshot(ctx context.Context, queryer historyQueryer) (HistoryList, error) {
	items, err := listHistoryItems(ctx, queryer)
	if err != nil {
		return HistoryList{}, err
	}
	for index := range items {
		items[index].Files, err = listHistoryFiles(ctx, queryer, items[index].ImportID)
		if err != nil {
			return HistoryList{}, err
		}
	}
	return HistoryList{Items: items}, nil
}

func listHistoryItems(ctx context.Context, queryer historyQueryer) (_ []HistoryItem, returnErr error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT import_id, started_at, completed_at, result_code, total_count, imported_count,
			rejected_count, failed_count, replaced_count, not_attempted_count, canceled_count
		FROM managed_import_history
		ORDER BY completed_at DESC, rowid DESC
		LIMIT ?`, IMPORT_HISTORY_LIMIT)
	if err != nil {
		return nil, fmt.Errorf("list Import History: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Import History: %w", closeErr))
		}
	}()
	items := []HistoryItem{}
	for rows.Next() {
		item, err := scanHistoryItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Import History: %w", err)
	}
	return items, nil
}

func scanHistoryItem(scanner historyFileScanner) (HistoryItem, error) {
	var item HistoryItem
	err := scanner.Scan(&item.ImportID, &item.StartedAt, &item.CompletedAt, &item.ResultCode,
		&item.Counts.Total, &item.Counts.Imported, &item.Counts.Rejected, &item.Counts.Failed,
		&item.Counts.Replaced, &item.Counts.NotAttempted, &item.Counts.Canceled)
	if err != nil {
		return HistoryItem{}, fmt.Errorf("read Import History: %w", err)
	}
	return item, nil
}

func listHistoryFiles(ctx context.Context, queryer historyQueryer, importID string) (_ []HistoryFile, returnErr error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT file_id, job_id, safe_filename, started_at, completed_at, content_sha256,
			result_code, created_track_id, replaced_track_id
		FROM managed_import_history_files WHERE import_id = ? ORDER BY position`, importID)
	if err != nil {
		return nil, fmt.Errorf("list Import History files for %q: %w", importID, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Import History files: %w", closeErr))
		}
	}()
	files := []HistoryFile{}
	for rows.Next() {
		var file HistoryFile
		var safeFilename, contentSHA256, createdTrackID, replacedTrackID sql.NullString
		if err := rows.Scan(&file.FileID, &file.JobID, &safeFilename, &file.StartedAt, &file.CompletedAt,
			&contentSHA256, &file.ResultCode, &createdTrackID, &replacedTrackID); err != nil {
			return nil, fmt.Errorf("read Import History file: %w", err)
		}
		file.SafeFilename = safeFilename.String
		file.ContentSHA256 = contentSHA256.String
		file.CreatedTrackID = createdTrackID.String
		file.ReplacedTrackID = replacedTrackID.String
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Import History files: %w", err)
	}
	return files, nil
}

func (store *Store) DeleteBatch(ctx context.Context, batchID string) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Managed Import Batch cancellation: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import Batch cancellation: %w", rollbackErr))
		}
	}()
	if archiveErr := archiveBatchHistory(ctx, transaction, batchID, HISTORY_RESULT_CANCELED); archiveErr != nil {
		return archiveErr
	}
	if _, deleteJobsErr := transaction.ExecContext(ctx, `DELETE FROM managed_import_jobs WHERE batch_id = ?`, batchID); deleteJobsErr != nil {
		return fmt.Errorf("delete canceled Managed Import Batch files: %w", deleteJobsErr)
	}
	result, err := transaction.ExecContext(ctx, `DELETE FROM managed_import_batches WHERE id = ? AND status != ?`, batchID, BATCH_STATUS_COMPLETED)
	if err != nil {
		return fmt.Errorf("delete canceled Managed Import Batch: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Managed Import Batch cancellation: %w", err)
	}
	return nil
}

func (store *Store) DeleteJob(ctx context.Context, jobID string) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Managed Import Job cancellation: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import Job cancellation: %w", rollbackErr))
		}
	}()
	var batchID sql.NullString
	var status ImportStatus
	if err := transaction.QueryRowContext(ctx, `
		SELECT jobs.batch_id, jobs.status
		FROM managed_import_jobs jobs
		LEFT JOIN managed_import_batches batches ON batches.id = jobs.batch_id
		WHERE jobs.id = ?
			AND jobs.status != ?
			AND (jobs.batch_id IS NULL OR batches.status != ?)
	`, jobID, STATUS_COMMITTED, BATCH_STATUS_COMPLETED).Scan(&batchID, &status); errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidState
	} else if err != nil {
		return fmt.Errorf("resolve canceled Managed Import Job: %w", err)
	}
	if !batchID.Valid && status != STATUS_FAILED {
		if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_jobs SET updated_at = ? WHERE id = ?`, time.Now().UTC(), jobID); err != nil {
			return fmt.Errorf("timestamp canceled standalone Managed Import Job: %w", err)
		}
		if err := archiveStandaloneHistory(ctx, transaction, jobID, HISTORY_RESULT_CANCELED); err != nil {
			return err
		}
	}
	if batchID.Valid {
		_, err := transaction.ExecContext(ctx, `
			INSERT INTO managed_import_canceled_files (
				batch_id, file_id, job_id, safe_filename, started_at, completed_at, content_sha256, position
			)
			SELECT batch_id, COALESCE(NULLIF(client_file_id, ''), id), id, original_filename,
				created_at, ?, content_sha256, batch_position
			FROM managed_import_jobs WHERE id = ?`, time.Now().UTC(), jobID)
		if err != nil {
			return fmt.Errorf("retain canceled Managed Import Batch file: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, batchID.String); err != nil {
			return fmt.Errorf("revise canceled Managed Import Batch file: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM managed_import_jobs WHERE id = ?`, jobID); err != nil {
		return fmt.Errorf("delete canceled Managed Import Job: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Managed Import Job cancellation: %w", err)
	}
	return nil
}

func (store *Store) ListUncommittedBatchIDs(ctx context.Context, updatedBefore *time.Time) ([]string, error) {
	query := `SELECT id FROM managed_import_batches WHERE ` + UNCOMMITTED_BATCH_PREDICATE
	arguments := []any{BATCH_STATUS_COMPLETED}
	if updatedBefore != nil {
		query += ` AND updated_at <= ?`
		arguments = append(arguments, *updatedBefore)
	}
	return queryIDs(ctx, store.database, query, arguments...)
}

func (store *Store) ListUncommittedStandaloneJobIDs(ctx context.Context, updatedBefore *time.Time) ([]string, error) {
	query := `SELECT id FROM managed_import_jobs WHERE ` + UNCOMMITTED_STANDALONE_JOB_PREDICATE
	arguments := []any{STATUS_COMMITTED}
	if updatedBefore != nil {
		query += ` AND updated_at <= ?`
		arguments = append(arguments, *updatedBefore)
	}
	return queryIDs(ctx, store.database, query, arguments...)
}

func (store *Store) IsBatchUncommittedBefore(ctx context.Context, batchID string, updatedBefore time.Time) (bool, error) {
	var isEligible bool
	err := store.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_import_batches WHERE id = ? AND `+UNCOMMITTED_BATCH_PREDICATE+` AND updated_at <= ?)`, batchID, BATCH_STATUS_COMPLETED, updatedBefore).Scan(&isEligible)
	if err != nil {
		return false, fmt.Errorf("check inactive Managed Import Batch %q: %w", batchID, err)
	}
	return isEligible, nil
}

func (store *Store) IsStandaloneJobUncommittedBefore(ctx context.Context, jobID string, updatedBefore time.Time) (bool, error) {
	var isEligible bool
	err := store.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_import_jobs WHERE id = ? AND `+UNCOMMITTED_STANDALONE_JOB_PREDICATE+` AND updated_at <= ?)`, jobID, STATUS_COMMITTED, updatedBefore).Scan(&isEligible)
	if err != nil {
		return false, fmt.Errorf("check inactive Managed Import Job %q: %w", jobID, err)
	}
	return isEligible, nil
}

func queryIDs(ctx context.Context, database *sql.DB, query string, arguments ...any) (ids []string, returnErr error) {
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
			error_field = NULL, error_reason = NULL, outcome = NULL, selected = 1,
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

func (store *Store) MarkExactDuplicate(ctx context.Context, jobID, originalFilename, stagedPath, contentSHA256, previewJSON, trackID string, uploadSize int64) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Exact Duplicate transition: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Exact Duplicate transition: %w", rollbackErr))
		}
	}()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, revision = revision + 1, original_filename = ?, content_sha256 = ?,
			track_id = ?, preview_json = ?, error_code = ?, error_field = 'file',
			error_reason = 'file bytes match an existing Track',
			outcome = CASE WHEN batch_id IS NULL THEN NULL ELSE ? END, selected = 0,
			staged_file_path = ?, upload_size_bytes = ?, validation_progress = 100,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ?`, STATUS_FAILED, originalFilename, contentSHA256,
		trackID, previewJSON, ERROR_CODE_EXACT_DUPLICATE, OUTCOME_REJECTED, stagedPath, uploadSize, jobID, STATUS_UPLOADING)
	if err != nil {
		return fmt.Errorf("mark Exact Duplicate: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("mark Exact Duplicate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT batch_id FROM managed_import_jobs WHERE id = ?) AND status = ?`,
		jobID, BATCH_STATUS_UPLOADING); err != nil {
		return fmt.Errorf("revise Exact Duplicate Batch file: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Exact Duplicate transition: %w", err)
	}
	return nil
}

func (store *Store) ClearStagedFile(ctx context.Context, jobID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Managed Import staging cleanup: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs SET staged_file_path = NULL, upload_size_bytes = 0,
			status = CASE WHEN error_code = ? THEN ? ELSE status END,
			outcome = CASE WHEN error_code = ? AND batch_id IS NOT NULL THEN ? ELSE outcome END,
			selected = CASE WHEN error_code = ? THEN 0 ELSE selected END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND staged_file_path IS NOT NULL`, ERROR_CODE_EXACT_DUPLICATE, STATUS_FAILED,
		ERROR_CODE_EXACT_DUPLICATE, OUTCOME_REJECTED, ERROR_CODE_EXACT_DUPLICATE, jobID)
	if err != nil {
		return fmt.Errorf("clear Managed Import staging path: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("clear Managed Import staging path: %w", err)
	}
	if err := archiveStandaloneHistory(ctx, transaction, jobID, HISTORY_RESULT_FAILED); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Managed Import staging cleanup: %w", err)
	}
	return nil
}

func (store *Store) MarkConfirmationExactDuplicate(ctx context.Context, jobID string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, error_code = ?, error_field = 'file',
			error_reason = 'file bytes match an existing Track',
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ?`, STATUS_FAILED, ERROR_CODE_EXACT_DUPLICATE,
		jobID, STATUS_AWAITING_CONFIRMATION)
	if err != nil {
		return fmt.Errorf("mark confirmation Exact Duplicate: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("mark confirmation Exact Duplicate: %w", err)
	}
	return nil
}

func (store *Store) RefreshDuplicatePreview(ctx context.Context, jobID, previewJSON string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin duplicate preview refresh: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs SET preview_json = ?, revision = revision + 1, selected = 0,
			updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		previewJSON, jobID, STATUS_AWAITING_CONFIRMATION)
	if err != nil {
		return fmt.Errorf("refresh duplicate preview: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("refresh duplicate preview: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_batches SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT batch_id FROM managed_import_jobs WHERE id = ?) AND status = ?`,
		jobID, BATCH_STATUS_UPLOADING); err != nil {
		return fmt.Errorf("revise refreshed duplicate batch: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit duplicate preview refresh: %w", err)
	}
	return nil
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
	if err := archiveStandaloneHistory(ctx, transaction, jobID, HISTORY_RESULT_FAILED); err != nil {
		return err
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
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Managed Import Batch completion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `UPDATE managed_import_batches SET status = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		BATCH_STATUS_COMPLETED, batchID, BATCH_STATUS_CONFIRMING)
	if err != nil {
		return fmt.Errorf("complete Managed Import Batch: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return err
	}
	if err := archiveBatchHistory(ctx, transaction, batchID, HISTORY_RESULT_COMPLETED); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE managed_import_jobs SET preview_json = NULL WHERE batch_id = ?`, batchID); err != nil {
		return fmt.Errorf("clear completed Import Preview payloads: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Managed Import Batch completion: %w", err)
	}
	return nil
}

func archiveBatchHistory(ctx context.Context, transaction *sql.Tx, batchID string, terminalCode HistoryResultCode) error {
	var item HistoryItem
	item.ImportID = batchID
	item.CompletedAt = time.Now().UTC()
	if err := transaction.QueryRowContext(ctx, `SELECT created_at FROM managed_import_batches WHERE id = ?`, batchID).Scan(&item.StartedAt); err != nil {
		return fmt.Errorf("read terminal Managed Import Batch %q: %w", batchID, err)
	}
	files, counts, err := readBatchHistoryFiles(ctx, transaction, batchID, terminalCode, item.CompletedAt)
	if err != nil {
		return err
	}
	item.Files = files
	item.Counts = counts
	item.ResultCode = historyResultCode(counts, terminalCode)
	return insertHistory(ctx, transaction, item)
}

func archiveStandaloneHistory(ctx context.Context, transaction *sql.Tx, jobID string, terminalCode HistoryResultCode) error {
	item, exists, err := readStandaloneHistory(ctx, transaction, jobID, terminalCode)
	if err != nil || !exists {
		return err
	}
	if err := insertHistory(ctx, transaction, item); err != nil {
		return err
	}
	// A canceled standalone job is deleted by the caller in the same transaction; clearing its staged path
	// here would violate the awaiting_confirmation CHECK constraint, so only terminal rows are cleared.
	if _, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs SET preview_json = NULL, staged_file_path = NULL
		WHERE id = ? AND batch_id IS NULL AND status IN (?, ?)`, jobID, STATUS_COMMITTED, STATUS_FAILED); err != nil {
		return fmt.Errorf("clear terminal standalone Import payloads: %w", err)
	}
	return nil
}

func readBatchHistoryFiles(ctx context.Context, transaction *sql.Tx, batchID string, terminalCode HistoryResultCode, completedAt time.Time) (_ []HistoryFile, counts HistoryCounts, returnErr error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(client_file_id, ''), id), id, original_filename, created_at, updated_at,
			content_sha256, status, outcome, error_code, track_id, batch_position
		FROM managed_import_jobs WHERE batch_id = ?
		UNION ALL
		SELECT file_id, job_id, safe_filename, started_at, completed_at, content_sha256, ?, ?, ?, NULL, position
		FROM managed_import_canceled_files WHERE batch_id = ?
		ORDER BY batch_position`, batchID, STATUS_FAILED, OUTCOME_NOT_ATTEMPTED, IMPORT_CANCELED_RESULT_CODE, batchID)
	if err != nil {
		return nil, counts, fmt.Errorf("read terminal Managed Import Batch files: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	files := []HistoryFile{}
	for rows.Next() {
		file, outcome, status, errorCode, trackID, err := scanHistorySourceFile(rows, true)
		if err != nil {
			return nil, counts, err
		}
		applyHistoryFileResult(&file, outcome, status, errorCode, trackID, terminalCode, completedAt)
		incrementHistoryCounts(&counts, outcome, status, errorCode, terminalCode)
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, counts, fmt.Errorf("iterate terminal Managed Import Batch files: %w", err)
	}
	return files, counts, nil
}

type historyFileScanner interface {
	Scan(...any) error
}

func scanHistorySourceFile(scanner historyFileScanner, hasPosition bool) (HistoryFile, ImportOutcome, ImportStatus, string, string, error) {
	var file HistoryFile
	var safeFilename, contentSHA256, outcome, errorCode, trackID sql.NullString
	var status ImportStatus
	destinations := []any{&file.FileID, &file.JobID, &safeFilename, &file.StartedAt, &file.CompletedAt,
		&contentSHA256, &status, &outcome, &errorCode, &trackID}
	var position int
	if hasPosition {
		destinations = append(destinations, &position)
	}
	err := scanner.Scan(destinations...)
	if err != nil {
		return HistoryFile{}, "", "", "", "", fmt.Errorf("read terminal Managed Import file: %w", err)
	}
	file.SafeFilename = safeFilename.String
	file.ContentSHA256 = contentSHA256.String
	return file, ImportOutcome(outcome.String), status, errorCode.String, trackID.String, nil
}

func readStandaloneHistory(ctx context.Context, transaction *sql.Tx, jobID string, terminalCode HistoryResultCode) (HistoryItem, bool, error) {
	row := transaction.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(client_file_id, ''), id), id, original_filename, created_at, updated_at,
			content_sha256, status, outcome, error_code, track_id
		FROM managed_import_jobs WHERE id = ? AND batch_id IS NULL`, jobID)
	file, outcome, status, errorCode, trackID, err := scanHistorySourceFile(row, false)
	if errors.Is(err, sql.ErrNoRows) {
		return HistoryItem{}, false, nil
	}
	if err != nil {
		return HistoryItem{}, false, err
	}
	applyHistoryFileResult(&file, outcome, status, errorCode, trackID, terminalCode, file.CompletedAt)
	counts := HistoryCounts{}
	incrementHistoryCounts(&counts, outcome, status, errorCode, terminalCode)
	return HistoryItem{
		ImportID: jobID, StartedAt: file.StartedAt, CompletedAt: file.CompletedAt,
		ResultCode: historyResultCode(counts, terminalCode), Counts: counts, Files: []HistoryFile{file},
	}, true, nil
}

func applyHistoryFileResult(file *HistoryFile, outcome ImportOutcome, status ImportStatus, errorCode, trackID string, terminalCode HistoryResultCode, terminalAt time.Time) {
	if isCanceledHistoryFile(outcome, status, errorCode, terminalCode) {
		file.ResultCode = string(HISTORY_RESULT_CANCELED)
		if errorCode != IMPORT_CANCELED_RESULT_CODE {
			file.CompletedAt = terminalAt
		}
		return
	}
	file.ResultCode = string(outcome)
	if status == STATUS_COMMITTED && file.ResultCode == "" {
		file.ResultCode = string(OUTCOME_IMPORTED)
	} else if file.ResultCode == "" {
		file.ResultCode = string(status)
	}
	if errorCode != "" && outcome != OUTCOME_IMPORTED && outcome != OUTCOME_REPLACED {
		file.ResultCode = errorCode
	}
	if outcome == OUTCOME_REPLACED {
		file.ReplacedTrackID = trackID
	} else if status == STATUS_COMMITTED {
		file.CreatedTrackID = trackID
	}
}

func incrementHistoryCounts(counts *HistoryCounts, outcome ImportOutcome, status ImportStatus, errorCode string, terminalCode HistoryResultCode) {
	counts.Total++
	if isCanceledHistoryFile(outcome, status, errorCode, terminalCode) {
		counts.Canceled++
		return
	}
	switch {
	case outcome == OUTCOME_IMPORTED || outcome == "" && status == STATUS_COMMITTED:
		counts.Imported++
	case outcome == OUTCOME_REJECTED:
		counts.Rejected++
	case outcome == OUTCOME_REPLACED:
		counts.Replaced++
	case outcome == OUTCOME_NOT_ATTEMPTED:
		counts.NotAttempted++
	default:
		counts.Failed++
	}
}

func isCanceledHistoryFile(outcome ImportOutcome, status ImportStatus, errorCode string, terminalCode HistoryResultCode) bool {
	if errorCode == IMPORT_CANCELED_RESULT_CODE {
		return true
	}
	return terminalCode == HISTORY_RESULT_CANCELED && outcome == "" && status != STATUS_COMMITTED && status != STATUS_FAILED
}

func historyResultCode(counts HistoryCounts, terminalCode HistoryResultCode) HistoryResultCode {
	if terminalCode == HISTORY_RESULT_CANCELED {
		return terminalCode
	}
	succeeded := counts.Imported + counts.Replaced
	if succeeded == counts.Total {
		return HISTORY_RESULT_COMPLETED
	}
	if succeeded > 0 {
		return HISTORY_RESULT_PARTIALLY_COMPLETED
	}
	return HISTORY_RESULT_FAILED
}

func insertHistory(ctx context.Context, transaction *sql.Tx, item HistoryItem) error {
	result, err := transaction.ExecContext(ctx, `
		INSERT OR IGNORE INTO managed_import_history (
			import_id, started_at, completed_at, result_code, total_count, imported_count,
			rejected_count, failed_count, replaced_count, not_attempted_count, canceled_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ImportID, item.StartedAt, item.CompletedAt,
		item.ResultCode, item.Counts.Total, item.Counts.Imported, item.Counts.Rejected, item.Counts.Failed,
		item.Counts.Replaced, item.Counts.NotAttempted, item.Counts.Canceled)
	if err != nil {
		return fmt.Errorf("store Import History %q: %w", item.ImportID, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect Import History insert %q: %w", item.ImportID, err)
	}
	if inserted == 0 {
		return nil
	}
	for position, file := range item.Files {
		if insertErr := insertHistoryFile(ctx, transaction, item.ImportID, position, file); insertErr != nil {
			return insertErr
		}
	}
	_, err = transaction.ExecContext(ctx, `DELETE FROM managed_import_history WHERE import_id IN (
		SELECT import_id FROM managed_import_history ORDER BY completed_at DESC, rowid DESC LIMIT -1 OFFSET ?
	)`, IMPORT_HISTORY_LIMIT)
	if err != nil {
		return fmt.Errorf("prune Import History: %w", err)
	}
	return nil
}

func insertHistoryFile(ctx context.Context, transaction *sql.Tx, importID string, position int, file HistoryFile) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO managed_import_history_files (
			import_id, file_id, job_id, safe_filename, started_at, completed_at, content_sha256,
			result_code, created_track_id, replaced_track_id, position
		) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?)`,
		importID, file.FileID, file.JobID, file.SafeFilename, file.StartedAt, file.CompletedAt,
		file.ContentSHA256, file.ResultCode, file.CreatedTrackID, file.ReplacedTrackID, position)
	if err != nil {
		return fmt.Errorf("store Import History file %q: %w", file.FileID, err)
	}
	return nil
}

func (store *Store) AlbumRequiresDiscNumber(ctx context.Context, metadata library.NormalizedMediaMetadata, excludedTrackID string) (bool, error) {
	var requiresDiscNumber bool
	err := store.database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM albums
			JOIN tracks ON tracks.album_id = albums.id
			WHERE albums.identity_key = ? AND tracks.missing_at IS NULL AND tracks.id != ?
				AND (tracks.disc_no > 1 OR tracks.disc_total > 1)
		)`, albumIdentityKey(metadata), excludedTrackID,
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

func (store *Store) AlbumPositionTotalConflict(ctx context.Context, metadata library.NormalizedMediaMetadata, excludedTrackID string) (string, error) {
	var hasDiscConflict, hasTrackConflict bool
	err := store.database.QueryRowContext(ctx, `
		WITH matching_tracks AS (
			SELECT tracks.disc_no, tracks.disc_total, tracks.track_no, tracks.track_total
			FROM albums JOIN tracks ON tracks.album_id = albums.id
			WHERE albums.identity_key = ? AND tracks.missing_at IS NULL AND tracks.id != ?
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
		excludedTrackID,
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

func (store *Store) ClassifyDuplicate(ctx context.Context, inspection library.MediaInspection) (DuplicateClassification, []DuplicateCandidate, error) {
	return store.ClassifyDuplicateExcluding(ctx, inspection, "")
}

// ClassifyDuplicateExcluding ignores one Track for Possible Duplicate matching while still treating any
// exact byte match, including that Track's own bytes, as an Exact Duplicate.
func (store *Store) ClassifyDuplicateExcluding(ctx context.Context, inspection library.MediaInspection, excludedTrackID string) (DuplicateClassification, []DuplicateCandidate, error) {
	trackID, err := store.FindExactDuplicateTrackID(ctx, inspection.FileSHA256)
	if err != nil {
		return "", nil, err
	}
	if trackID != "" {
		candidate, readErr := store.readDuplicateCandidate(ctx, trackID)
		if readErr != nil {
			return "", nil, readErr
		}
		return DUPLICATE_EXACT, []DuplicateCandidate{candidate}, nil
	}
	trackIDs, err := store.findPossibleDuplicateTrackIDs(ctx, inspection.Metadata, excludedTrackID)
	if err != nil {
		return "", nil, err
	}
	if len(trackIDs) == 0 {
		return DUPLICATE_NONE, nil, nil
	}
	candidates := make([]DuplicateCandidate, 0, len(trackIDs))
	for _, candidateTrackID := range trackIDs {
		candidate, readErr := store.readDuplicateCandidate(ctx, candidateTrackID)
		if readErr != nil {
			return "", nil, readErr
		}
		candidates = append(candidates, candidate)
	}
	return DUPLICATE_POSSIBLE, candidates, nil
}

func (store *Store) findPossibleDuplicateTrackIDs(ctx context.Context, metadata library.NormalizedMediaMetadata, excludedTrackID string) ([]string, error) {
	positionIDs, err := store.findDuplicatesByPosition(ctx, metadata, excludedTrackID)
	if err != nil {
		return nil, err
	}
	identityIDs, err := store.findDuplicatesByIdentity(ctx, metadata, excludedTrackID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(positionIDs)+len(identityIDs))
	trackIDs := make([]string, 0, len(positionIDs)+len(identityIDs))
	for _, trackID := range append(positionIDs, identityIDs...) {
		if !seen[trackID] {
			seen[trackID] = true
			trackIDs = append(trackIDs, trackID)
		}
	}
	return trackIDs, nil
}

func (store *Store) findDuplicatesByPosition(ctx context.Context, metadata library.NormalizedMediaMetadata, excludedTrackID string) ([]string, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT tracks.id
		FROM tracks
		JOIN albums ON albums.id = tracks.album_id
		WHERE tracks.missing_at IS NULL AND tracks.id != ?
			AND albums.identity_key = ?
			AND COALESCE(tracks.disc_no, 1) = ? AND tracks.track_no = ?
		ORDER BY tracks.id`, excludedTrackID, albumIdentityKey(metadata), metadata.DiscPosition.Number,
		metadata.TrackPosition.Number)
	if err != nil {
		return nil, fmt.Errorf("inspect possible Managed Import position duplicates: %w", err)
	}
	return scanTrackIDs(rows, "position duplicates")
}

func (store *Store) findDuplicatesByIdentity(ctx context.Context, metadata library.NormalizedMediaMetadata, excludedTrackID string) ([]string, error) {
	normalizedArtists := make([]string, len(metadata.Artists))
	for index, artist := range metadata.Artists {
		normalizedArtists[index] = normalizeIdentity(artist)
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT tracks.id
		FROM tracks
		JOIN albums ON albums.id = tracks.album_id
		WHERE tracks.missing_at IS NULL AND tracks.id != ?
			AND albums.identity_key = ?
			AND tracks.title_sort = ?
			AND COALESCE((SELECT GROUP_CONCAT(name_normalized, char(30)) FROM (
				SELECT artists.name_normalized
				FROM track_artists JOIN artists ON artists.id = track_artists.artist_id
				WHERE track_artists.track_id = tracks.id ORDER BY track_artists.position
			)), '') = ?
		ORDER BY tracks.id`, excludedTrackID, albumIdentityKey(metadata), normalizeIdentity(metadata.Title),
		strings.Join(normalizedArtists, "\x1e"))
	if err != nil {
		return nil, fmt.Errorf("inspect possible Managed Import identity duplicates: %w", err)
	}
	return scanTrackIDs(rows, "identity duplicates")
}

func scanTrackIDs(rows *sql.Rows, description string) (trackIDs []string, returnErr error) {
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Managed Import %s: %w", description, err))
		}
	}()
	for rows.Next() {
		var trackID string
		if err := rows.Scan(&trackID); err != nil {
			return nil, fmt.Errorf("scan Managed Import %s: %w", description, err)
		}
		trackIDs = append(trackIDs, trackID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Managed Import %s: %w", description, err)
	}
	return trackIDs, nil
}

func (store *Store) readDuplicateCandidate(ctx context.Context, trackID string) (candidate DuplicateCandidate, returnErr error) {
	err := store.database.QueryRowContext(ctx, `
		SELECT tracks.id, tracks.title, albums.title, COALESCE(tracks.disc_no, 1),
			tracks.track_no, tracks.format, tracks.duration_ms
		FROM tracks JOIN albums ON albums.id = tracks.album_id
		WHERE tracks.id = ?`, trackID,
	).Scan(&candidate.TrackID, &candidate.Title, &candidate.Album, &candidate.DiscNo,
		&candidate.TrackNo, &candidate.Format, &candidate.DurationMs)
	if err != nil {
		return DuplicateCandidate{}, fmt.Errorf("read Managed Import duplicate candidate: %w", err)
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT artists.name FROM track_artists
		JOIN artists ON artists.id = track_artists.artist_id
		WHERE track_artists.track_id = ? ORDER BY track_artists.position`, trackID)
	if err != nil {
		return DuplicateCandidate{}, fmt.Errorf("read duplicate candidate Artists: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rows.Close())
	}()
	for rows.Next() {
		var artist string
		if err := rows.Scan(&artist); err != nil {
			return DuplicateCandidate{}, fmt.Errorf("scan duplicate candidate Artist: %w", err)
		}
		candidate.Artists = append(candidate.Artists, artist)
	}
	if err := rows.Err(); err != nil {
		return DuplicateCandidate{}, fmt.Errorf("iterate duplicate candidate Artists: %w", err)
	}
	return candidate, nil
}

func (store *Store) ResolveCommitIdentity(ctx context.Context, metadata library.NormalizedMediaMetadata, albumKeys ...string) (commitIdentity, error) {
	albumKey := albumIdentityKey(metadata)
	if len(albumKeys) > 0 {
		albumKey = albumKeys[0]
	}
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
		WHERE albums.identity_key = ?`, albumKey,
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
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return Result{}, fmt.Errorf("begin Managed Import commit: %w", beginErr)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Managed Import transaction: %w", rollbackErr))
		}
	}()
	if err := writeCommitData(ctx, transaction, data); err != nil {
		return Result{}, err
	}
	result, commitErr := markCommitted(ctx, transaction, data.Job, data.Identity.TrackID)
	if commitErr != nil {
		return Result{}, commitErr
	}
	if err := archiveStandaloneHistory(ctx, transaction, data.Job.ID, HISTORY_RESULT_COMPLETED); err != nil {
		return Result{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit Managed Import database transaction: %w", err)
	}
	return result, nil
}

func writeCommitData(ctx context.Context, transaction *sql.Tx, data commitData) error {
	artistIDs, err := upsertArtists(ctx, transaction, data.Inspection.Metadata, data.Identity)
	if err != nil {
		return err
	}
	if err := upsertAlbum(ctx, transaction, data, artistIDs); err != nil {
		return err
	}
	if err := insertTrack(ctx, transaction, data); err != nil {
		return err
	}
	if err := insertRelationships(ctx, transaction, data, artistIDs); err != nil {
		return err
	}
	return insertArtwork(ctx, transaction, data)
}

func (store *Store) CreateCommitJournal(ctx context.Context, journal commitJournal) error {
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO managed_import_commit_journal (
			id, job_id, track_id, phase, staged_file_path, audio_file_path, artwork_file_path,
			audio_sha256, artwork_sha256, artwork_created
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		journal.ID, journal.JobID, journal.TrackID, journal.Phase, journal.StagedFilePath,
		journal.AudioFilePath, journal.ArtworkFilePath, journal.AudioSHA256,
		journal.ArtworkSHA256, journal.IsArtworkCreated,
	)
	if err != nil {
		return fmt.Errorf("create Managed Import commit journal: %w", err)
	}
	return nil
}

func (store *Store) MarkCommitPlaced(ctx context.Context, journalID string, isArtworkCreated bool) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_commit_journal
		SET phase = ?, artwork_created = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`,
		COMMIT_PHASE_PLACED, isArtworkCreated, journalID, COMMIT_PHASE_PREPARED)
	if err != nil {
		return fmt.Errorf("journal canonical Managed Import placement: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) MarkCommitArtworkCreated(ctx context.Context, journalID string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_commit_journal
		SET artwork_created = 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, journalID, COMMIT_PHASE_PREPARED)
	if err != nil {
		return fmt.Errorf("journal Canonical Album Artwork creation: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) UpdateCommitPhase(ctx context.Context, journalID string, phase commitPhase) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_commit_journal SET phase = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?)`,
		phase, journalID, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("update Managed Import commit journal to %q: %w", phase, err)
	}
	return requireMutation(result)
}

func (store *Store) RollbackCommitJournal(ctx context.Context, journalID, reason string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_commit_journal
		SET phase = ?, recovery_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?)`,
		COMMIT_PHASE_ROLLED_BACK, reason, journalID, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("roll back Managed Import commit journal: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) RecordCommitRecoveryReason(ctx context.Context, journalID, reason string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_commit_journal
		SET recovery_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?)`,
		reason, journalID, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("record Managed Import recovery reason: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) ListIncompleteCommitJournals(ctx context.Context) (journals []commitJournal, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, job_id, track_id, phase, staged_file_path, audio_file_path,
			artwork_file_path, audio_sha256, artwork_sha256, artwork_created,
			COALESCE(recovery_reason, '')
		FROM managed_import_commit_journal
		WHERE phase NOT IN (?, ?)
		ORDER BY created_at, id`, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if err != nil {
		return nil, fmt.Errorf("list incomplete Managed Import commits: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var journal commitJournal
		if err := rows.Scan(
			&journal.ID, &journal.JobID, &journal.TrackID, &journal.Phase,
			&journal.StagedFilePath, &journal.AudioFilePath, &journal.ArtworkFilePath,
			&journal.AudioSHA256, &journal.ArtworkSHA256, &journal.IsArtworkCreated,
			&journal.RecoveryReason,
		); err != nil {
			return nil, fmt.Errorf("read Managed Import commit journal: %w", err)
		}
		journals = append(journals, journal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Managed Import commit journals: %w", err)
	}
	return journals, nil
}

func (store *Store) GetCommitJournal(ctx context.Context, journalID string) (commitJournal, error) {
	return store.readCommitJournal(ctx, `WHERE id = ?`, journalID)
}

func (store *Store) FindIncompleteCommitJournal(ctx context.Context, jobID string) (commitJournal, bool, error) {
	journal, err := store.readCommitJournal(ctx, `WHERE job_id = ? AND phase NOT IN (?, ?)`, jobID, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if errors.Is(err, ErrNotFound) {
		return commitJournal{}, false, nil
	}
	return journal, err == nil, err
}

func (store *Store) readCommitJournal(ctx context.Context, where string, args ...any) (commitJournal, error) {
	var journal commitJournal
	err := store.database.QueryRowContext(ctx, `
		SELECT id, job_id, track_id, phase, staged_file_path, audio_file_path,
			artwork_file_path, audio_sha256, artwork_sha256, artwork_created,
			COALESCE(recovery_reason, '')
		FROM managed_import_commit_journal `+where, args...).Scan(
		&journal.ID, &journal.JobID, &journal.TrackID, &journal.Phase,
		&journal.StagedFilePath, &journal.AudioFilePath, &journal.ArtworkFilePath,
		&journal.AudioSHA256, &journal.ArtworkSHA256, &journal.IsArtworkCreated,
		&journal.RecoveryReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return commitJournal{}, ErrNotFound
	}
	if err != nil {
		return commitJournal{}, fmt.Errorf("read Managed Import commit journal: %w", err)
	}
	return journal, nil
}

func (store *Store) CommitPending(ctx context.Context, data commitData, journalID string) (returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return fmt.Errorf("begin pending Managed Import commit: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "pending Managed Import commit"))
	}()
	if err := writeCommitData(ctx, transaction, data); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE tracks SET is_pending_commit = 1 WHERE id = ?`, data.Identity.TrackID); err != nil {
		return fmt.Errorf("hide pending Managed Track: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_commit_journal SET phase = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, COMMIT_PHASE_DATABASE_COMMITTED, journalID, COMMIT_PHASE_VERIFIED)
	if err != nil {
		return fmt.Errorf("journal Managed Import database commit: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit pending Managed Import database transaction: %w", err)
	}
	return nil
}

func (store *Store) FinalizeCommit(ctx context.Context, job importJob, trackID, journalID string) (result Result, returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return Result{}, fmt.Errorf("begin Managed Import finalization: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "Managed Import finalization"))
	}()
	trackResult, publishErr := transaction.ExecContext(ctx, `UPDATE tracks SET is_pending_commit = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND is_pending_commit = 1`, trackID)
	if publishErr != nil {
		return Result{}, fmt.Errorf("publish committed Managed Track: %w", publishErr)
	}
	if err := requireMutation(trackResult); err != nil {
		return Result{}, err
	}
	result, commitErr := markCommitted(ctx, transaction, job, trackID)
	if commitErr != nil {
		return Result{}, commitErr
	}
	journalResult, journalErr := transaction.ExecContext(ctx, `
		UPDATE managed_import_commit_journal
		SET phase = ?, recovery_reason = COALESCE(recovery_reason, 'commit completed'), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, COMMIT_PHASE_COMPLETED, journalID, COMMIT_PHASE_CLEANED)
	if journalErr != nil {
		return Result{}, fmt.Errorf("complete Managed Import commit journal: %w", journalErr)
	}
	if err := requireMutation(journalResult); err != nil {
		return Result{}, err
	}
	if err := archiveStandaloneHistory(ctx, transaction, job.ID, HISTORY_RESULT_COMPLETED); err != nil {
		return Result{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit Managed Import finalization: %w", err)
	}
	return result, nil
}

func (store *Store) RollbackPendingCommit(ctx context.Context, journal commitJournal, reason string) (returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return fmt.Errorf("begin pending Managed Import rollback: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "pending Managed Import rollback"))
	}()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM album_artwork WHERE source_track_id = ?`, journal.TrackID); err != nil {
		return fmt.Errorf("delete pending Album Artwork: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM tracks WHERE id = ? AND is_pending_commit = 1`, journal.TrackID); err != nil {
		return fmt.Errorf("delete pending Managed Track: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_commit_journal SET phase = ?, recovery_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?)`,
		COMMIT_PHASE_VERIFIED, reason, journal.ID, COMMIT_PHASE_COMPLETED, COMMIT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("journal pending Managed Import rollback: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit pending Managed Import rollback: %w", err)
	}
	return nil
}

func rollbackTransaction(transaction *sql.Tx, operation string) error {
	err := transaction.Rollback()
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return fmt.Errorf("rollback %s transaction: %w", operation, err)
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
	err := transaction.QueryRowContext(ctx, `SELECT id FROM albums WHERE identity_key = ?`, data.AlbumKey).Scan(&existingID)
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
		data.Identity.AlbumID, primaryArtistID, metadata.Album, normalizeIdentity(metadata.Album), nullablePositive(metadata.Year), releaseDate(metadata.Year), string(genres), data.AlbumKey,
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
