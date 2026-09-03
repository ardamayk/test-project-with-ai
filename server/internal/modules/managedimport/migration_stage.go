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
	identitiesByAlbum, err := service.existingMigrationAlbumIdentities(ctx, candidates)
	if err != nil {
		return MigrationStage{}, err
	}
	stage := MigrationStage{Files: make([]MigrationStageFile, 0, len(preview.Files))}
	for index, previewFile := range preview.Files {
		if previewFile.State == MIGRATION_FILE_REJECTED {
			stage.Files = append(stage.Files, rejectedMigrationStageFile(previewFile))
			stage.RejectedCount++
			continue
		}
		file, stageErr := service.stageMigrationCandidate(ctx, candidatesByIndex[index], identitiesByAlbum)
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

func (service *Service) stageMigrationCandidate(ctx context.Context, candidate migrationCandidate, identitiesByAlbum map[string]commitIdentity) (MigrationStageFile, error) {
	existingCopy, found, err := service.store.FindMigrationCopy(ctx, candidate.source.TrackID)
	if err != nil {
		return MigrationStageFile{}, err
	}
	if found && existingCopy.Status == "verified" {
		return verifiedMigrationStageFile(candidate, existingCopy)
	}
	if found {
		if retryErr := service.removeRetryableMigrationCopy(ctx, existingCopy); retryErr != nil {
			return MigrationStageFile{}, retryErr
		}
	}
	upload, inspection, err := service.copyAndVerifyMigrationCandidate(ctx, candidate)
	if err != nil {
		return MigrationStageFile{}, err
	}
	identity, err := service.store.ResolveCommitIdentity(ctx, inspection.Metadata)
	if err != nil {
		return MigrationStageFile{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	identity = reuseMigrationAlbumIdentity(identity, inspection.Metadata, identitiesByAlbum)
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

func (service *Service) existingMigrationAlbumIdentities(ctx context.Context, candidates []migrationCandidate) (map[string]commitIdentity, error) {
	identities := make(map[string]commitIdentity)
	for _, candidate := range candidates {
		copy, found, err := service.store.FindMigrationCopy(ctx, candidate.source.TrackID)
		if err != nil {
			return nil, err
		}
		if found && copy.Status == "verified" && copy.SourceFilePath == candidate.source.FilePath && copy.SourceSHA256 == candidate.inspection.FileSHA256 {
			identities[albumIdentityKey(candidate.inspection.Metadata)] = migrationCopyIdentity(copy)
		}
	}
	return identities, nil
}

func (service *Service) removeRetryableMigrationCopy(ctx context.Context, copy migrationCopyRecord) error {
	removeArtwork, err := service.store.IsMigrationArtworkExclusive(ctx, copy.SourceTrackID, copy.PendingArtworkPath)
	if err != nil {
		return err
	}
	if cleanupErr := service.storage.CleanupRecordedMigrationCopy(copy, removeArtwork); cleanupErr != nil {
		return cleanupErr
	}
	return service.store.DeleteRetryableMigrationCopy(ctx, copy.SourceTrackID)
}

func verifiedMigrationStageFile(candidate migrationCandidate, copy migrationCopyRecord) (MigrationStageFile, error) {
	if copy.SourceFilePath != candidate.source.FilePath || copy.SourceSHA256 != candidate.inspection.FileSHA256 {
		return MigrationStageFile{}, migrationSourceChangedError()
	}
	return MigrationStageFile{
		TrackID: candidate.source.TrackID, OriginalFilename: filepath.Base(candidate.source.FilePath),
		State: MIGRATION_STAGE_VERIFIED, PendingTrackID: copy.PendingTrackID, PendingPath: copy.PendingAudioPath,
		SourceSHA256: copy.SourceSHA256, PendingSHA256: copy.PendingSHA256,
	}, nil
}

func reuseMigrationAlbumIdentity(identity commitIdentity, metadata library.NormalizedMediaMetadata, identities map[string]commitIdentity) commitIdentity {
	albumKey := albumIdentityKey(metadata)
	if existingIdentity, ok := identities[albumKey]; ok {
		identity.AlbumArtistID = existingIdentity.AlbumArtistID
		identity.AlbumID = existingIdentity.AlbumID
		identity.ExistingArtworkPath = existingIdentity.ExistingArtworkPath
		identity.ExistingArtworkSHA256 = existingIdentity.ExistingArtworkSHA256
		return identity
	}
	identities[albumKey] = identity
	return identity
}

func migrationCopyIdentity(copy migrationCopyRecord) commitIdentity {
	return commitIdentity{AlbumArtistID: copy.PendingAlbumArtistID, AlbumID: copy.PendingAlbumID}
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
	plannedPlacement, err := service.storage.planMigrationPlacement(upload.Path, inspection, identity)
	if err != nil {
		return placedFiles{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	inspectionJSON, err := migrationInspectionJSON(inspection)
	if err != nil {
		return placedFiles{}, errors.Join(err, service.storage.RemoveStaged(upload.Path))
	}
	copy := verifiedMigrationCopy{
		Source: candidate.source, Identity: identity, Placement: plannedPlacement,
		SourceSHA256: candidate.inspection.FileSHA256, PendingSHA256: inspection.FileSHA256,
		ArtworkSHA256: inspection.AlbumArtwork.SHA256, InspectionJSON: inspectionJSON,
	}
	prepareErr := service.store.CreatePreparedMigrationCopy(ctx, copy)
	if prepareErr != nil {
		return placedFiles{}, errors.Join(prepareErr, service.storage.RemoveStaged(upload.Path))
	}
	if hookErr := service.afterMigrationPhase(MIGRATION_PHASE_PREPARED); hookErr != nil {
		return placedFiles{}, hookErr
	}
	placement, err := service.storage.PlaceMigration(plannedPlacement, inspection)
	if err != nil {
		return placedFiles{}, service.failPreparedMigrationCopy(ctx, candidate.source.TrackID, err, service.storage.RemoveStaged(upload.Path))
	}
	verificationErr := service.storage.VerifyMigrationPlacement(placement, identity, inspection.FileSHA256, inspection.AlbumArtwork.SHA256)
	if verificationErr != nil {
		return placedFiles{}, service.failPreparedMigrationCopy(ctx, candidate.source.TrackID, verificationErr, service.storage.CleanupMigrationPlacement(placement))
	}
	if err := service.store.MarkMigrationCopyVerified(ctx, candidate.source.TrackID); err != nil {
		return placedFiles{}, service.failPreparedMigrationCopy(ctx, candidate.source.TrackID, err, service.storage.CleanupMigrationPlacement(placement))
	}
	if hookErr := service.afterMigrationPhase(MIGRATION_PHASE_VERIFIED); hookErr != nil {
		return placedFiles{}, hookErr
	}
	return placement, nil
}

func (service *Service) failPreparedMigrationCopy(ctx context.Context, sourceTrackID string, operationErr, cleanupErr error) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	recordErr := service.store.MarkMigrationCopyFailed(recoveryCtx, sourceTrackID, operationErr.Error())
	return errors.Join(operationErr, cleanupErr, recordErr)
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
