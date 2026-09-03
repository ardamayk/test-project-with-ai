package managedimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const ERROR_CODE_CLEANUP_NOT_MIGRATED = "legacy_source_not_migrated"
const ERROR_CODE_CLEANUP_MANAGED_TRACK_MISSING = "migrated_track_missing"
const ERROR_CODE_CLEANUP_SOURCE_MISSING = "legacy_source_missing"
const ERROR_CODE_CLEANUP_SOURCE_CHANGED = "legacy_source_changed"
const ERROR_CODE_CLEANUP_SOURCE_UNSAFE = "legacy_source_unsafe"
const ERROR_CODE_CLEANUP_SOURCE_OUTSIDE_MUSIC_PATHS = "legacy_source_outside_music_paths"
const ERROR_CODE_CLEANUP_DELETE_FAILED = "legacy_source_delete_failed"

// migratedSourceRecord is the durable proof that a legacy source file was
// activated as a Managed Track by a Library Migration cutover.
type migratedSourceRecord struct {
	TrackID        string
	SourceTrackID  string
	SourceFilePath string
	SourceSHA256   string
}

// resolvedCleanupTarget is a migrated source that passed every eligibility
// check at the moment of resolution.
type resolvedCleanupTarget struct {
	record    migratedSourceRecord
	root      string
	relative  string
	sizeBytes int64
}

// PreviewMigrationCleanup lists every legacy source file: sources proven to
// correspond to an active migrated Managed Track are eligible for cleanup,
// while rejected, failed, pending, unverified, changed, or unsafe sources are
// reported as ineligible with a reason. The preview never mutates anything.
func (service *Service) PreviewMigrationCleanup(ctx context.Context) (MigrationCleanupPreview, error) {
	if !libraryMigrationPreviewMu.TryLock() {
		return MigrationCleanupPreview{}, ErrMigrationInProgress
	}
	defer libraryMigrationPreviewMu.Unlock()
	return service.previewMigrationCleanup(ctx)
}

func (service *Service) previewMigrationCleanup(ctx context.Context) (MigrationCleanupPreview, error) {
	preview := MigrationCleanupPreview{Files: make([]MigrationCleanupPreviewFile, 0)}
	records, err := service.store.ListMigratedSources(ctx)
	if err != nil {
		return MigrationCleanupPreview{}, err
	}
	for _, record := range records {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return MigrationCleanupPreview{}, ctxErr
		}
		file := MigrationCleanupPreviewFile{
			TrackID: record.TrackID, SourceTrackID: record.SourceTrackID,
			OriginalFilename: filepath.Base(record.SourceFilePath), State: MIGRATION_CLEANUP_ELIGIBLE,
			ContentSHA256: record.SourceSHA256,
		}
		target, resolveErr := service.resolveCleanupTarget(ctx, record)
		if resolveErr != nil {
			if ctx.Err() != nil {
				return MigrationCleanupPreview{}, resolveErr
			}
			file.State = MIGRATION_CLEANUP_INELIGIBLE
			file.ErrorCode, file.ErrorField, file.ErrorReason = migrationStageFailure(resolveErr)
			preview.Files = append(preview.Files, file)
			preview.IneligibleCount++
			continue
		}
		file.SizeBytes = target.sizeBytes
		preview.Files = append(preview.Files, file)
		preview.EligibleCount++
		preview.TotalSizeBytes += target.sizeBytes
	}
	legacyFiles, err := service.listUnmigratedLegacySources(ctx)
	if err != nil {
		return MigrationCleanupPreview{}, err
	}
	preview.Files = append(preview.Files, legacyFiles...)
	preview.IneligibleCount += len(legacyFiles)
	return preview, nil
}

