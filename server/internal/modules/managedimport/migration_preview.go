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

type migrationCandidate struct {
	previewIndex int
	fileSize     int64
	inspection   library.MediaInspection
}

type migrationAlbumRules struct {
	isMultiDisc          bool
	discTotal            int
	hasDiscTotalConflict bool
	trackTotals          map[int]int
	trackTotalConflicts  map[int]bool
	artworkSHA256        string
	hasArtworkConflict   bool
}

func (service *Service) PreviewMigration(ctx context.Context) (MigrationPreview, error) {
	if !service.migrationPreviewMu.TryLock() {
		return MigrationPreview{}, ErrMigrationInProgress
	}
	defer service.migrationPreviewMu.Unlock()
	if err := ctx.Err(); err != nil {
		return MigrationPreview{}, err
	}
	sources, err := service.store.ListLegacyMigrationSources(ctx)
	if err != nil {
		return MigrationPreview{}, err
	}
	preview, candidates, err := service.inspectMigrationSources(ctx, sources)
	if err != nil {
		return MigrationPreview{}, err
	}
	validateMigrationSet(&preview, candidates)
	requirement, err := migrationStorageRequirement(preview, candidates)
	if err != nil {
		return MigrationPreview{}, err
	}
	if requirement.SelectedBytes > service.storage.batchLimit {
		rejectAcceptedMigrationFiles(&preview, ErrBatchTooLarge)
	} else if requirement.SelectedBytes > 0 {
		capacityErr := service.storage.Preflight(requirement)
		if errors.Is(capacityErr, ErrInsufficientStorage) || errors.Is(capacityErr, ErrUnsafeStoragePath) {
			rejectAcceptedMigrationFiles(&preview, capacityErr)
		} else if capacityErr != nil {
			return MigrationPreview{}, capacityErr
		}
	}
	countMigrationResults(&preview)
	return preview, nil
}

func (service *Service) inspectMigrationSources(ctx context.Context, sources []legacyMigrationSource) (MigrationPreview, []migrationCandidate, error) {
	preview := MigrationPreview{Files: make([]MigrationPreviewFile, 0, len(sources))}
	candidates := make([]migrationCandidate, 0, len(sources))
	for _, source := range sources {
		file, inspection, inspectErr := service.inspectLegacyMigrationSource(ctx, source)
		if inspectErr != nil {
			if !isMigrationFileRejection(ctx, inspectErr) {
				return MigrationPreview{}, nil, inspectErr
			}
			preview.Files = append(preview.Files, rejectedMigrationFile(source, inspectErr))
			continue
		}
		if validationErr := service.validateMigrationInspection(ctx, inspection); validationErr != nil {
			if !isMigrationFileRejection(ctx, validationErr) {
				return MigrationPreview{}, nil, validationErr
			}
			preview.Files = append(preview.Files, rejectedMigrationFile(source, validationErr))
			continue
		}
		preview.Files = append(preview.Files, acceptedMigrationFile(source, inspection))
		candidates = append(candidates, migrationCandidate{previewIndex: len(preview.Files) - 1, fileSize: file.Size(), inspection: inspection})
	}
	return preview, candidates, nil
}

func (service *Service) validateMigrationInspection(ctx context.Context, inspection library.MediaInspection) error {
	if err := service.validateMigrationDiscNumber(ctx, inspection.Metadata); err != nil {
		return err
	}
	if err := service.validateExistingAlbumTotals(ctx, inspection.Metadata); err != nil {
		return err
	}
	return service.validateMigrationHash(ctx, inspection.FileSHA256)
}

func (service *Service) validateMigrationDiscNumber(ctx context.Context, metadata library.NormalizedMediaMetadata) error {
	if metadata.HasDiscNumber {
		return nil
	}
	requiresDiscNumber, err := service.store.AlbumRequiresDiscNumber(ctx, metadata)
	if err != nil || !requiresDiscNumber {
		return err
	}
	return migrationAlbumValidationError("DISCNUMBER", "DISCNUMBER is required for a known multi-disc Album")
}

func (service *Service) validateMigrationHash(ctx context.Context, contentSHA256 string) error {
	existingTrackID, err := service.store.FindManagedTrackByHash(ctx, contentSHA256)
	if err != nil {
		return err
	}
	if existingTrackID != "" {
		return &ValidationError{
			Code:   "exact_duplicate",
			Field:  "file",
			Reason: "file bytes already belong to an existing Managed Track",
			Err:    errors.New("migration source duplicates an existing Managed Track"),
		}
	}
	return nil
}

func (service *Service) inspectLegacyMigrationSource(ctx context.Context, source legacyMigrationSource) (os.FileInfo, library.MediaInspection, error) {
	file, err := os.Stat(source.FilePath)
	if err != nil {
		return nil, library.MediaInspection{}, fileReadInspectionError(err)
	}
	if !file.Mode().IsRegular() {
		return nil, library.MediaInspection{}, &library.InspectionError{
			Code:   library.INSPECTION_ERROR_FILE_READ,
			Field:  "file",
			Reason: "file is not a regular audio file",
			Err:    errors.New("legacy Track source is not a regular file"),
		}
	}
	if limitErr := service.storage.validateUploadLength(file.Size()); limitErr != nil {
		return nil, library.MediaInspection{}, limitErr
	}
	inspection, err := service.inspector.Inspect(ctx, source.FilePath, nil)
	if err != nil {
		return nil, library.MediaInspection{}, err
	}
	currentFile, err := os.Stat(source.FilePath)
	if err != nil {
		return nil, library.MediaInspection{}, fileReadInspectionError(err)
	}
	if !os.SameFile(file, currentFile) || file.Size() != currentFile.Size() || !file.ModTime().Equal(currentFile.ModTime()) {
		return nil, library.MediaInspection{}, &ValidationError{
			Code:   "legacy_source_changed",
			Field:  "file",
			Reason: "legacy source changed during migration analysis",
			Err:    errors.New("legacy source changed during migration analysis"),
		}
	}
	return currentFile, inspection, nil
}

