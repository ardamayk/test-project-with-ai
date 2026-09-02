package managedimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

type Service struct {
	store               *Store
	storage             *Storage
	inspector           library.MediaInspector
	commitMu            sync.Mutex
	batchConfirmationMu sync.Mutex
	uploadLocksMu       sync.Mutex
	uploadLocks         map[string]*uploadLock
}

type uploadLock struct {
	mutex sync.Mutex
	users int
}

func NewService(store *Store, storage *Storage, inspector library.MediaInspector) *Service {
	return &Service{store: store, storage: storage, inspector: inspector, uploadLocks: make(map[string]*uploadLock)}
}

func (service *Service) CreateJob(ctx context.Context, batchID, clientFileID string) (Job, error) {
	if batchID != "" {
		if _, err := uuid.Parse(clientFileID); err != nil {
			return Job{}, fmt.Errorf("%w: clientFileId must be a UUID", ErrInvalidUpload)
		}
	}
	return service.store.CreateJob(ctx, batchID, clientFileID)
}

func (service *Service) CreateBatch(ctx context.Context) (Batch, error) {
	return service.store.CreateBatch(ctx)
}

func (service *Service) GetBatch(ctx context.Context, batchID string) (Batch, error) {
	return service.store.GetBatch(ctx, batchID)
}

func (service *Service) GetJob(ctx context.Context, jobID string) (Job, error) {
	job, err := service.store.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	return job.Job, nil
}

func (service *Service) Upload(ctx context.Context, jobID, originalFilename string, body io.Reader, contentLength int64) (Preview, error) {
	unlock := service.lockUpload(jobID)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	job, err := service.getUploadingJob(ctx, jobID)
	if err != nil {
		return Preview{}, err
	}
	originalFilename, err = safeOriginalFilename(originalFilename)
	if err != nil {
		return Preview{}, errors.Join(err, service.markRejected(ctx, job, originalFilename, err))
	}
	err = service.reserveBatchUpload(ctx, job, contentLength)
	if err != nil {
		return Preview{}, errors.Join(err, service.markRejected(ctx, job, originalFilename, err))
	}
	body = service.batchUploadReader(ctx, job, body, contentLength)
	upload, err := service.storage.StageUpload(body, contentLength)
	if err != nil {
		return Preview{}, errors.Join(err, service.markRejected(ctx, job, originalFilename, err))
	}
	if job.BatchID != "" {
		err = service.store.ReserveBatchUpload(ctx, job.ID, upload.Size, service.storage.batchLimit)
		if err != nil {
			return Preview{}, service.failUpload(ctx, job, originalFilename, upload.Path, err)
		}
	}
	inspection, err := service.validateStagedUpload(ctx, jobID, upload)
	if err != nil {
		return Preview{}, service.failUpload(ctx, job, originalFilename, upload.Path, err)
	}
	return service.persistPreview(ctx, job, originalFilename, upload, inspection)
}

type batchReservationReader struct {
	source  io.Reader
	reserve func(int64) error
	read    int64
}

func (reader *batchReservationReader) Read(buffer []byte) (int, error) {
	read, readErr := reader.source.Read(buffer)
	reader.read += int64(read)
	if read > 0 {
		if reserveErr := reader.reserve(reader.read); reserveErr != nil {
			return read, reserveErr
		}
	}
	return read, readErr
}

func (service *Service) batchUploadReader(ctx context.Context, job importJob, body io.Reader, contentLength int64) io.Reader {
	if job.BatchID == "" || contentLength >= 0 {
		return body
	}
	return &batchReservationReader{
		source: body,
		reserve: func(uploadSize int64) error {
			return service.store.ReserveBatchUpload(ctx, job.ID, uploadSize, service.storage.batchLimit)
		},
	}
}

func (service *Service) lockUpload(jobID string) func() {
	service.uploadLocksMu.Lock()
	lock := service.uploadLocks[jobID]
	if lock == nil {
		lock = &uploadLock{}
		service.uploadLocks[jobID] = lock
	}
	lock.users++
	service.uploadLocksMu.Unlock()
	lock.mutex.Lock()
	return func() {
		lock.mutex.Unlock()
		service.uploadLocksMu.Lock()
		lock.users--
		if lock.users == 0 {
			delete(service.uploadLocks, jobID)
		}
		service.uploadLocksMu.Unlock()
	}
}

