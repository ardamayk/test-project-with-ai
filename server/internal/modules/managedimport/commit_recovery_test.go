package managedimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	internaldb "github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

var errInjectedCommitCrash = errors.New("injected Managed Import commit crash")

type recoveryInspector struct {
	inspection library.MediaInspection
}

func (inspector recoveryInspector) Inspect(_ context.Context, _ string, _ library.InspectionProgressReporter) (library.MediaInspection, error) {
	return inspector.inspection, nil
}

func TestRecoverCommitAtEveryDurablePhase(t *testing.T) {
	phases := []commitPhase{
		COMMIT_PHASE_PREPARED,
		COMMIT_PHASE_PLACED,
		COMMIT_PHASE_VERIFIED,
		COMMIT_PHASE_DATABASE_COMMITTED,
		COMMIT_PHASE_CLEANED,
	}
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "recovery.db")
			database := openCommitRecoveryDatabase(t, databasePath)
			storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
			service, job, inspection := prepareCommitRecoveryJob(t, database, storage)
			service.commitPhaseHook = func(reached commitPhase) error {
				if reached == phase {
					return errInjectedCommitCrash
				}
				return nil
			}

			_, confirmErr := service.Confirm(context.Background(), job.ID, job.Revision)
			if !errors.Is(confirmErr, errInjectedCommitCrash) {
				t.Fatalf("confirm at phase %q error = %v", phase, confirmErr)
			}

			libraryStore := library.NewStore(database)
			trackID := commitTrackID(t, database, job.ID)
			if _, err := libraryStore.GetTrack(context.Background(), trackID); !errors.Is(err, library.ErrNotFound) {
				t.Fatalf("pending Track visible at phase %q: %v", phase, err)
			}
			if _, err := libraryStore.GetTrackFilePath(context.Background(), trackID); !errors.Is(err, library.ErrNotFound) {
				t.Fatalf("pending Track stream path visible at phase %q: %v", phase, err)
			}
			tracks, listErr := libraryStore.ListTracks(context.Background(), 100, 0, "")
			if listErr != nil || tracks.Total != 0 || len(tracks.Items) != 0 {
				t.Fatalf("pending Track listed at phase %q: tracks = %+v, error = %v", phase, tracks, listErr)
			}

			if err := database.Close(); err != nil {
				t.Fatalf("close database before recovery: %v", err)
			}
			database = openCommitRecoveryDatabase(t, databasePath)
			restarted := NewService(NewStore(database), storage, recoveryInspector{inspection: inspection})
			if err := restarted.RecoverCommits(context.Background()); err != nil {
				t.Fatalf("recover phase %q: %v", phase, err)
			}
			if err := database.Close(); err != nil {
				t.Fatalf("close database after recovery: %v", err)
			}
			database = openCommitRecoveryDatabase(t, databasePath)
			restarted = NewService(NewStore(database), storage, recoveryInspector{inspection: inspection})
			if err := restarted.RecoverCommits(context.Background()); err != nil {
				t.Fatalf("repeat recovery phase %q: %v", phase, err)
			}

			storedJob, err := restarted.store.GetJob(context.Background(), job.ID)
			if err != nil {
				t.Fatalf("get recovered job: %v", err)
			}
			if phase == COMMIT_PHASE_DATABASE_COMMITTED || phase == COMMIT_PHASE_CLEANED {
				if storedJob.Status != STATUS_COMMITTED {
					t.Fatalf("job status after completing recovery = %q", storedJob.Status)
				}
				track, err := library.NewStore(database).GetTrack(context.Background(), storedJob.TrackID)
				if err != nil {
					t.Fatalf("get recovered Track: %v", err)
				}
				if _, err := os.Stat(track.FilePath); err != nil {
					t.Fatalf("stat recovered canonical Track: %v", err)
				}
				return
			}
			if storedJob.Status != STATUS_AWAITING_CONFIRMATION {
				t.Fatalf("job status after rollback recovery = %q", storedJob.Status)
			}
			if _, err := os.Stat(storedJob.StagedFilePath); err != nil {
				t.Fatalf("stat restored staged file: %v", err)
			}
		})
	}
}

