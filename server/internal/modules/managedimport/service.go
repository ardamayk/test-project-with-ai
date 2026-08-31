package managedimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

type Service struct {
	store     *Store
	storage   *Storage
	inspector library.MediaInspector
}

func NewService(store *Store, storage *Storage, inspector library.MediaInspector) *Service {
	return &Service{store: store, storage: storage, inspector: inspector}
}

func (service *Service) CreateJob(ctx context.Context) (Job, error) {
	return service.store.CreateJob(ctx)
}

func (service *Service) GetJob(ctx context.Context, jobID string) (Job, error) {
	job, err := service.store.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	return job.Job, nil
}

func (service *Service) Upload(ctx context.Context, jobID, originalFilename string, body io.Reader, contentLength int64) (Preview, error) {
	job, err := service.store.GetJob(ctx, jobID)
	if err != nil {
		return Preview{}, err
	}
	if job.Status != STATUS_UPLOADING {
		return Preview{}, ErrInvalidState
	}
	originalFilename, err = safeOriginalFilename(originalFilename)
	if err != nil {
		return Preview{}, err
	}
	upload, err := service.storage.StageUpload(body, contentLength)
	if err != nil {
		return Preview{}, err
	}
	inspection, err := service.inspector.Inspect(ctx, upload.Path, service.validationProgressReporter(ctx, jobID))
	if err != nil {
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, validationError(err))
	}
	if inspection.FileSHA256 != upload.SHA256 {
		hashErr := &ValidationError{
			Code:   "staged_file_changed",
			Field:  "file",
			Reason: "staged file differs from uploaded bytes",
			Err:    errors.New("staged file hash differs from uploaded bytes"),
		}
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, hashErr)
	}
	if validationErr := service.validateAlbumPositions(ctx, jobID, inspection.Metadata); validationErr != nil {
		if ctx.Err() != nil {
			validationErr = validationCancellationError(ctx)
		}
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, validationErr)
	}
	if preflightErr := service.preflightCommit(upload.Size, inspection); preflightErr != nil {
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, preflightErr)
	}
	job, err = service.store.MarkPreview(ctx, jobID, originalFilename, upload.Path, upload.SHA256)
	if err != nil {
		return service.recoverPreviewFailure(ctx, jobID, upload.Path, err, inspection)
	}
	return previewFromInspection(job, inspection), nil
}

func (service *Service) recoverPreviewFailure(ctx context.Context, jobID, stagedPath string, transitionErr error, inspection library.MediaInspection) (Preview, error) {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	job, getErr := service.store.GetJob(recoveryCtx, jobID)
	if getErr == nil && job.Status == STATUS_AWAITING_CONFIRMATION {
		return previewFromInspection(job, inspection), nil
	}
	if ctx.Err() != nil {
		transitionErr = validationCancellationError(ctx)
	}
	return Preview{}, service.failUpload(ctx, jobID, stagedPath, errors.Join(transitionErr, getErr))
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
	if job.Status == STATUS_COMMITTED {
		return Result{JobID: job.ID, Status: job.Status, Revision: job.Revision, TrackID: job.TrackID}, nil
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
	if err := service.validateAlbumPositions(ctx, jobID, inspection.Metadata); err != nil {
		return Result{}, err
	}
	if inspection.FileSHA256 != job.ContentSHA256 {
		return Result{}, &ValidationError{
			Code:   "staged_file_changed",
			Field:  "file",
			Reason: "staged file changed after Import Preview",
			Err:    errors.New("staged file hash changed after Import Preview"),
		}
	}
	if err := service.preflightCommit(stagedBytes, inspection); err != nil {
		return Result{}, err
	}
	return service.commit(ctx, job, inspection)
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

func (service *Service) failUpload(ctx context.Context, jobID, stagedPath string, uploadErr error) error {
	errorCode := "inspection_failed"
	var validationErr *ValidationError
	if errors.As(uploadErr, &validationErr) {
		errorCode = validationErr.Code
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	return errors.Join(uploadErr, service.storage.RemoveStaged(stagedPath), service.store.MarkFailed(cleanupCtx, jobID, errorCode))
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