// listUnmigratedLegacySources reports every active Legacy Track as an
// ineligible cleanup entry so rejected, pending, verified-but-not-cut-over,
// and failed sources are visibly unselectable.
func (service *Service) listUnmigratedLegacySources(ctx context.Context) ([]MigrationCleanupPreviewFile, error) {
	sources, err := service.store.ListLegacyMigrationSources(ctx)
	if err != nil {
		return nil, err
	}
	files := make([]MigrationCleanupPreviewFile, 0, len(sources))
	for _, source := range sources {
		copy, found, err := service.store.FindMigrationCopy(ctx, source.TrackID)
		if err != nil {
			return nil, err
		}
		reason := "Legacy source has not been migrated"
		if found {
			switch copy.Status {
			case "verified":
				reason = "Legacy source is verified but its migration has not been cut over"
			case "failed":
				reason = "Legacy source migration failed and has not been retried"
			default:
				reason = "Legacy source migration is still pending"
			}
		}
		files = append(files, MigrationCleanupPreviewFile{
			TrackID: source.TrackID, OriginalFilename: filepath.Base(source.FilePath), State: MIGRATION_CLEANUP_INELIGIBLE,
			ErrorCode: ERROR_CODE_CLEANUP_NOT_MIGRATED, ErrorField: "file", ErrorReason: reason,
		})
	}
	return files, nil
}

// CleanupMigratedSources deletes the exact legacy source files named in the
// confirmation. Every target is resolved and verified again before any file is
// removed; if a single target no longer matches the confirmed selection, count,
// or total size, nothing is deleted. Cleanup is never triggered by a migration
// phase; it only runs through this explicit confirmation.
func (service *Service) CleanupMigratedSources(ctx context.Context, confirmation MigrationCleanupConfirmation) (MigrationCleanup, error) {
	if !libraryMigrationPreviewMu.TryLock() {
		return MigrationCleanup{}, ErrMigrationInProgress
	}
	defer libraryMigrationPreviewMu.Unlock()
	managedImportCommitMu.Lock()
	defer managedImportCommitMu.Unlock()

	targets, err := service.resolveCleanupSelection(ctx, confirmation)
	if err != nil {
		return MigrationCleanup{}, err
	}
	cleanup := MigrationCleanup{Files: make([]MigrationCleanupFile, 0, len(targets))}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return MigrationCleanup{}, err
		}
		file := MigrationCleanupFile{
			TrackID: target.record.TrackID, SourceTrackID: target.record.SourceTrackID,
			OriginalFilename: filepath.Base(target.record.SourceFilePath), SizeBytes: target.sizeBytes,
		}
		prunedDirectories, deleteErr := service.deleteCleanupTarget(ctx, target)
		if deleteErr != nil {
			if ctx.Err() != nil {
				return MigrationCleanup{}, deleteErr
			}
			file.State = MIGRATION_CLEANUP_FAILED
			file.ErrorCode, file.ErrorField, file.ErrorReason = migrationStageFailure(deleteErr)
			cleanup.Files = append(cleanup.Files, file)
			cleanup.FailedCount++
			continue
		}
		file.State = MIGRATION_CLEANUP_DELETED
		cleanup.Files = append(cleanup.Files, file)
		cleanup.DeletedCount++
		cleanup.DeletedBytes += target.sizeBytes
		cleanup.PrunedDirectoryCount += prunedDirectories
	}
	return cleanup, nil
}