func openCommitRecoveryDatabase(t *testing.T, databasePath string) *sql.DB {
	t.Helper()
	database, err := internaldb.OpenAndMigrate(context.Background(), databasePath, filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("open recovery database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestRecoverPreparedJournalAfterPlacementGapRemovesCanonicalOrphans(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storageRoot := t.TempDir()
	storage := newStorage(storageRoot, StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	service, job, inspection := prepareCommitRecoveryJob(t, database, storage)
	identity, resolveErr := service.store.ResolveCommitIdentity(context.Background(), inspection.Metadata)
	if resolveErr != nil {
		t.Fatalf("resolve commit identity: %v", resolveErr)
	}
	planned, planErr := storage.planPlacement(job.StagedFilePath, inspection, identity)
	if planErr != nil {
		t.Fatalf("plan placement: %v", planErr)
	}
	journal := commitJournal{
		ID: "placement-gap", JobID: job.ID, TrackID: identity.TrackID, Phase: COMMIT_PHASE_PREPARED,
		StagedFilePath: job.StagedFilePath, AudioFilePath: planned.AudioPath,
		ArtworkFilePath: planned.ArtworkPath, AudioSHA256: inspection.FileSHA256,
		ArtworkSHA256: inspection.AlbumArtwork.SHA256, IsArtworkCreated: true,
	}
	if err := service.store.CreateCommitJournal(context.Background(), journal); err != nil {
		t.Fatalf("create prepared journal: %v", err)
	}
	if _, err := storage.Place(job.StagedFilePath, inspection, identity); err != nil {
		t.Fatalf("place before journal transition: %v", err)
	}

	if err := service.RecoverCommits(context.Background()); err != nil {
		t.Fatalf("recover placement gap: %v", err)
	}
	storedJob, err := service.store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if _, err := os.Stat(storedJob.StagedFilePath); err != nil {
		t.Fatalf("stat restored staging file: %v", err)
	}
	assertNoCanonicalFiles(t, storageRoot)
}

func TestRecoverCorruptPendingCanonicalTrackRollsBackWithReason(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	service, job, inspection := prepareCommitRecoveryJob(t, database, storage)
	service.commitPhaseHook = func(reached commitPhase) error {
		if reached == COMMIT_PHASE_DATABASE_COMMITTED {
			return errInjectedCommitCrash
		}
		return nil
	}
	if _, err := service.Confirm(context.Background(), job.ID, job.Revision); !errors.Is(err, errInjectedCommitCrash) {
		t.Fatalf("interrupt database commit: %v", err)
	}

	var canonicalPath string
	if err := database.QueryRow(`SELECT audio_file_path FROM managed_import_commit_journal WHERE job_id = ?`, job.ID).Scan(&canonicalPath); err != nil {
		t.Fatalf("read canonical path: %v", err)
	}
	if err := os.WriteFile(canonicalPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt canonical Track: %v", err)
	}

	restarted := NewService(NewStore(database), storage, recoveryInspector{inspection: inspection})
	if err := restarted.RecoverCommits(context.Background()); err != nil {
		t.Fatalf("recover corrupt canonical Track: %v", err)
	}
	storedJob, err := restarted.store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get rolled-back job: %v", err)
	}
	if storedJob.Status != STATUS_AWAITING_CONFIRMATION {
		t.Fatalf("rolled-back job status = %q", storedJob.Status)
	}
	if _, err := library.NewStore(database).GetTrack(context.Background(), commitTrackID(t, database, job.ID)); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("corrupt pending Track visible after recovery: %v", err)
	}
	var phase commitPhase
	var reason string
	if err := database.QueryRow(`SELECT phase, recovery_reason FROM managed_import_commit_journal WHERE job_id = ?`, job.ID).Scan(&phase, &reason); err != nil {
		t.Fatalf("read rollback reason: %v", err)
	}
	if phase != COMMIT_PHASE_ROLLED_BACK || reason == "" {
		t.Fatalf("recovery record = (%q, %q)", phase, reason)
	}
}

func TestCleanupRestartCompletesPendingDatabaseCommitWithoutDeletingContent(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	service, job, inspection := prepareCommitRecoveryJob(t, database, storage)
	service.commitPhaseHook = func(reached commitPhase) error {
		if reached == COMMIT_PHASE_DATABASE_COMMITTED {
			return errInjectedCommitCrash
		}
		return nil
	}
	if _, err := service.Confirm(context.Background(), job.ID, job.Revision); !errors.Is(err, errInjectedCommitCrash) {
		t.Fatalf("interrupt database commit: %v", err)
	}

	restarted := NewService(NewStore(database), storage, recoveryInspector{inspection: inspection})
	if err := restarted.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("run restart cleanup: %v", err)
	}
	if err := restarted.CleanupRestart(context.Background()); err != nil {
		t.Fatalf("repeat restart cleanup: %v", err)
	}
	storedJob, err := restarted.store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get completed job: %v", err)
	}
	track, err := library.NewStore(database).GetTrack(context.Background(), storedJob.TrackID)
	if err != nil {
		t.Fatalf("get completed Track: %v", err)
	}
	contents, err := os.ReadFile(track.FilePath)
	if err != nil {
		t.Fatalf("read completed canonical Track: %v", err)
	}
	if string(contents) != "managed audio" {
		t.Fatalf("completed canonical content = %q", contents)
	}
}

