package managedimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	stagedPath, _, err := service.storage.StageUpload(jobID, body, contentLength)
	if err != nil {
		return Preview{}, err
	}
	inspection, err := service.inspector.Inspect(ctx, stagedPath, service.validationProgressReporter(ctx, jobID))
	if err != nil {
		return Preview{}, service.failUpload(ctx, jobID, stagedPath, validationError(err))
	}
	job, err = service.store.MarkPreview(ctx, jobID, originalFilename, stagedPath, inspection.FileSHA256)
	if err != nil {
		return service.recoverPreviewFailure(ctx, jobID, stagedPath, err, inspection)
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
		transitionErr = validationError(&library.InspectionError{Code: library.INSPECTION_ERROR_VALIDATION_CANCELLED, Field: "validation", Err: ctx.Err()})
	}
	return Preview{}, service.failUpload(ctx, jobID, stagedPath, errors.Join(transitionErr, getErr))
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
	inspection, err := service.inspector.Inspect(ctx, job.StagedFilePath, nil)
	if err != nil {
		return Result{}, validationError(err)
	}
	if inspection.FileSHA256 != job.ContentSHA256 {
		return Result{}, &ValidationError{Code: "staged_file_changed", Field: "file", Err: errors.New("staged file hash changed after Import Preview")}
	}
	return service.commit(ctx, job, inspection)
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
	return errors.Join(uploadErr, os.Remove(stagedPath), service.store.MarkFailed(cleanupCtx, jobID, errorCode))
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
	value = filepath.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if value == "." || value == "" || len(value) > MAX_ORIGINAL_FILENAME_BYTES {
		return "", fmt.Errorf("%w: filename is missing or too long", ErrInvalidUpload)
	}
	return value, nil
}
