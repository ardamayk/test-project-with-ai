package managedimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

const MIGRATION_CAPACITY_REASON = "Managed Storage does not have enough capacity for this migration and its safety reserve"

func (service *Service) PreviewMigration(ctx context.Context) (MigrationPreview, error) {
	sources, err := service.store.ListLegacyMigrationSources(ctx)
	if err != nil {
		return MigrationPreview{}, err
	}
	preview := MigrationPreview{Files: make([]MigrationPreviewFile, 0, len(sources))}
	var selectedBytes int64
	var temporaryBytes int64
	for _, source := range sources {
		file, inspection, inspectErr := service.inspectLegacyMigrationSource(ctx, source)
		if inspectErr != nil {
			preview.Files = append(preview.Files, rejectedMigrationFile(source, inspectErr))
			continue
		}
		selectedBytes, err = addByteCounts(selectedBytes, file.Size())
		if err != nil {
			return MigrationPreview{}, fmt.Errorf("calculate Library Migration selected capacity: %w", err)
		}
		temporaryBytes, err = addByteCounts(temporaryBytes, file.Size(), int64(len(inspection.AlbumArtwork.Data)))
		if err != nil {
			return MigrationPreview{}, fmt.Errorf("calculate Library Migration temporary capacity: %w", err)
		}
		preview.Files = append(preview.Files, acceptedMigrationFile(source, inspection))
	}
	if selectedBytes > 0 {
		if err := service.storage.Preflight(StorageRequirement{SelectedBytes: selectedBytes, TemporaryBytes: temporaryBytes}); err != nil {
			if !errors.Is(err, ErrInsufficientStorage) && !errors.Is(err, ErrUnsafeStoragePath) {
				return MigrationPreview{}, err
			}
			rejectMigrationCapacity(&preview, err)
		}
	}
	countMigrationResults(&preview)
	return preview, nil
}

func (service *Service) inspectLegacyMigrationSource(ctx context.Context, source legacyMigrationSource) (os.FileInfo, library.MediaInspection, error) {
	file, err := os.Stat(source.FilePath)
	if err != nil {
		return nil, library.MediaInspection{}, &library.InspectionError{
			Code:   library.INSPECTION_ERROR_FILE_READ,
			Field:  "file",
			Reason: "file could not be read",
			Err:    err,
		}
	}
	if !file.Mode().IsRegular() {
		return nil, library.MediaInspection{}, &library.InspectionError{
			Code:   library.INSPECTION_ERROR_FILE_READ,
			Field:  "file",
			Reason: "file is not a regular audio file",
			Err:    errors.New("legacy Track source is not a regular file"),
		}
	}
	inspection, err := service.inspector.Inspect(ctx, source.FilePath, nil)
	return file, inspection, err
}

func acceptedMigrationFile(source legacyMigrationSource, inspection library.MediaInspection) MigrationPreviewFile {
	filePreview := previewFileFromInspection(filepath.Base(source.FilePath), inspection)
	return MigrationPreviewFile{
		TrackID:          source.TrackID,
		OriginalFilename: filepath.Base(source.FilePath),
		State:            MIGRATION_FILE_ACCEPTED,
		Preview:          &filePreview,
	}
}

func rejectedMigrationFile(source legacyMigrationSource, err error) MigrationPreviewFile {
	validationErr := validationError(err)
	code, reason := failureDetails(validationErr)
	var field string
	var structuredErr *ValidationError
	if errors.As(validationErr, &structuredErr) {
		field = structuredErr.Field
	}
	return MigrationPreviewFile{
		TrackID:          source.TrackID,
		OriginalFilename: filepath.Base(source.FilePath),
		State:            MIGRATION_FILE_REJECTED,
		ErrorCode:        code,
		ErrorField:       field,
		ErrorReason:      reason,
	}
}

func rejectMigrationCapacity(preview *MigrationPreview, capacityErr error) {
	code, reason := failureDetails(capacityErr)
	if errors.Is(capacityErr, ErrInsufficientStorage) {
		reason = MIGRATION_CAPACITY_REASON
	}
	for index := range preview.Files {
		if preview.Files[index].State != MIGRATION_FILE_ACCEPTED {
			continue
		}
		preview.Files[index].State = MIGRATION_FILE_REJECTED
		preview.Files[index].Preview = nil
		preview.Files[index].ErrorCode = code
		preview.Files[index].ErrorField = "capacity"
		preview.Files[index].ErrorReason = reason
	}
}

func countMigrationResults(preview *MigrationPreview) {
	for _, file := range preview.Files {
		if file.State == MIGRATION_FILE_ACCEPTED {
			preview.AcceptedCount++
		} else {
			preview.RejectedCount++
		}
	}
}