func fileReadInspectionError(err error) *library.InspectionError {
	return &library.InspectionError{Code: library.INSPECTION_ERROR_FILE_READ, Field: "file", Reason: "file could not be read", Err: err}
}

func isMigrationFileRejection(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var inspectionErr *library.InspectionError
	var validationErr *ValidationError
	return errors.As(err, &inspectionErr) || errors.As(err, &validationErr) || errors.Is(err, ErrUploadTooLarge) || errors.Is(err, ErrBatchTooLarge)
}

func validateMigrationSet(preview *MigrationPreview, candidates []migrationCandidate) {
	albums := migrationAlbumRuleSets(candidates)
	for _, candidate := range candidates {
		metadata := candidate.inspection.Metadata
		rules := albums[albumIdentityKey(metadata)]
		if err := validateMigrationAlbumCandidate(candidate.inspection, rules); err != nil {
			rejectMigrationFile(&preview.Files[candidate.previewIndex], err)
		}
	}
}

func migrationAlbumRuleSets(candidates []migrationCandidate) map[string]migrationAlbumRules {
	albums := make(map[string]migrationAlbumRules)
	for _, candidate := range candidates {
		inspection := candidate.inspection
		metadata := inspection.Metadata
		albumKey := albumIdentityKey(metadata)
		rules := albums[albumKey]
		if rules.trackTotals == nil {
			rules.trackTotals = make(map[int]int)
			rules.trackTotalConflicts = make(map[int]bool)
		}
		rules.isMultiDisc = rules.isMultiDisc || isMultiDisc(metadata)
		if rules.discTotal == 0 && metadata.DiscPosition.Total > 0 {
			rules.discTotal = metadata.DiscPosition.Total
		} else if metadata.DiscPosition.Total > 0 && metadata.DiscPosition.Total != rules.discTotal {
			rules.hasDiscTotalConflict = true
		}
		discNumber := metadata.DiscPosition.Number
		if rules.trackTotals[discNumber] == 0 && metadata.TrackPosition.Total > 0 {
			rules.trackTotals[discNumber] = metadata.TrackPosition.Total
		} else if metadata.TrackPosition.Total > 0 && metadata.TrackPosition.Total != rules.trackTotals[discNumber] {
			rules.trackTotalConflicts[discNumber] = true
		}
		if rules.artworkSHA256 == "" {
			rules.artworkSHA256 = inspection.AlbumArtwork.SHA256
		} else if rules.artworkSHA256 != inspection.AlbumArtwork.SHA256 {
			rules.hasArtworkConflict = true
		}
		albums[albumKey] = rules
	}
	return albums
}

func validateMigrationAlbumCandidate(inspection library.MediaInspection, rules migrationAlbumRules) error {
	metadata := inspection.Metadata
	if rules.isMultiDisc && !metadata.HasDiscNumber {
		return migrationAlbumValidationError("DISCNUMBER", "DISCNUMBER is required for a known multi-disc Album")
	}
	if rules.hasDiscTotalConflict {
		return migrationAlbumValidationError("DISCNUMBER", "DISCNUMBER total conflicts with another Track in the Album")
	}
	if rules.trackTotalConflicts[metadata.DiscPosition.Number] {
		return migrationAlbumValidationError("TRACKNUMBER", "TRACKNUMBER total conflicts with another Track in the Album")
	}
	if rules.hasArtworkConflict {
		return &ValidationError{
			Code:   string(library.INSPECTION_ERROR_INVALID_ARTWORK),
			Field:  "artwork",
			Reason: "embedded artwork differs from another Track in the Album",
			Err:    errors.New("album artwork hash conflicts with another migration Track"),
		}
	}
	return nil
}

func migrationAlbumValidationError(field, reason string) *ValidationError {
	return &ValidationError{Code: string(library.INSPECTION_ERROR_INVALID_METADATA), Field: field, Reason: reason, Err: errors.New(reason)}
}

func rejectMigrationFile(file *MigrationPreviewFile, err error) {
	rejected := rejectedMigrationFile(legacyMigrationSource{TrackID: file.TrackID, FilePath: file.OriginalFilename}, err)
	*file = rejected
}

func migrationStorageRequirement(preview MigrationPreview, candidates []migrationCandidate) (StorageRequirement, error) {
	var requirement StorageRequirement
	for _, candidate := range candidates {
		if preview.Files[candidate.previewIndex].State != MIGRATION_FILE_ACCEPTED {
			continue
		}
		var err error
		requirement.SelectedBytes, err = addByteCounts(requirement.SelectedBytes, candidate.fileSize)
		if err != nil {
			return StorageRequirement{}, fmt.Errorf("calculate Library Migration selected capacity: %w", err)
		}
		requirement.TemporaryBytes, err = addByteCounts(requirement.TemporaryBytes, candidate.fileSize, int64(len(candidate.inspection.AlbumArtwork.Data)))
		if err != nil {
			return StorageRequirement{}, fmt.Errorf("calculate Library Migration temporary capacity: %w", err)
		}
	}
	return requirement, nil
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

func rejectAcceptedMigrationFiles(preview *MigrationPreview, rejectionErr error) {
	code, reason := failureDetails(rejectionErr)
	if errors.Is(rejectionErr, ErrInsufficientStorage) {
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
