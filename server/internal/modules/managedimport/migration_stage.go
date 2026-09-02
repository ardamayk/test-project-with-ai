package managedimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func (service *Service) StageMigration(ctx context.Context) (MigrationStage, error) {
	if !libraryMigrationPreviewMu.TryLock() {
		return MigrationStage{}, ErrMigrationInProgress
	}
	defer libraryMigrationPreviewMu.Unlock()
	preview, candidates, err := service.previewMigration(ctx)
	if err != nil {
		return MigrationStage{}, err
	}
	candidatesByIndex := make(map[int]migrationCandidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByIndex[candidate.previewIndex] = candidate
	}
	stage := MigrationStage{Files: make([]MigrationStageFile, 0, len(preview.Files))}
	for index, previewFile := range preview.Files {
		if previewFile.State == MIGRATION_FILE_REJECTED {
			stage.Files = append(stage.Files, rejectedMigrationStageFile(previewFile))
			stage.RejectedCount++
			continue
		}
		file, stageErr := service.stageMigrationCandidate(ctx, candidatesByIndex[index])
		if stageErr != nil {
			if ctx.Err() != nil {
				return MigrationStage{}, errors.Join(stageErr, ctx.Err())
			}
			stage.Files = append(stage.Files, failedMigrationStageFile(previewFile, stageErr))
			stage.FailedCount++
			continue
		}
		stage.Files = append(stage.Files, file)
		stage.VerifiedCount++
	}
	return stage, nil
}

