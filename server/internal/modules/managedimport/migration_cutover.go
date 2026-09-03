package managedimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	internaldb "github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

const MIGRATION_CUTOVER_INACTIVE_SOURCE_REASON = "Legacy source is no longer an active Legacy Track"

// ActivateMigration cuts over every verified Library Migration copy: it
// re-runs the migration preview and staging, promotes each verified copy to
// its Canonical Library Path under the new stable Track ID, and removes the
// corresponding Legacy Track and its Playlist, Queue, and snapshot references
// only after the managed activation succeeds. The report distinguishes
// migrated, rejected, failed, and not-attempted files.
func (service *Service) ActivateMigration(ctx context.Context) (MigrationCutover, error) {
	if !libraryMigrationPreviewMu.TryLock() {
		return MigrationCutover{}, ErrMigrationInProgress
	}
	defer libraryMigrationPreviewMu.Unlock()
	managedImportCommitMu.Lock()
	defer managedImportCommitMu.Unlock()

	inactiveFiles, verifiedSourceIDs, err := service.reconcileVerifiedMigrationCopies(ctx)
	if err != nil {
		return MigrationCutover{}, err
	}
	inactiveBySource := make(map[string]bool, len(inactiveFiles))
	for _, file := range inactiveFiles {
		inactiveBySource[file.TrackID] = true
	}
	preview, candidates, err := service.previewMigrationExcludingVerified(ctx, verifiedSourceIDs)
	if err != nil {
		return MigrationCutover{}, err
	}
	candidatesByIndex := make(map[int]migrationCandidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByIndex[candidate.previewIndex] = candidate
	}
	identitiesByAlbum, err := service.existingMigrationAlbumIdentities(ctx, candidates)
	if err != nil {
		return MigrationCutover{}, err
	}
	cutover := MigrationCutover{Files: make([]MigrationCutoverFile, 0, len(preview.Files)+len(inactiveFiles))}
	for index, previewFile := range preview.Files {
		if previewFile.State == MIGRATION_FILE_REJECTED {
			cutover.Files = append(cutover.Files, rejectedMigrationCutoverFile(previewFile))
			cutover.RejectedCount++
			continue
		}
		if inactiveBySource[previewFile.TrackID] {
			continue
		}
		if _, stageErr := service.stageMigrationCandidate(ctx, candidatesByIndex[index], identitiesByAlbum); stageErr != nil {
			if ctx.Err() != nil {
				return MigrationCutover{}, errors.Join(stageErr, ctx.Err())
			}
			cutover.Files = append(cutover.Files, failedMigrationCutoverFile(previewFile, stageErr))
			cutover.FailedCount++
			continue
		}
		file, activateErr := service.activateMigrationCandidate(ctx, candidatesByIndex[index].source)
		if activateErr != nil {
			if ctx.Err() != nil {
				return MigrationCutover{}, errors.Join(activateErr, ctx.Err())
			}
			cutover.Files = append(cutover.Files, failedMigrationCutoverFile(previewFile, activateErr))
			cutover.FailedCount++
			continue
		}
		cutover.Files = append(cutover.Files, file)
		cutover.MigratedCount++
	}
	cutover.Files = append(cutover.Files, inactiveFiles...)
	cutover.NotAttemptedCount = len(inactiveFiles)
	return cutover, nil
}