func prepareCommitRecoveryJob(t *testing.T, database *sql.DB, storage *Storage) (*Service, importJob, library.MediaInspection) {
	t.Helper()
	content := []byte("managed audio")
	audioHash := sha256.Sum256(content)
	artwork := []byte("artwork")
	artworkHash := sha256.Sum256(artwork)
	inspection := library.MediaInspection{
		Metadata: library.NormalizedMediaMetadata{
			Title: "Recovery Track", Artists: []string{"Track Artist"}, AlbumArtists: []string{"Album Artist"},
			Album: "Recovery Album", Genres: []string{"Rock"}, TrackPosition: library.MediaPosition{Number: 1}, DiscPosition: library.MediaPosition{Number: 1},
		},
		AlbumArtwork: library.AlbumArtwork{MIMEType: "image/png", Width: 1, Height: 1, Data: artwork, SHA256: hex.EncodeToString(artworkHash[:])},
		Audio:        library.TechnicalAudioProperties{Format: "flac", Container: "flac", Codec: "flac", DurationMs: 1000, SampleRateHz: 44100, ChannelCount: 2, BitDepth: 16, BitrateKbps: 800},
		FileSHA256:   hex.EncodeToString(audioHash[:]),
	}
	store := NewStore(database)
	created, err := store.CreateJob(context.Background(), "", "")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	upload, err := storage.StageUpload(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}
	job, err := store.MarkPreview(context.Background(), created.ID, "track.flac", upload.Path, upload.SHA256, `{}`, upload.Size, 1024)
	if err != nil {
		t.Fatalf("mark preview: %v", err)
	}
	return NewService(store, storage, recoveryInspector{inspection: inspection}), job, inspection
}

func commitTrackID(t *testing.T, database *sql.DB, jobID string) string {
	t.Helper()
	var trackID string
	if err := database.QueryRow(`
		SELECT track_id FROM managed_import_commit_journal
		WHERE job_id = ? ORDER BY created_at DESC LIMIT 1`, jobID).Scan(&trackID); err != nil {
		t.Fatalf("read journaled Track ID: %v", err)
	}
	return trackID
}

func assertNoCanonicalFiles(t *testing.T, storageRoot string) {
	t.Helper()
	libraryPath := filepath.Join(storageRoot, "library")
	err := filepath.WalkDir(libraryPath, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			t.Fatalf("canonical orphan remains: %s", entry.Name())
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect canonical files: %v", err)
	}
}