func (service *Service) reserveBatchUpload(ctx context.Context, job importJob, contentLength int64) error {
	if job.BatchID == "" {
		return nil
	}
	reservationSize, err := service.storage.UploadReservationSize(contentLength)
	if err != nil {
		return err
	}
	return service.store.ReserveBatchUpload(ctx, job.ID, reservationSize, service.storage.batchLimit)
}

func (service *Service) getUploadingJob(ctx context.Context, jobID string) (importJob, error) {
	job, err := service.store.GetJob(ctx, jobID)
	if err != nil || job.Status != STATUS_UPLOADING {
		if err == nil {
			err = ErrInvalidState
		}
		return importJob{}, err
	}
	if job.BatchID == "" {
		return job, nil
	}
	batchStatus, err := service.store.GetBatchStatus(ctx, job.BatchID)
	if err != nil {
		return importJob{}, err
	}
	if batchStatus != BATCH_STATUS_UPLOADING {
		return importJob{}, ErrInvalidState
	}
	return job, nil
}

func (service *Service) validateStagedUpload(ctx context.Context, jobID string, upload stagedUpload) (library.MediaInspection, error) {
	inspection, err := service.inspector.Inspect(ctx, upload.Path, service.validationProgressReporter(ctx, jobID))
	if err != nil {
		return library.MediaInspection{}, validationError(err)
	}
	if inspection.FileSHA256 != upload.SHA256 {
		return library.MediaInspection{}, &ValidationError{Code: "staged_file_changed", Field: "file", Reason: "staged file differs from uploaded bytes", Err: errors.New("staged file hash differs from uploaded bytes")}
	}
	if err := service.validateAlbumPositions(ctx, jobID, inspection.Metadata); err != nil {
		if ctx.Err() != nil {
			err = validationCancellationError(ctx)
		}
		return library.MediaInspection{}, err
	}
	if err := service.preflightCommit(upload.Size, inspection); err != nil {
		return library.MediaInspection{}, err
	}
	return inspection, nil
}

func (service *Service) persistPreview(ctx context.Context, job importJob, originalFilename string, upload stagedUpload, inspection library.MediaInspection) (Preview, error) {
	previewJob := job
	previewJob.Status = STATUS_AWAITING_CONFIRMATION
	previewJob.Revision++
	previewJob.OriginalFilename = originalFilename
	preview := previewFromInspection(previewJob, inspection)
	previewBytes, err := json.Marshal(preview)
	if err != nil {
		return Preview{}, service.failUpload(ctx, job, originalFilename, upload.Path, fmt.Errorf("encode Import Preview: %w", err))
	}
	markedJob, err := service.store.MarkPreview(ctx, job.ID, originalFilename, upload.Path, upload.SHA256, string(previewBytes), upload.Size, service.storage.batchLimit)
	if err != nil {
		return service.recoverPreviewFailure(ctx, job, originalFilename, upload.Path, err, inspection)
	}
	return previewFromInspection(markedJob, inspection), nil
}