// resolveCleanupSelection turns the confirmed Track IDs into verified targets
// and rejects the whole selection when any ID is unknown, duplicated, or no
// longer eligible, or when the confirmed count or total size disagree.
func (service *Service) resolveCleanupSelection(ctx context.Context, confirmation MigrationCleanupConfirmation) ([]resolvedCleanupTarget, error) {
	if len(confirmation.TrackIDs) == 0 || confirmation.FileCount != len(confirmation.TrackIDs) {
		return nil, fmt.Errorf("%w: confirmed file count %d does not match %d selected files", ErrCleanupConflict, confirmation.FileCount, len(confirmation.TrackIDs))
	}
	seen := make(map[string]bool, len(confirmation.TrackIDs))
	targets := make([]resolvedCleanupTarget, 0, len(confirmation.TrackIDs))
	var totalSize int64
	for _, trackID := range confirmation.TrackIDs {
		if seen[trackID] {
			return nil, fmt.Errorf("%w: Track %q is selected more than once", ErrCleanupConflict, trackID)
		}
		seen[trackID] = true
		record, found, err := service.store.FindMigratedSource(ctx, trackID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: Track %q has no migrated legacy source", ErrCleanupConflict, trackID)
		}
		target, resolveErr := service.resolveCleanupTarget(ctx, record)
		if resolveErr != nil {
			if ctx.Err() != nil {
				return nil, resolveErr
			}
			return nil, fmt.Errorf("%w: %v", ErrCleanupConflict, resolveErr)
		}
		targets = append(targets, target)
		totalSize += target.sizeBytes
	}
	if totalSize != confirmation.TotalSizeBytes {
		return nil, fmt.Errorf("%w: confirmed total size %d does not match %d bytes", ErrCleanupConflict, confirmation.TotalSizeBytes, totalSize)
	}
	return targets, nil
}

// resolveCleanupTarget proves that a recorded source still corresponds to an
// active migrated Managed Track: the Managed Track must be active with the
// recorded hash and intact managed bytes, and the legacy source must be a
// regular, symlink-free file inside a configured music path whose bytes still
// hash to the recorded value.
func (service *Service) resolveCleanupTarget(ctx context.Context, record migratedSourceRecord) (resolvedCleanupTarget, error) {
	managedPath, managedHash, err := service.store.FindActiveManagedTrackSource(ctx, record.TrackID)
	if err != nil {
		return resolvedCleanupTarget{}, err
	}
	if managedPath == "" || managedHash != record.SourceSHA256 {
		return resolvedCleanupTarget{}, cleanupValidationError(ERROR_CODE_CLEANUP_MANAGED_TRACK_MISSING, "the migrated Managed Track is no longer active with the migrated bytes")
	}
	if _, _, resolveErr := service.storage.ResolveManagedFile(managedPath, record.SourceSHA256); resolveErr != nil {
		return resolvedCleanupTarget{}, cleanupValidationError(ERROR_CODE_CLEANUP_MANAGED_TRACK_MISSING, "the migrated Managed Track file no longer matches the migrated bytes")
	}
	root, relative, containErr := service.legacySources.contain(record.SourceFilePath)
	if containErr != nil {
		return resolvedCleanupTarget{}, containErr
	}
	sizeBytes, verifyErr := service.legacySources.verify(ctx, root, relative, record.SourceSHA256)
	if verifyErr != nil {
		return resolvedCleanupTarget{}, verifyErr
	}
	return resolvedCleanupTarget{record: record, root: root, relative: relative, sizeBytes: sizeBytes}, nil
}

// deleteCleanupTarget removes exactly one verified legacy source file, records
// the cleanup, and prunes the now-empty parent directories up to the music
// path root. The file is verified once more immediately before removal.
func (service *Service) deleteCleanupTarget(ctx context.Context, target resolvedCleanupTarget) (int, error) {
	prunedDirectories, err := service.legacySources.remove(ctx, target.root, target.relative, target.record.SourceSHA256)
	if err != nil {
		return 0, err
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), VALIDATION_CLEANUP_TIMEOUT)
	defer cancel()
	if markErr := service.store.MarkMigratedSourceCleaned(recordCtx, target.record.TrackID); markErr != nil {
		return prunedDirectories, markErr
	}
	return prunedDirectories, nil
}

func cleanupValidationError(code, reason string) *ValidationError {
	return &ValidationError{Code: code, Field: "file", Reason: reason, Err: errors.New(reason)}
}

// legacySourceStorage performs every filesystem operation on legacy source
// files. Files are only ever addressed relative to a configured music path
// opened as an os.Root, so a moved or symlinked source can never redirect the
// deletion outside the legacy library.
type legacySourceStorage struct {
	musicPaths []string
}