// reconcileVerifiedMigrationCopies reports verified copies whose Legacy source
// is no longer an active Legacy Track, and returns the source IDs of every
// verified copy. Untouched copies can be revisited by a later run once the
// source is restored.
func (service *Service) reconcileVerifiedMigrationCopies(ctx context.Context) ([]MigrationCutoverFile, map[string]bool, error) {
	copies, err := service.store.ListVerifiedMigrationCopies(ctx)
	if err != nil {
		return nil, nil, err
	}
	verifiedSourceIDs := make(map[string]bool, len(copies))
	inactiveFiles := make([]MigrationCutoverFile, 0)
	for _, copy := range copies {
		verifiedSourceIDs[copy.SourceTrackID] = true
		active, err := service.store.LegacyMigrationSourceActive(ctx, copy.SourceTrackID)
		if err != nil {
			return nil, nil, err
		}
		if active {
			continue
		}
		inactiveFiles = append(inactiveFiles, MigrationCutoverFile{
			TrackID:          copy.SourceTrackID,
			OriginalFilename: filepath.Base(copy.SourceFilePath),
			State:            MIGRATION_CUTOVER_NOT_ATTEMPTED,
			ErrorCode:        ERROR_CODE_MIGRATION_SOURCE_INACTIVE,
			ErrorField:       "file",
			ErrorReason:      MIGRATION_CUTOVER_INACTIVE_SOURCE_REASON,
		})
	}
	return inactiveFiles, verifiedSourceIDs, nil
}

// activateMigrationCandidate promotes one verified copy into an active
// Managed Track and removes the corresponding Legacy Track. It activates from
// the inspection verified at staging: the pending Track, Album, and path
// identities were derived from that inspection, so a fresh inspection can no
// longer be trusted to match them.
func (service *Service) activateMigrationCandidate(ctx context.Context, source legacyMigrationSource) (MigrationCutoverFile, error) {
	file := MigrationCutoverFile{
		TrackID:          source.TrackID,
		OriginalFilename: filepath.Base(source.FilePath),
	}
	copy, found, err := service.store.FindMigrationCopy(ctx, source.TrackID)
	if err != nil {
		return file, err
	}
	if !found || copy.Status != "verified" {
		return file, fmt.Errorf("verified Library Migration copy for legacy Track %q is missing", source.TrackID)
	}
	inspectionJSON, err := service.store.FindMigrationCopyInspection(ctx, source.TrackID)
	if err != nil {
		return file, err
	}
	inspection, err := decodeMigrationInspection(inspectionJSON)
	if err != nil {
		return file, err
	}
	existingArtworkPath, existingArtworkSHA256, err := service.store.FindMigrationAlbumArtwork(ctx, copy.PendingAlbumID)
	if err != nil {
		return file, err
	}
	if existingArtworkPath != "" && existingArtworkSHA256 != copy.ArtworkSHA256 {
		return file, migrationAlbumArtworkConflict()
	}
	// The pending artwork becomes redundant once the Album Artwork is already
	// registered; remove it after activation if no other copy still needs it.
	removePendingArtwork := existingArtworkPath != ""
	if removePendingArtwork {
		exclusive, exclusiveErr := service.store.IsMigrationArtworkExclusive(ctx, copy.SourceTrackID, copy.PendingArtworkPath)
		if exclusiveErr != nil {
			return file, exclusiveErr
		}
		removePendingArtwork = exclusive
	}
	placement, err := service.storage.PromoteMigrationCopy(copy, existingArtworkPath)
	if err != nil {
		return file, err
	}
	invalidations, err := service.store.ActivateMigrationCopy(ctx, copy, inspection, placement, existingArtworkPath)
	if err != nil {
		return file, err
	}
	if removePendingArtwork {
		if cleanupErr := service.storage.RemovePendingMigrationArtwork(copy); cleanupErr != nil {
			slog.WarnContext(ctx, "removing the redundant pending Library Migration artwork failed", "error", cleanupErr)
		}
	}
	service.publishQueueInvalidations(invalidations)
	file.State = MIGRATION_CUTOVER_MIGRATED
	file.CreatedTrackID = copy.PendingTrackID
	file.ContentSHA256 = copy.PendingSHA256
	return file, nil
}

func migrationAlbumArtworkConflict() *ValidationError {
	return &ValidationError{
		Code:   "album_artwork_conflict",
		Field:  "artwork",
		Reason: "embedded Album Artwork differs from the existing Album",
		Err:    errors.New("embedded Album Artwork differs from the existing Album"),
	}
}