func (service *Service) recoverPreviewFailure(ctx context.Context, originalJob importJob, originalFilename, stagedPath string, transitionErr error, inspection library.MediaInspection) (Preview, error) {
	if errors.Is(transitionErr, ErrBatchTooLarge) {
		return Preview{}, service.failUpload(ctx, originalJob, originalFilename, stagedPath, transitionErr)
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	job, getErr := service.store.GetJob(recoveryCtx, originalJob.ID)
	if getErr == nil && job.Status == STATUS_AWAITING_CONFIRMATION {
		return previewFromInspection(job, inspection), nil
	}
	if ctx.Err() != nil {
		transitionErr = validationCancellationError(ctx)
	}
	return Preview{}, service.failUpload(ctx, originalJob, originalFilename, stagedPath, errors.Join(transitionErr, getErr))
}

func validationCancellationError(ctx context.Context) error {
	return validationError(&library.InspectionError{Code: library.INSPECTION_ERROR_VALIDATION_CANCELLED, Field: "validation", Err: ctx.Err()})
}

func (service *Service) validateAlbumPositions(ctx context.Context, jobID string, metadata library.NormalizedMediaMetadata) error {
	if metadata.HasDiscNumber {
		return service.validateExistingAlbumTotals(ctx, metadata)
	}
	requiresDiscNumber, err := service.requiresDiscNumber(ctx, jobID, metadata)
	if err != nil {
		return err
	}
	if !requiresDiscNumber {
		return service.validateExistingAlbumTotals(ctx, metadata)
	}
	return &ValidationError{
		Code:   string(library.INSPECTION_ERROR_INVALID_METADATA),
		Field:  "DISCNUMBER",
		Reason: "DISCNUMBER is required for a known multi-disc Album",
		Err:    errors.New("DISCNUMBER is required for a known multi-disc Album"),
	}
}

func (service *Service) requiresDiscNumber(ctx context.Context, jobID string, metadata library.NormalizedMediaMetadata) (bool, error) {
	requiresDiscNumber, err := service.store.AlbumRequiresDiscNumber(ctx, metadata)
	if err != nil || requiresDiscNumber {
		return requiresDiscNumber, err
	}
	return service.awaitingSiblingRequiresDiscNumber(ctx, jobID, metadata)
}

func (service *Service) awaitingSiblingRequiresDiscNumber(ctx context.Context, jobID string, metadata library.NormalizedMediaMetadata) (bool, error) {
	paths, err := service.store.AwaitingPreviewPaths(ctx, jobID)
	if err != nil {
		return false, err
	}
	albumKey := albumIdentityKey(metadata)
	for _, path := range paths {
		inspection, inspectionErr := service.inspector.Inspect(ctx, path, nil)
		if inspectionErr != nil {
			return false, fmt.Errorf("inspect awaiting sibling file: %w", inspectionErr)
		}
		sibling := inspection.Metadata
		if albumIdentityKey(sibling) == albumKey && isMultiDisc(sibling) {
			return true, nil
		}
	}
	return false, nil
}

func isMultiDisc(metadata library.NormalizedMediaMetadata) bool {
	return metadata.DiscPosition.Number > 1 || metadata.DiscPosition.Total > 1
}

func (service *Service) validateExistingAlbumTotals(ctx context.Context, metadata library.NormalizedMediaMetadata) error {
	field, err := service.store.AlbumPositionTotalConflict(ctx, metadata)
	if err != nil {
		return err
	}
	if field == "" {
		return nil
	}
	return &ValidationError{
		Code:   string(library.INSPECTION_ERROR_INVALID_METADATA),
		Field:  field,
		Reason: "position total conflicts with the existing Album",
		Err:    errors.New("position total conflicts with the existing Album"),
	}
}

func (service *Service) Confirm(ctx context.Context, jobID string, revision int) (Result, error) {
	job, err := service.store.GetJob(ctx, jobID)
	if err != nil {
		return Result{}, err
	}
	if job.BatchID != "" {
		return Result{}, ErrInvalidState
	}
	return service.confirmJob(ctx, job, revision)
}

func (service *Service) confirmJob(ctx context.Context, job importJob, revision int) (Result, error) {
	service.commitMu.Lock()
	defer service.commitMu.Unlock()
	job, err := service.store.GetJob(ctx, job.ID)
	if err != nil {
		return Result{}, err
	}
	if job.Status == STATUS_COMMITTED {
		return Result{JobID: job.ID, Status: job.Status, Revision: job.Revision, TrackID: job.TrackID}, nil
	}
	if job.Status == STATUS_FAILED && job.ErrorCode == ERROR_CODE_EXACT_DUPLICATE {
		return Result{}, ErrExactDuplicate
	}
	if job.Status != STATUS_AWAITING_CONFIRMATION {
		return Result{}, ErrInvalidState
	}
	if revision != job.Revision {
		return Result{}, ErrRevisionConflict
	}
	stagedBytes, err := service.storage.StagedFileSize(job.StagedFilePath)
	if err != nil {
		return Result{}, err
	}
	inspection, err := service.inspector.Inspect(ctx, job.StagedFilePath, nil)
	if err != nil {
		return Result{}, validationError(err)
	}
	if positionErr := service.validateAlbumPositions(ctx, job.ID, inspection.Metadata); positionErr != nil {
		return Result{}, positionErr
	}
	if inspection.FileSHA256 != job.ContentSHA256 {
		return Result{}, &ValidationError{
			Code:   "staged_file_changed",
			Field:  "file",
			Reason: "staged file changed after Import Preview",
			Err:    errors.New("staged file hash changed after Import Preview"),
		}
	}
	existingTrackID, err := service.store.FindExactDuplicateTrackID(ctx, inspection.FileSHA256)
	if err != nil {
		return Result{}, err
	}
	if existingTrackID != "" {
		return Result{}, service.rejectExactDuplicate(ctx, job)
	}
	if preflightErr := service.preflightCommit(stagedBytes, inspection); preflightErr != nil {
		return Result{}, preflightErr
	}
	return service.commit(ctx, job, inspection)
}

func (service *Service) rejectExactDuplicate(ctx context.Context, job importJob) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	cleanupErr := service.storage.RemoveStaged(job.StagedFilePath)
	if cleanupErr == nil {
		cleanupErr = service.store.MarkFailed(cleanupCtx, job.ID, job.OriginalFilename, ERROR_CODE_EXACT_DUPLICATE, "file", "file bytes match an existing Track")
	}
	return errors.Join(ErrExactDuplicate, cleanupErr)
}