func newLegacySourceStorage(musicPaths []string) *legacySourceStorage {
	storage := &legacySourceStorage{musicPaths: make([]string, 0, len(musicPaths))}
	for _, musicPath := range musicPaths {
		absolutePath, err := filepath.Abs(musicPath)
		if err != nil {
			continue
		}
		storage.musicPaths = append(storage.musicPaths, filepath.Clean(absolutePath))
	}
	return storage
}

// contain resolves the configured music path that holds the source file and
// the path relative to it.
func (storage *legacySourceStorage) contain(sourcePath string) (string, string, error) {
	absolutePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", "", cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, "the legacy source path cannot be resolved")
	}
	for _, musicPath := range storage.musicPaths {
		relative, relErr := filepath.Rel(musicPath, absolutePath)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		return musicPath, relative, nil
	}
	return "", "", cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_OUTSIDE_MUSIC_PATHS, "the legacy source is outside the configured music paths")
}

func (storage *legacySourceStorage) openRoot(musicPath string) (*os.Root, error) {
	root, err := os.OpenRoot(musicPath)
	if err != nil {
		return nil, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, fmt.Sprintf("the music path cannot be opened: %v", err))
	}
	return root, nil
}

// verify confirms the source is a regular, symlink-free file whose bytes hash
// to the recorded value and returns its size.
func (storage *legacySourceStorage) verify(ctx context.Context, musicPath, relative, expectedHash string) (sizeBytes int64, returnErr error) {
	root, err := storage.openRoot(musicPath)
	if err != nil {
		return 0, err
	}
	defer func() { returnErr = errors.Join(returnErr, closeLegacySourceRoot(root)) }()
	return verifyLegacySource(ctx, root, relative, expectedHash)
}

func verifyLegacySource(ctx context.Context, root *os.Root, relative, expectedHash string) (int64, error) {
	if err := rejectSymlinks(root, relative); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_MISSING, "the legacy source file no longer exists")
		}
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, "the legacy source path contains a symbolic link or cannot be inspected")
	}
	info, err := root.Lstat(relative)
	if err != nil || !info.Mode().IsRegular() {
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, "the legacy source is not a regular file")
	}
	file, err := root.Open(relative)
	if err != nil {
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, "the legacy source file cannot be read")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_UNSAFE, "the legacy source file cannot be read")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_SOURCE_CHANGED, "the legacy source bytes changed after the migration")
	}
	return info.Size(), nil
}

// remove deletes exactly the verified source file and then removes each
// parent directory that is left empty, walking upward one level at a time
// and stopping at the first non-empty directory or the music path root.
func (storage *legacySourceStorage) remove(ctx context.Context, musicPath, relative, expectedHash string) (prunedDirectories int, returnErr error) {
	root, err := storage.openRoot(musicPath)
	if err != nil {
		return 0, err
	}
	defer func() { returnErr = errors.Join(returnErr, closeLegacySourceRoot(root)) }()
	if _, err := verifyLegacySource(ctx, root, relative, expectedHash); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := root.Remove(relative); err != nil {
		return 0, cleanupValidationError(ERROR_CODE_CLEANUP_DELETE_FAILED, fmt.Sprintf("the legacy source file could not be deleted: %v", err))
	}
	return pruneEmptyLegacyDirectories(root, filepath.Dir(relative)), nil
}

// pruneEmptyLegacyDirectories removes empty ancestors of a deleted source
// without recursing into siblings; a directory that still holds any entry
// stops the walk. Failures are not fatal: the source file is already gone.
func pruneEmptyLegacyDirectories(root *os.Root, directoryPath string) int {
	pruned := 0
	for directoryPath != "." && directoryPath != string(filepath.Separator) {
		info, err := root.Lstat(directoryPath)
		if err != nil || !info.IsDir() {
			return pruned
		}
		directory, err := root.Open(directoryPath)
		if err != nil {
			return pruned
		}
		entries, readErr := directory.ReadDir(1)
		closeErr := directory.Close()
		if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil || len(entries) > 0 {
			return pruned
		}
		if err := root.Remove(directoryPath); err != nil {
			return pruned
		}
		pruned++
		directoryPath = filepath.Dir(directoryPath)
	}
	return pruned
}