func decodeMigrationInspection(encoded string) (library.MediaInspection, error) {
	var inspection library.MediaInspection
	if err := json.Unmarshal([]byte(encoded), &inspection); err != nil {
		return library.MediaInspection{}, fmt.Errorf("decode verified Library Migration inspection: %w", err)
	}
	return inspection, nil
}

// ActivateMigrationCopy atomically removes the migrated Legacy Track with all
// of its references and inserts the new active Managed Track. Either the whole
// cutover commits or the Legacy Track remains exactly as it was.
func (store *Store) ActivateMigrationCopy(ctx context.Context, copy migrationCopyRecord, inspection library.MediaInspection, placement placedFiles, existingArtworkPath string) (invalidations []queueInvalidation, returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return nil, fmt.Errorf("begin Library Migration cutover: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "Library Migration cutover"))
	}()
	legacyAlbumID, loadErr := loadLegacyMigrationTrackState(ctx, transaction, copy)
	if loadErr != nil {
		return nil, loadErr
	}
	if artworkErr := verifyMigrationCutoverArtwork(ctx, transaction, copy, existingArtworkPath); artworkErr != nil {
		return nil, artworkErr
	}
	queueReferences, listErr := listDeletionQueues(ctx, transaction, copy.SourceTrackID)
	if listErr != nil {
		return nil, listErr
	}
	invalidations = make([]queueInvalidation, 0, len(queueReferences))
	for _, queue := range queueReferences {
		var invalidation queueInvalidation
		invalidation.userID = queue.UserID
		if bumpErr := transaction.QueryRowContext(ctx, `
			INSERT INTO playback_queue_state (user_id, revision, event_sequence) VALUES (?, 1, 1)
			ON CONFLICT(user_id) DO UPDATE SET revision = revision + 1, event_sequence = event_sequence + 1
			RETURNING revision, event_sequence`, queue.UserID).Scan(&invalidation.revision, &invalidation.sequence); bumpErr != nil {
			return nil, fmt.Errorf("advance affected Queue revision: %w", bumpErr)
		}
		invalidations = append(invalidations, invalidation)
	}
	if _, deleteErr := transaction.ExecContext(ctx, `DELETE FROM legacy_migration_copies WHERE source_track_id = ?`, copy.SourceTrackID); deleteErr != nil {
		return nil, fmt.Errorf("delete cut over Library Migration copy: %w", deleteErr)
	}
	if _, queueDeleteErr := transaction.ExecContext(ctx, `DELETE FROM playback_queue WHERE track_id = ?`, copy.SourceTrackID); queueDeleteErr != nil {
		return nil, fmt.Errorf("drop Queue references: %w", queueDeleteErr)
	}
	if _, playlistDeleteErr := transaction.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE track_id = ?`, copy.SourceTrackID); playlistDeleteErr != nil {
		return nil, fmt.Errorf("drop Playlist references: %w", playlistDeleteErr)
	}
	artistIDs, artistListErr := deletionArtistIDs(ctx, transaction, copy.SourceTrackID, legacyAlbumID)
	if artistListErr != nil {
		return nil, artistListErr
	}
	identity := commitIdentity{
		TrackID: copy.PendingTrackID, AlbumID: copy.PendingAlbumID, AlbumArtistID: copy.PendingAlbumArtistID,
		ExistingArtworkPath: existingArtworkPath,
	}
	if existingArtworkPath != "" {
		identity.ExistingArtworkSHA256 = copy.ArtworkSHA256
	}
	data := commitData{Identity: identity, Placement: placement, Inspection: inspection, AlbumKey: albumIdentityKey(inspection.Metadata)}
	artistIDMap, upsertErr := upsertArtists(ctx, transaction, inspection.Metadata, identity)
	if upsertErr != nil {
		return nil, upsertErr
	}
	if albumErr := upsertAlbum(ctx, transaction, data, artistIDMap); albumErr != nil {
		return nil, albumErr
	}
	// The Legacy Track and the new Managed Track claim the same active Album
	// position; hide the Legacy Track for the remainder of the transaction so
	// the active-position unique index never sees both. Everything commits or
	// rolls back together, so the Legacy Track only ever leaves the active
	// library when the managed activation succeeds.
	if _, hideErr := transaction.ExecContext(ctx, `UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ? AND missing_at IS NULL`, copy.SourceTrackID); hideErr != nil {
		return nil, fmt.Errorf("hide migrated Legacy Track: %w", hideErr)
	}
	if trackErr := insertTrack(ctx, transaction, data); trackErr != nil {
		return nil, trackErr
	}
	// Registered Album Artwork sourced from the removed Legacy Track is
	// re-owned by the new Managed Track so the removal cannot break the
	// artwork source invariants. This must run after the insert: the
	// artwork-source triggers require the source Track to belong to the
	// Album at all times.
	if _, reownErr := transaction.ExecContext(ctx, `
		UPDATE album_artwork SET source_track_id = ? WHERE source_track_id = ?`, copy.PendingTrackID, copy.SourceTrackID); reownErr != nil {
		return nil, fmt.Errorf("re-own Album Artwork sourced from the migrated Legacy Track: %w", reownErr)
	}
	trackResult, deleteTrackErr := transaction.ExecContext(ctx, `DELETE FROM tracks WHERE id = ? AND missing_at IS NOT NULL`, copy.SourceTrackID)
	if deleteTrackErr != nil {
		return nil, fmt.Errorf("remove migrated Legacy Track: %w", deleteTrackErr)
	}
	if mutationErr := requireMutation(trackResult); mutationErr != nil {
		return nil, fmt.Errorf("remove migrated Legacy Track: %w", mutationErr)
	}
	if legacyAlbumID != copy.PendingAlbumID {
		if _, deleteAlbumErr := transaction.ExecContext(ctx, `
			DELETE FROM albums WHERE id = ? AND NOT EXISTS (SELECT 1 FROM tracks WHERE album_id = ?)`, legacyAlbumID, legacyAlbumID); deleteAlbumErr != nil {
			return nil, fmt.Errorf("delete emptied Legacy Album: %w", deleteAlbumErr)
		}
	}
	for _, artistID := range artistIDs {
		if _, deleteArtistErr := transaction.ExecContext(ctx, `
			DELETE FROM artists WHERE id = ?
			AND NOT EXISTS (SELECT 1 FROM albums WHERE albums.artist_id = ?)
			AND NOT EXISTS (SELECT 1 FROM track_artists WHERE artist_id = ?)
			AND NOT EXISTS (SELECT 1 FROM album_artists WHERE artist_id = ?)`, artistID, artistID, artistID, artistID); deleteArtistErr != nil {
			return nil, fmt.Errorf("delete inactive Artist: %w", deleteArtistErr)
		}
	}
	// Removing legacy entities orphans transition-only legacy Artists and
	// Genres; clean them like the established library deletion paths do.
	// Legacy identity promotion (FinalizeLegacyRemoval) is deliberately not
	// run here: the inserted Managed Album now owns the contested identity
	// key, and promoting a surviving legacy Album onto the same key would
	// violate the unique Album identity index and abort the cutover.
	if cleanupErr := internaldb.CleanupLegacyTransitionEntities(ctx, transaction); cleanupErr != nil {
		return nil, fmt.Errorf("clean legacy transition entities after the Library Migration cutover: %w", cleanupErr)
	}
	// The Legacy Track is gone; activate the new Managed Track.
	if _, publishErr := transaction.ExecContext(ctx, `UPDATE tracks SET missing_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, copy.PendingTrackID); publishErr != nil {
		return nil, fmt.Errorf("activate the cut over Managed Track: %w", publishErr)
	}
	if relationshipErr := insertRelationships(ctx, transaction, data, artistIDMap); relationshipErr != nil {
		return nil, relationshipErr
	}
	if existingArtworkPath == "" {
		if artworkInsertErr := insertMigrationAlbumArtwork(ctx, transaction, copy, placement, inspection); artworkInsertErr != nil {
			return nil, artworkInsertErr
		}
	}
	if commitErr := transaction.Commit(); commitErr != nil {
		return nil, fmt.Errorf("commit Library Migration cutover: %w", commitErr)
	}
	return invalidations, nil
}

