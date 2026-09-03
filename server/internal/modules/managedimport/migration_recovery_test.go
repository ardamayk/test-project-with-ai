package managedimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

var errInjectedMigrationCrash = errors.New("injected Library Migration crash")

// TestLibraryMigrationRecoversFromCrashAtEveryDurablePhase interrupts the
// migration pipeline at every durable phase boundary, restarts the service
// against the same database and storage, and asserts that recovery lands in a
// deterministic state the repeated migration job resumes from without
// duplicating files or Tracks. A restart never deletes the original legacy
// source or completes the cutover automatically.
func TestLibraryMigrationRecoversFromCrashAtEveryDurablePhase(t *testing.T) {
	phases := []migrationPhase{
		MIGRATION_PHASE_PREPARED,
		MIGRATION_PHASE_VERIFIED,
		MIGRATION_PHASE_PROMOTED,
		MIGRATION_PHASE_DATABASE_COMMITTED,
	}
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "migration-recovery.db")
			database := openCommitRecoveryDatabase(t, databasePath)
			storageRoot := t.TempDir()
			sourceBytes := []byte("recovery legacy audio")
			sourcePath, trackID := seedLegacyMigrationTrack(t, database, "recovery.flac", sourceBytes, 1)
			service := newMigrationRecoveryService(t, database, storageRoot, sourceBytes)
			service.migrationPhaseHook = func(reached migrationPhase) error {
				if reached == phase {
					return errInjectedMigrationCrash
				}
				return nil
			}

			if phase == MIGRATION_PHASE_PREPARED || phase == MIGRATION_PHASE_VERIFIED {
				if _, err := service.StageMigration(context.Background()); err != nil {
					t.Fatalf("interrupt staging at %q: %v", phase, err)
				}
			} else {
				if _, err := service.StageMigration(context.Background()); err != nil {
					t.Fatalf("stage before interrupt at %q: %v", phase, err)
				}
				if _, err := service.ActivateMigration(context.Background()); err != nil {
					t.Fatalf("interrupt cutover at %q: %v", phase, err)
				}
			}
			assertCrashPointState(t, database, storageRoot, trackID, phase)

			if err := database.Close(); err != nil {
				t.Fatalf("close database before recovery: %v", err)
			}
			database = openCommitRecoveryDatabase(t, databasePath)
			restarted := newMigrationRecoveryService(t, database, storageRoot, sourceBytes)
			if err := restarted.CleanupRestart(context.Background()); err != nil {
				t.Fatalf("recover phase %q: %v", phase, err)
			}
			if err := restarted.CleanupRestart(context.Background()); err != nil {
				t.Fatalf("repeat recovery phase %q: %v", phase, err)
			}
			assertRecoveredState(t, database, storageRoot, trackID, phase)
			if _, err := os.Stat(sourcePath); err != nil {
				t.Fatalf("restart deleted the original legacy source file at %q: %v", phase, err)
			}

			if phase == MIGRATION_PHASE_DATABASE_COMMITTED {
				cutover, err := restarted.ActivateMigration(context.Background())
				if err != nil {
					t.Fatalf("repeat cutover after %q: %v", phase, err)
				}
				if cutover.MigratedCount != 0 || len(cutover.Files) != 0 {
					t.Fatalf("repeated cutover after %q = %+v", phase, cutover)
				}
				return
			}
			if phase == MIGRATION_PHASE_PREPARED {
				if _, err := restarted.StageMigration(context.Background()); err != nil {
					t.Fatalf("resume staging after %q: %v", phase, err)
				}
			}
			cutover, err := restarted.ActivateMigration(context.Background())
			if err != nil {
				t.Fatalf("resume cutover after %q: %v", phase, err)
			}
			if cutover.MigratedCount != 1 || len(cutover.Files) != 1 || cutover.Files[0].State != MIGRATION_CUTOVER_MIGRATED {
				t.Fatalf("resumed cutover after %q = %+v", phase, cutover)
			}
			assertResumedMigration(t, database, storageRoot, sourcePath, trackID)
		})
	}
}

