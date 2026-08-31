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
	inspection, err := service.inspector.Inspect(upload.Path)
	if err != nil {
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, validationError(err))
	}
	if inspection.FileSHA256 != upload.SHA256 {
		hashErr := &ValidationError{Code: "staged_file_changed", Field: "file", Err: errors.New("staged file hash differs from uploaded bytes")}
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, hashErr)
	}
	if preflightErr := service.preflightCommit(upload.Size, inspection); preflightErr != nil {
		return Preview{}, service.failUpload(ctx, jobID, upload.Path, preflightErr)
	}
	job, err = service.store.MarkPreview(ctx, jobID, originalFilename, upload.Path, upload.SHA256)
	if err != nil {
		return Preview{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	return previewFromInspection(job, inspection), nil
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
	inspection, err := service.inspector.Inspect(job.StagedFilePath)
	if err != nil {
		return Result{}, validationError(err)
	}
	if inspection.FileSHA256 != job.ContentSHA256 {
		return Result{}, &ValidationError{Code: "staged_file_changed", Field: "file", Err: errors.New("staged file hash changed after Import Preview")}
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
	return errors.Join(uploadErr, service.storage.RemoveStaged(stagedPath), service.store.MarkFailed(ctx, jobID, errorCode))
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
	if !strings.EqualFold(filepath.Ext(value), ".flac") {
		return "", fmt.Errorf("%w: only FLAC files are accepted", ErrInvalidUpload)
	}
	return value, nil
}