func (service *Service) ConfirmBatch(ctx context.Context, batchID string, confirmation BatchConfirmation) (Batch, error) {
	service.batchConfirmationMu.Lock()
	defer service.batchConfirmationMu.Unlock()
	selectedIDs, err := selectedFileIDs(confirmation.SelectedFileIDs)
	if err != nil {
		return Batch{}, err
	}
	batch, err := service.store.GetBatch(ctx, batchID)
	if err != nil {
		return Batch{}, err
	}
	if batch.Status == BATCH_STATUS_COMPLETED {
		return batch, nil
	}
	err = service.startOrResumeBatch(ctx, batch, confirmation.Revision, selectedIDs)
	if err != nil {
		return Batch{}, err
	}
	jobs, err := service.store.ListBatchJobs(ctx, batchID)
	if err != nil {
		return Batch{}, err
	}
	if err := service.confirmBatchJobs(ctx, jobs); err != nil {
		return Batch{}, err
	}
	return service.completeBatch(ctx, batchID)
}

func selectedFileIDs(fileIDs []string) (map[string]bool, error) {
	selectedIDs := make(map[string]bool, len(fileIDs))
	for _, jobID := range fileIDs {
		if jobID == "" || selectedIDs[jobID] {
			return nil, fmt.Errorf("%w: selected file IDs must be unique and non-empty", ErrInvalidUpload)
		}
		selectedIDs[jobID] = true
	}
	return selectedIDs, nil
}

func (service *Service) startOrResumeBatch(ctx context.Context, batch Batch, revision int, selectedIDs map[string]bool) error {
	if batch.Status == BATCH_STATUS_CONFIRMING {
		return nil
	}
	if batch.Status != BATCH_STATUS_UPLOADING {
		return ErrInvalidState
	}
	return service.store.StartBatchConfirmation(ctx, batch.ID, revision, selectedIDs)
}

func (service *Service) confirmBatchJobs(ctx context.Context, jobs []importJob) error {
	for _, job := range jobs {
		if job.Outcome != "" {
			continue
		}
		if !job.Selected {
			if err := service.finishUncommittedBatchFile(ctx, job, OUTCOME_NOT_ATTEMPTED, "", ""); err != nil {
				return err
			}
			continue
		}
		if _, err := service.confirmJob(ctx, job, job.Revision); err != nil {
			if errors.Is(err, ErrExactDuplicate) {
				persistedJob, getErr := service.store.GetJob(ctx, job.ID)
				if getErr != nil {
					return errors.Join(err, getErr)
				}
				if persistedJob.Outcome == OUTCOME_REJECTED && persistedJob.ErrorCode == ERROR_CODE_EXACT_DUPLICATE {
					continue
				}
				return err
			}
			if ctx.Err() != nil {
				return errors.Join(err, ctx.Err())
			}
			errorCode, reason := failureDetails(err)
			if finishErr := service.finishUncommittedBatchFile(ctx, job, OUTCOME_FAILED, errorCode, reason); finishErr != nil {
				return errors.Join(err, finishErr)
			}
		}
	}
	return nil
}

func (service *Service) completeBatch(ctx context.Context, batchID string) (Batch, error) {
	if err := service.store.CompleteBatch(ctx, batchID); err != nil {
		batch, getErr := service.store.GetBatch(ctx, batchID)
		if getErr == nil && batch.Status == BATCH_STATUS_COMPLETED {
			return batch, nil
		}
		return Batch{}, errors.Join(err, getErr)
	}
	return service.store.GetBatch(ctx, batchID)
}