func assertCrashPointState(t *testing.T, database *sql.DB, storageRoot, trackID string, phase migrationPhase) {
	t.Helper()
	switch phase {
	case MIGRATION_PHASE_PREPARED:
		assertMigrationCopyState(t, database, trackID, "prepared", "prepared", "")
	case MIGRATION_PHASE_VERIFIED:
		assertMigrationCopyState(t, database, trackID, "verified", "verified", "")
	case MIGRATION_PHASE_PROMOTED:
		assertMigrationCopyState(t, database, trackID, "verified", "promoted", "")
		if count := countStorageFiles(t, storageRoot, CANONICAL_LIBRARY_ROOT); count == 0 {
			t.Fatal("promoted crash point has no canonical Library Migration files")
		}
		if count := countStorageFiles(t, storageRoot, MIGRATION_STORAGE_ROOT); count != 0 {
			t.Fatalf("promoted crash point still holds %d pending files", count)
		}
	case MIGRATION_PHASE_DATABASE_COMMITTED:
		assertNoMigrationCopy(t, database, trackID)
		if _, err := os.Stat(managedMigrationAudio(t, database)); err != nil {
			t.Fatalf("stat canonical migrated Track: %v", err)
		}
		return
	}
	assertActiveLegacySource(t, database, trackID)
}

func assertRecoveredState(t *testing.T, database *sql.DB, storageRoot, trackID string, phase migrationPhase) {
	t.Helper()
	switch phase {
	case MIGRATION_PHASE_PREPARED:
		assertMigrationCopyState(t, database, trackID, "failed", "prepared", "restarted")
		if count := countStorageFiles(t, storageRoot, MIGRATION_STORAGE_ROOT); count != 0 {
			t.Fatalf("prepared recovery left %d pending files", count)
		}
	case MIGRATION_PHASE_VERIFIED:
		assertMigrationCopyState(t, database, trackID, "verified", "verified", "")
		if _, err := os.Stat(recordedMigrationAudio(t, database, trackID)); err != nil {
			t.Fatalf("verified recovery lost the pending audio: %v", err)
		}
	case MIGRATION_PHASE_PROMOTED:
		assertMigrationCopyState(t, database, trackID, "verified", "verified", "cutover")
		if _, err := os.Stat(recordedMigrationAudio(t, database, trackID)); err != nil {
			t.Fatalf("promoted recovery did not restore the pending audio: %v", err)
		}
		if count := countStorageFiles(t, storageRoot, CANONICAL_LIBRARY_ROOT); count != 0 {
			t.Fatalf("promoted recovery left %d canonical files", count)
		}
	case MIGRATION_PHASE_DATABASE_COMMITTED:
		assertNoMigrationCopy(t, database, trackID)
		if _, err := os.Stat(managedMigrationAudio(t, database)); err != nil {
			t.Fatalf("stat committed canonical Track after recovery: %v", err)
		}
	}
}