func closeLegacySourceRoot(root *os.Root) error {
	if err := root.Close(); err != nil {
		return fmt.Errorf("close music path root %q: %w", root.Name(), err)
	}
	return nil
}

// RecordMigratedSource stores the proof that the cutover transaction activated
// the legacy source as the given Managed Track.
func recordMigratedSource(ctx context.Context, transaction *sql.Tx, copy migrationCopyRecord) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO legacy_migration_sources (track_id, source_track_id, source_file_path, source_sha256)
		VALUES (?, ?, ?, ?)`, copy.PendingTrackID, copy.SourceTrackID, copy.SourceFilePath, copy.SourceSHA256)
	if err != nil {
		return fmt.Errorf("record migrated legacy source: %w", err)
	}
	return nil
}

func scanMigratedSource(record *migratedSourceRecord) []any {
	return []any{&record.TrackID, &record.SourceTrackID, &record.SourceFilePath, &record.SourceSHA256}
}

// ListMigratedSources returns every recorded migrated source whose file has
// not been cleaned up yet.
func (store *Store) ListMigratedSources(ctx context.Context) (records []migratedSourceRecord, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT track_id, source_track_id, source_file_path, source_sha256
		FROM legacy_migration_sources
		WHERE cleaned_at IS NULL
		ORDER BY source_file_path`)
	if err != nil {
		return nil, fmt.Errorf("list migrated legacy sources: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var record migratedSourceRecord
		if err := rows.Scan(scanMigratedSource(&record)...); err != nil {
			return nil, fmt.Errorf("read migrated legacy source: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migrated legacy sources: %w", err)
	}
	return records, nil
}

func (store *Store) FindMigratedSource(ctx context.Context, trackID string) (migratedSourceRecord, bool, error) {
	var record migratedSourceRecord
	err := store.database.QueryRowContext(ctx, `
		SELECT track_id, source_track_id, source_file_path, source_sha256
		FROM legacy_migration_sources
		WHERE track_id = ? AND cleaned_at IS NULL`, trackID,
	).Scan(scanMigratedSource(&record)...)
	if errors.Is(err, sql.ErrNoRows) {
		return migratedSourceRecord{}, false, nil
	}
	if err != nil {
		return migratedSourceRecord{}, false, fmt.Errorf("find migrated legacy source: %w", err)
	}
	return record, true, nil
}

// FindActiveManagedTrackSource returns the managed file path and content hash
// of an active, committed Managed Track, or empty values when no such Track
// exists.
func (store *Store) FindActiveManagedTrackSource(ctx context.Context, trackID string) (string, string, error) {
	var filePath, contentSHA256 string
	err := store.database.QueryRowContext(ctx, `
		SELECT track_sources.file_path, COALESCE(track_sources.content_sha256, '')
		FROM tracks
		JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ? AND tracks.missing_at IS NULL AND tracks.is_pending_commit = 0
		AND track_sources.source_kind = 'managed'`, trackID,
	).Scan(&filePath, &contentSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("find active Managed Track source: %w", err)
	}
	return filePath, contentSHA256, nil
}

func (store *Store) MarkMigratedSourceCleaned(ctx context.Context, trackID string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE legacy_migration_sources SET cleaned_at = CURRENT_TIMESTAMP
		WHERE track_id = ? AND cleaned_at IS NULL`, trackID)
	if err != nil {
		return fmt.Errorf("record legacy source cleanup: %w", err)
	}
	if mutationErr := requireMutation(result); mutationErr != nil {
		return fmt.Errorf("record legacy source cleanup: %w", mutationErr)
	}
	return nil
}