func (service *Service) finishUncommittedBatchFile(ctx context.Context, job importJob, outcome ImportOutcome, errorCode, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	if job.StagedFilePath != "" {
		if err := service.storage.RemoveStaged(job.StagedFilePath); err != nil {
			return fmt.Errorf("remove uncommitted Managed Import staging file: %w", err)
		}
	}
	if outcome == OUTCOME_FAILED && errorCode == "" {
		errorCode = "commit_failed"
	}
	return service.store.MarkBatchFileOutcome(cleanupCtx, job.ID, outcome, errorCode, reason)
}

func (service *Service) preflightCommit(stagedBytes int64, inspection library.MediaInspection) error {
	return service.storage.Preflight(StorageRequirement{
		SelectedBytes:  stagedBytes,
		TemporaryBytes: int64(len(inspection.AlbumArtwork.Data)),
	})
}

func (service *Service) commit(ctx context.Context, job importJob, inspection library.MediaInspection) (Result, error) {
	identity, err := service.store.ResolveCommitIdentity(ctx, inspection.Metadata)
	if err != nil {
		return Result{}, err
	}
	placement, err := service.storage.Place(job.StagedFilePath, inspection, identity)
	if err != nil {
		return Result{}, err
	}
	result, err := service.store.Commit(ctx, commitData{
		Job:        job,
		Identity:   identity,
		Placement:  placement,
		Inspection: inspection,
	})
	if err == nil {
		return result, nil
	}
	return Result{}, errors.Join(err, service.storage.Rollback(placement))
}

func (service *Service) failUpload(ctx context.Context, job importJob, originalFilename, stagedPath string, uploadErr error) error {
	errorCode, reason := failureDetails(uploadErr)
	errorField := ""
	var validationErr *ValidationError
	if errors.As(uploadErr, &validationErr) {
		errorField = validationErr.Field
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	return errors.Join(uploadErr, service.storage.RemoveStaged(stagedPath), service.store.MarkFailed(cleanupCtx, job.ID, originalFilename, errorCode, errorField, reason))
}

func (service *Service) markRejected(ctx context.Context, job importJob, originalFilename string, uploadErr error) error {
	if job.BatchID == "" {
		return nil
	}
	errorCode, reason := failureDetails(uploadErr)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	return service.store.MarkFailed(cleanupCtx, job.ID, originalFilename, errorCode, "file", reason)
}

func failureDetails(err error) (string, string) {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code, validationErr.Reason
	}
	switch {
	case errors.Is(err, ErrUploadTooLarge):
		return "upload_too_large", "file exceeds the configured per-file byte limit"
	case errors.Is(err, ErrBatchTooLarge):
		return "batch_upload_too_large", "batch exceeds the configured byte limit"
	case errors.Is(err, ErrInvalidUpload):
		return "invalid_upload", "upload is invalid"
	case errors.Is(err, ErrInsufficientStorage):
		return "insufficient_storage", "Managed Storage does not have enough capacity for this import and its safety reserve"
	case errors.Is(err, ErrUnsafeStoragePath):
		return "unsafe_storage_path", "Managed Storage path failed containment checks"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return string(library.INSPECTION_ERROR_VALIDATION_CANCELLED), "file validation was canceled"
	default:
		return "inspection_failed", "file validation failed"
	}
}

func (service *Service) validationProgressReporter(ctx context.Context, jobID string) library.InspectionProgressReporter {
	lastProgress := 0
	return func(progress library.InspectionProgress) error {
		if progress.Percent <= lastProgress {
			return nil
		}
		if err := service.store.UpdateValidationProgress(ctx, jobID, progress.Percent); err != nil {
			return err
		}
		lastProgress = progress.Percent
		return nil
	}
}

func safeOriginalFilename(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%w: filename contains forbidden characters", ErrInvalidUpload)
	}
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "/\\") || filepath.Base(value) != value {
		return "", fmt.Errorf("%w: filename must not contain path segments", ErrInvalidUpload)
	}
	if value == "." || value == "" || len(value) > MAX_ORIGINAL_FILENAME_BYTES {
		return "", fmt.Errorf("%w: filename is missing or too long", ErrInvalidUpload)
	}
	return value, nil
}