// assertResumedMigration verifies the completed migration left exactly one
// managed Track with its canonical files and never deleted the original
// legacy source file from disk.
func assertResumedMigration(t *testing.T, database *sql.DB, storageRoot, sourcePath, legacyTrackID string) {
	t.Helper()
	assertNoMigrationCopy(t, database, legacyTrackID)
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("original legacy source file was deleted: %v", err)
	}
	var managedCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM track_sources WHERE source_kind = 'managed'`).Scan(&managedCount); err != nil {
		t.Fatalf("count managed sources: %v", err)
	}
	if managedCount != 1 {
		t.Fatalf("resumed migration created %d managed sources, want 1", managedCount)
	}
	if count := countStorageFiles(t, storageRoot, CANONICAL_LIBRARY_ROOT); count != 2 {
		t.Fatalf("resumed migration left %d canonical files, want audio and artwork", count)
	}
	if count := countStorageFiles(t, storageRoot, MIGRATION_STORAGE_ROOT); count != 0 {
		t.Fatalf("resumed migration left %d pending files", count)
	}
}

func TestCleanupRestartFailsVerifiedMigrationCopyWhenPendingAudioDisappeared(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storageRoot := t.TempDir()
	copy := seedRecordedMigrationCopy(t, database, storageRoot, "disappeared", "verified", "verified")
	if err := os.Remove(copy.PendingAudioPath); err != nil {
		t.Fatalf("remove pending migration audio: %v", err)
	}
	service := NewService(NewStore(database), newStorage(storageRoot, StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity), nil)

	if err := service.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("cleanup restart: %v", err)
	}
	assertMigrationCopyState(t, database, copy.SourceTrackID, "failed", "prepared", "disappeared")
	if _, err := os.Stat(copy.PendingArtworkPath); !os.IsNotExist(err) {
		t.Fatalf("exclusive pending artwork survived the failed copy: %v", err)
	}
	assertActiveLegacySource(t, database, copy.SourceTrackID)
}

func TestCleanupRestartSweepsOrphanedMigrationFiles(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storageRoot := t.TempDir()
	copy := seedRecordedMigrationCopy(t, database, storageRoot, "swept", "verified", "verified")
	orphanPath := filepath.Join(filepath.Dir(copy.PendingAudioPath), "01-01-orphan-track.flac")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan migration file: %v", err)
	}
	strayRoot := filepath.Join(storageRoot, MIGRATION_STORAGE_ROOT, "artist-orphan", "album-orphan")
	if err := os.MkdirAll(strayRoot, 0o700); err != nil {
		t.Fatalf("create stray migration directory: %v", err)
	}
	strayPath := filepath.Join(strayRoot, "cover.png")
	if err := os.WriteFile(strayPath, []byte("stray"), 0o600); err != nil {
		t.Fatalf("write stray migration artwork: %v", err)
	}
	service := NewService(NewStore(database), newStorage(storageRoot, StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity), nil)

	if err := service.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("cleanup restart: %v", err)
	}
	for _, orphan := range []string{orphanPath, strayPath} {
		if _, err := os.Stat(orphan); !os.IsNotExist(err) {
			t.Fatalf("orphan migration file %q survived the sweep: %v", orphan, err)
		}
	}
	for _, recorded := range []string{copy.PendingAudioPath, copy.PendingArtworkPath} {
		if _, err := os.Stat(recorded); err != nil {
			t.Fatalf("recorded migration file %q was swept: %v", recorded, err)
		}
	}
	assertMigrationCopyState(t, database, copy.SourceTrackID, "verified", "verified", "")
}

// TestCleanupRestartRestoresPromotedCopyInterruptedMidPromotion simulates a
// crash between the audio and artwork renames of a promotion: the row is
// promoted, the audio already reached the Canonical Library Path, and the
// artwork is still pending. Recovery must return both to the pending location.
func TestCleanupRestartRestoresPromotedCopyInterruptedMidPromotion(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storageRoot := t.TempDir()
	copy := seedRecordedMigrationCopy(t, database, storageRoot, "midpromote", "verified", "promoted")
	canonicalDirectory := filepath.Join(storageRoot, CANONICAL_LIBRARY_ROOT, "artist-"+copy.PendingAlbumArtistID, "album-"+copy.PendingAlbumID)
	canonicalAudio := filepath.Join(canonicalDirectory, filepath.Base(copy.PendingAudioPath))
	if err := os.MkdirAll(canonicalDirectory, 0o750); err != nil {
		t.Fatalf("create canonical directory: %v", err)
	}
	if err := os.Rename(copy.PendingAudioPath, canonicalAudio); err != nil {
		t.Fatalf("move pending audio to the Canonical Library Path: %v", err)
	}
	service := NewService(NewStore(database), newStorage(storageRoot, StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity), nil)

	if err := service.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("cleanup restart: %v", err)
	}
	if err := service.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("repeat cleanup restart: %v", err)
	}
	assertMigrationCopyState(t, database, copy.SourceTrackID, "verified", "verified", "cutover")
	for _, restored := range []string{copy.PendingAudioPath, copy.PendingArtworkPath} {
		if _, err := os.Stat(restored); err != nil {
			t.Fatalf("mid-promotion recovery did not restore %q: %v", restored, err)
		}
	}
	if count := countStorageFiles(t, storageRoot, CANONICAL_LIBRARY_ROOT); count != 0 {
		t.Fatalf("mid-promotion recovery left %d canonical files", count)
	}
	assertActiveLegacySource(t, database, copy.SourceTrackID)
}

// TestLibraryMigrationCutoverActivationFailureRestoresPromotedCopy fails the
// cutover transaction after the copy reached the Canonical Library Path and
// verifies the within-run rollback restores the pending bytes so the next
// cutover run can promote them again.
func TestLibraryMigrationCutoverActivationFailureRestoresPromotedCopy(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storageRoot := t.TempDir()
	sourceBytes := []byte("activation failure legacy audio")
	sourcePath, trackID := seedLegacyMigrationTrack(t, database, "activation-failure.flac", sourceBytes, 1)
	service := newMigrationRecoveryService(t, database, storageRoot, sourceBytes)
	if _, err := service.StageMigration(context.Background()); err != nil {
		t.Fatalf("stage migration: %v", err)
	}
	if _, err := database.Exec(fmt.Sprintf(`
		CREATE TRIGGER reject_migration_cutover
		BEFORE UPDATE ON tracks
		WHEN NEW.missing_at IS NOT NULL AND OLD.missing_at IS NULL AND NEW.id = '%s'
		BEGIN
			SELECT RAISE(ABORT, 'forced cutover failure');
		END`, trackID)); err != nil {
		t.Fatalf("create cutover failure trigger: %v", err)
	}

	cutover, err := service.ActivateMigration(context.Background())
	if err != nil {
		t.Fatalf("run failing cutover: %v", err)
	}
	if cutover.MigratedCount != 0 || cutover.FailedCount != 1 || len(cutover.Files) != 1 {
		t.Fatalf("failing cutover = %+v", cutover)
	}
	assertMigrationCopyState(t, database, trackID, "verified", "verified", "rolled back")
	if _, statErr := os.Stat(recordedMigrationAudio(t, database, trackID)); statErr != nil {
		t.Fatalf("failed cutover did not restore the pending audio: %v", statErr)
	}
	if count := countStorageFiles(t, storageRoot, CANONICAL_LIBRARY_ROOT); count != 0 {
		t.Fatalf("failed cutover left %d canonical files", count)
	}
	assertActiveLegacySource(t, database, trackID)
	if _, statErr := os.Stat(sourcePath); statErr != nil {
		t.Fatalf("legacy source file did not survive the failed cutover: %v", statErr)
	}

	if _, err := database.Exec(`DROP TRIGGER reject_migration_cutover`); err != nil {
		t.Fatalf("drop cutover failure trigger: %v", err)
	}
	resumed, err := service.ActivateMigration(context.Background())
	if err != nil {
		t.Fatalf("resume cutover after activation failure: %v", err)
	}
	if resumed.MigratedCount != 1 || len(resumed.Files) != 1 || resumed.Files[0].State != MIGRATION_CUTOVER_MIGRATED {
		t.Fatalf("resumed cutover = %+v", resumed)
	}
	assertResumedMigration(t, database, storageRoot, sourcePath, trackID)
}

func newMigrationRecoveryService(t *testing.T, database *sql.DB, storageRoot string, sourceBytes []byte) *Service {
	t.Helper()
	inspection := strictMigrationInspection()
	inspection.FileSHA256 = migrationTestSHA256(sourceBytes)
	inspection.AlbumArtwork.SHA256 = migrationTestSHA256(inspection.AlbumArtwork.Data)
	module := newModule(database, config.Config{ManagedStoragePath: storageRoot}, migrationContentInspector{
		migrationTestSHA256(sourceBytes): inspection,
	}, unlimitedStorageCapacity)
	return module.service
}

func assertMigrationCopyState(t *testing.T, database *sql.DB, trackID, status, phase, reasonContains string) {
	t.Helper()
	var storedStatus, storedPhase, recoveryReason string
	if err := database.QueryRow(
		`SELECT status, phase, COALESCE(recovery_reason, '') FROM legacy_migration_copies WHERE source_track_id = ?`, trackID,
	).Scan(&storedStatus, &storedPhase, &recoveryReason); err != nil {
		t.Fatalf("read migration copy: %v", err)
	}
	if storedStatus != status || storedPhase != phase {
		t.Fatalf("migration copy = (%q, %q), want (%q, %q)", storedStatus, storedPhase, status, phase)
	}
	if reasonContains != "" && !strings.Contains(recoveryReason, reasonContains) {
		t.Fatalf("migration copy recovery reason = %q, want it to contain %q", recoveryReason, reasonContains)
	}
}

func assertNoMigrationCopy(t *testing.T, database *sql.DB, trackID string) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM legacy_migration_copies WHERE source_track_id = ?`, trackID).Scan(&count); err != nil {
		t.Fatalf("count migration copies: %v", err)
	}
	if count != 0 {
		t.Fatalf("migration copy for %q still exists", trackID)
	}
}