func (service *Service) stageMigrationCandidate(ctx context.Context, candidate migrationCandidate) (MigrationStageFile, error) {
	upload, inspection, err := service.copyAndVerifyMigrationCandidate(ctx, candidate)
	if err != nil {
		return MigrationStageFile{}, err
	}
	identity, err := service.store.ResolveCommitIdentity(ctx, inspection.Metadata)
	if err != nil {
		return MigrationStageFile{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	placement, err := service.placeAndStoreMigrationCopy(ctx, candidate, upload, inspection, identity)
	if err != nil {
		return MigrationStageFile{}, err
	}
	return MigrationStageFile{
		TrackID: candidate.source.TrackID, OriginalFilename: filepath.Base(candidate.source.FilePath),
		State: MIGRATION_STAGE_VERIFIED, PendingTrackID: identity.TrackID, PendingPath: placement.AudioPath,
		SourceSHA256: candidate.inspection.FileSHA256, PendingSHA256: inspection.FileSHA256,
	}, nil
}

func (service *Service) copyAndVerifyMigrationCandidate(ctx context.Context, candidate migrationCandidate) (stagedUpload, library.MediaInspection, error) {
	upload, err := service.storage.StageMigrationSource(candidate.source.FilePath, candidate.fileSize)
	if err != nil {
		return stagedUpload{}, library.MediaInspection{}, err
	}
	if upload.SHA256 != candidate.inspection.FileSHA256 {
		return stagedUpload{}, library.MediaInspection{}, errors.Join(
			migrationSourceChangedError(),
			service.storage.RemoveStaged(upload.Path),
		)
	}
	inspection, err := service.inspector.Inspect(ctx, upload.Path, nil)
	if err != nil {
		return stagedUpload{}, library.MediaInspection{}, errors.Join(validationError(err), service.storage.RemoveStaged(upload.Path))
	}
	verificationErr := verifyMigrationInspection(candidate.inspection, inspection, upload.SHA256)
	if verificationErr != nil {
		return stagedUpload{}, library.MediaInspection{}, errors.Join(verificationErr, service.storage.RemoveStaged(upload.Path))
	}
	validationErr := service.validateMigrationInspection(ctx, inspection)
	if validationErr != nil {
		return stagedUpload{}, library.MediaInspection{}, errors.Join(validationErr, service.storage.RemoveStaged(upload.Path))
	}
	return upload, inspection, nil
}

func (service *Service) placeAndStoreMigrationCopy(ctx context.Context, candidate migrationCandidate, upload stagedUpload, inspection library.MediaInspection, identity commitIdentity) (placedFiles, error) {
	placement, err := service.storage.PlaceMigration(upload.Path, inspection, identity)
	if err != nil {
		return placedFiles{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	verificationErr := service.storage.VerifyMigrationPlacement(placement, identity, inspection.FileSHA256, inspection.AlbumArtwork.SHA256)
	if verificationErr != nil {
		return placedFiles{}, errors.Join(verificationErr, service.storage.CleanupMigrationPlacement(placement))
	}
	inspectionJSON, err := migrationInspectionJSON(inspection)
	if err != nil {
		return placedFiles{}, errors.Join(err, service.storage.CleanupMigrationPlacement(placement))
	}
	copy := verifiedMigrationCopy{
		Source: candidate.source, Identity: identity, Placement: placement,
		SourceSHA256: candidate.inspection.FileSHA256, PendingSHA256: inspection.FileSHA256,
		ArtworkSHA256: inspection.AlbumArtwork.SHA256, InspectionJSON: inspectionJSON,
	}
	storeErr := service.store.StoreVerifiedMigrationCopy(ctx, copy)
	if storeErr != nil {
		return placedFiles{}, errors.Join(storeErr, service.storage.CleanupMigrationPlacement(placement))
	}
	return placement, nil
}

func verifyMigrationInspection(source, pending library.MediaInspection, pendingSHA256 string) error {
	artworkMatches := source.AlbumArtwork.MIMEType == pending.AlbumArtwork.MIMEType &&
		source.AlbumArtwork.Width == pending.AlbumArtwork.Width &&
		source.AlbumArtwork.Height == pending.AlbumArtwork.Height &&
		source.AlbumArtwork.SHA256 == pending.AlbumArtwork.SHA256
	if pending.FileSHA256 != pendingSHA256 || source.FileSHA256 != pending.FileSHA256 ||
		!reflect.DeepEqual(source.Metadata, pending.Metadata) ||
		!reflect.DeepEqual(source.Audio, pending.Audio) || !artworkMatches {
		return &ValidationError{
			Code: "migration_copy_verification_failed", Field: "file",
			Reason: "pending migration copy differs from the accepted Legacy Track",
			Err:    errors.New("pending migration inspection differs from source preview"),
		}
	}
	return nil
}

func migrationInspectionJSON(inspection library.MediaInspection) (string, error) {
	inspection.AlbumArtwork.Data = nil
	encoded, err := json.Marshal(inspection)
	if err != nil {
		return "", fmt.Errorf("encode verified Library Migration inspection: %w", err)
	}
	return string(encoded), nil
}

func migrationSourceChangedError() *ValidationError {
	return &ValidationError{
		Code: "legacy_source_changed", Field: "file",
		Reason: "legacy source changed after migration preview",
		Err:    errors.New("copied Legacy Track hash differs from migration preview"),
	}
}

func rejectedMigrationStageFile(preview MigrationPreviewFile) MigrationStageFile {
	return MigrationStageFile{
		TrackID: preview.TrackID, OriginalFilename: preview.OriginalFilename, State: MIGRATION_STAGE_REJECTED,
		ErrorCode: preview.ErrorCode, ErrorField: preview.ErrorField, ErrorReason: preview.ErrorReason,
	}
}

func failedMigrationStageFile(preview MigrationPreviewFile, err error) MigrationStageFile {
	errorCode, errorField, errorReason := migrationStageFailure(err)
	return MigrationStageFile{
		TrackID: preview.TrackID, OriginalFilename: preview.OriginalFilename, State: MIGRATION_STAGE_FAILED,
		ErrorCode: errorCode, ErrorField: errorField, ErrorReason: errorReason,
	}
}

func migrationStageFailure(err error) (string, string, string) {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code, validationErr.Field, validationErr.Reason
	}
	var inspectionErr *library.InspectionError
	if errors.As(err, &inspectionErr) {
		return string(inspectionErr.Code), inspectionErr.Field, inspectionErr.Reason
	}
	errorCode, reason := failureDetails(err)
	return errorCode, "file", reason
}