func loadLegacyMigrationTrackState(ctx context.Context, transaction *sql.Tx, copy migrationCopyRecord) (string, error) {
	var legacyAlbumID string
	err := transaction.QueryRowContext(ctx, `
		SELECT tracks.album_id
		FROM tracks
		JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ? AND tracks.missing_at IS NULL
		AND track_sources.source_kind = 'legacy' AND track_sources.file_path = ?`,
		copy.SourceTrackID, copy.SourceFilePath,
	).Scan(&legacyAlbumID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("legacy Track %q changed before the Library Migration cutover", copy.SourceTrackID)
	}
	if err != nil {
		return "", fmt.Errorf("load migrated Legacy Track: %w", err)
	}
	return legacyAlbumID, nil
}

func verifyMigrationCutoverArtwork(ctx context.Context, transaction *sql.Tx, copy migrationCopyRecord, expectedArtworkPath string) error {
	var artworkPath, artworkSHA256 string
	err := transaction.QueryRowContext(ctx, `
		SELECT COALESCE(file_path, ''), COALESCE(content_sha256, '')
		FROM album_artwork WHERE album_id = ?`, copy.PendingAlbumID,
	).Scan(&artworkPath, &artworkSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedArtworkPath != "" {
			return fmt.Errorf("existing Album Artwork disappeared during the Library Migration cutover")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load existing Album Artwork: %w", err)
	}
	if artworkSHA256 != copy.ArtworkSHA256 || artworkPath != expectedArtworkPath {
		return &ValidationError{
			Code:   "album_artwork_conflict",
			Field:  "artwork",
			Reason: "embedded Album Artwork differs from the existing Album",
			Err:    errors.New("embedded Album Artwork differs from the existing Album"),
		}
	}
	return nil
}

func insertMigrationAlbumArtwork(ctx context.Context, transaction *sql.Tx, copy migrationCopyRecord, placement placedFiles, inspection library.MediaInspection) error {
	artworkInfo, err := os.Stat(placement.ArtworkPath)
	if err != nil {
		return fmt.Errorf("stat canonical Library Migration Album Artwork: %w", err)
	}
	artwork := inspection.AlbumArtwork
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type, width, height,
			encoded_size_bytes, file_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), copy.PendingAlbumID, copy.PendingTrackID, copy.ArtworkSHA256, artwork.MIMEType,
		artwork.Width, artwork.Height, artworkInfo.Size(), placement.ArtworkPath,
	)
	if err != nil {
		return fmt.Errorf("create migrated Album Artwork: %w", err)
	}
	return nil
}

func rejectedMigrationCutoverFile(preview MigrationPreviewFile) MigrationCutoverFile {
	return MigrationCutoverFile{
		TrackID: preview.TrackID, OriginalFilename: preview.OriginalFilename, State: MIGRATION_CUTOVER_REJECTED,
		ErrorCode: preview.ErrorCode, ErrorField: preview.ErrorField, ErrorReason: preview.ErrorReason,
	}
}

func failedMigrationCutoverFile(preview MigrationPreviewFile, err error) MigrationCutoverFile {
	errorCode, errorField, errorReason := migrationStageFailure(err)
	return MigrationCutoverFile{
		TrackID: preview.TrackID, OriginalFilename: preview.OriginalFilename, State: MIGRATION_CUTOVER_FAILED,
		ErrorCode: errorCode, ErrorField: errorField, ErrorReason: errorReason,
	}
}