func assertActiveLegacySource(t *testing.T, database *sql.DB, trackID string) {
	t.Helper()
	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM tracks JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ? AND tracks.missing_at IS NULL AND track_sources.source_kind = 'legacy'`, trackID,
	).Scan(&count); err != nil {
		t.Fatalf("check active legacy source: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy Track %q is not an active legacy source after recovery", trackID)
	}
}

func recordedMigrationAudio(t *testing.T, database *sql.DB, trackID string) string {
	t.Helper()
	var pendingAudioPath string
	if err := database.QueryRow(`SELECT pending_audio_path FROM legacy_migration_copies WHERE source_track_id = ?`, trackID).Scan(&pendingAudioPath); err != nil {
		t.Fatalf("read pending migration audio path: %v", err)
	}
	return pendingAudioPath
}

func managedMigrationAudio(t *testing.T, database *sql.DB) string {
	t.Helper()
	var filePath string
	if err := database.QueryRow(`
		SELECT track_sources.file_path FROM tracks
		JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE track_sources.source_kind = 'managed' AND tracks.missing_at IS NULL`).Scan(&filePath); err != nil {
		t.Fatalf("read canonical migrated Track path: %v", err)
	}
	return filePath
}

func countStorageFiles(t *testing.T, storageRoot, rootName string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(filepath.Join(storageRoot, rootName), func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count files under %q: %v", rootName, err)
	}
	return count
}

// seedRecordedMigrationCopy inserts a copy row with pending files on disk,
// mirroring the manual fixture of the prepared-copy restart test.
func seedRecordedMigrationCopy(t *testing.T, database *sql.DB, storageRoot, label, status, phase string) migrationCopyRecord {
	t.Helper()
	sourcePath, sourceTrackID := seedLegacyMigrationTrack(t, database, label+".flac", []byte(label+" legacy audio"), 1)
	pendingTrackID := "00000000-0000-4000-8000-0000000000a1"
	pendingAlbumID := "00000000-0000-4000-8000-0000000000a2"
	pendingAlbumArtistID := "00000000-0000-4000-8000-0000000000a3"
	pendingDirectory := filepath.Join(storageRoot, MIGRATION_STORAGE_ROOT, "artist-"+pendingAlbumArtistID, "album-"+pendingAlbumID)
	pendingAudioPath := filepath.Join(pendingDirectory, "01-01-track-"+pendingTrackID+".flac")
	pendingArtworkPath := filepath.Join(pendingDirectory, "cover.png")
	if err := os.MkdirAll(pendingDirectory, 0o700); err != nil {
		t.Fatalf("create pending migration directory: %v", err)
	}
	if err := os.WriteFile(pendingAudioPath, []byte(label+" pending audio"), 0o600); err != nil {
		t.Fatalf("write pending migration audio: %v", err)
	}
	if err := os.WriteFile(pendingArtworkPath, []byte(label+" pending artwork"), 0o600); err != nil {
		t.Fatalf("write pending migration artwork: %v", err)
	}
	audioHash := migrationTestSHA256([]byte(label + " pending audio"))
	artworkHash := migrationTestSHA256([]byte(label + " pending artwork"))
	if _, err := database.Exec(`
		INSERT INTO legacy_migration_copies (
			source_track_id, pending_track_id, pending_album_id, pending_album_artist_id,
			source_file_path, pending_audio_path, pending_artwork_path, source_sha256,
			pending_sha256, artwork_sha256, inspection_json, status, phase
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		sourceTrackID, pendingTrackID, pendingAlbumID, pendingAlbumArtistID, sourcePath,
		pendingAudioPath, pendingArtworkPath, audioHash, audioHash, artworkHash, status, phase,
	); err != nil {
		t.Fatalf("seed migration copy: %v", err)
	}
	return migrationCopyRecord{
		SourceTrackID: sourceTrackID, PendingTrackID: pendingTrackID, PendingAlbumID: pendingAlbumID,
		PendingAlbumArtistID: pendingAlbumArtistID, SourceFilePath: sourcePath,
		PendingAudioPath: pendingAudioPath, PendingArtworkPath: pendingArtworkPath,
		SourceSHA256: audioHash, PendingSHA256: audioHash, ArtworkSHA256: artworkHash,
		Status: status, Phase: phase,
	}
}
