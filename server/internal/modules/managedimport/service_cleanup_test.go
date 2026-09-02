package managedimport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestFinishUncommittedBatchFileDoesNotCleanUpAfterCancellation(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	store := NewStore(database)
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	upload, err := storage.StageUpload(bytes.NewReader([]byte("audio")), 5)
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}
	service := NewService(store, storage, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = service.finishUncommittedBatchFile(ctx, importJob{Job: Job{ID: "job-1"}, StagedFilePath: upload.Path}, OUTCOME_NOT_ATTEMPTED, "", "")
	if err == nil {
		t.Fatal("cleanup succeeded after cancellation")
	}
	if _, err := os.Stat(upload.Path); err != nil {
		t.Fatalf("staged upload was removed after cancellation: %v", err)
	}
}

func TestFinishUncommittedBatchFileRetainsPathWhenCleanupFails(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	store := NewStore(database)
	batch, err := store.CreateBatch(context.Background())
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	job, err := store.CreateJob(context.Background(), batch.ID, "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	unsafePath := t.TempDir() + "/outside.upload"
	if _, err = database.Exec(`UPDATE managed_import_jobs SET staged_file_path = ? WHERE id = ?`, unsafePath, job.ID); err != nil {
		t.Fatalf("set staged path: %v", err)
	}
	if _, err = database.Exec(`UPDATE managed_import_batches SET status = ? WHERE id = ?`, BATCH_STATUS_CONFIRMING, batch.ID); err != nil {
		t.Fatalf("set batch status: %v", err)
	}
	if _, err = database.Exec(`UPDATE managed_import_jobs SET status = ?, selected = 0, original_filename = 'track.flac', content_sha256 = ? WHERE id = ?`, STATUS_AWAITING_CONFIRMATION, strings.Repeat("0", 64), job.ID); err != nil {
		t.Fatalf("set job status: %v", err)
	}
	service := NewService(store, newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity), nil)

	err = service.finishUncommittedBatchFile(context.Background(), importJob{Job: job, BatchID: batch.ID, StagedFilePath: unsafePath}, OUTCOME_NOT_ATTEMPTED, "", "")
	if err == nil {
		t.Fatal("cleanup unexpectedly succeeded")
	}
	storedJob, err := store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if storedJob.StagedFilePath != unsafePath || storedJob.Outcome != "" {
		t.Fatalf("job after failed cleanup = %+v", storedJob)
	}
}

func TestRejectExactDuplicateRetainsStagingUntilFailureIsPersisted(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	store := NewStore(database)
	job, err := store.CreateJob(context.Background(), "", "")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	upload, err := storage.StageUpload(bytes.NewReader([]byte("audio")), 5)
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}
	if _, err = database.Exec(`
		UPDATE managed_import_jobs
		SET status = ?, original_filename = 'duplicate.flac', staged_file_path = ?, content_sha256 = ?
		WHERE id = ?`, STATUS_AWAITING_CONFIRMATION, upload.Path, strings.Repeat("0", 64), job.ID); err != nil {
		t.Fatalf("prepare duplicate job: %v", err)
	}
	if _, err = database.Exec(`
		CREATE TRIGGER reject_exact_duplicate_transition
		BEFORE UPDATE ON managed_import_jobs
		WHEN NEW.status = 'failed' AND NEW.error_code = 'exact_duplicate'
		BEGIN
			SELECT RAISE(ABORT, 'forced duplicate persistence failure');
		END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	service := NewService(store, storage, nil)
	duplicateJob, err := store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get duplicate job: %v", err)
	}

	err = service.rejectExactDuplicate(context.Background(), duplicateJob)
	if !errors.Is(err, ErrExactDuplicate) {
		t.Fatalf("reject exact duplicate error = %v", err)
	}
	if _, err = os.Stat(upload.Path); err != nil {
		t.Fatalf("staged duplicate removed before failure was persisted: %v", err)
	}
	storedJob, err := store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get unpersisted duplicate job: %v", err)
	}
	if storedJob.Status != STATUS_AWAITING_CONFIRMATION || storedJob.StagedFilePath != upload.Path {
		t.Fatalf("job after persistence failure = %+v", storedJob)
	}

	if _, err = database.Exec(`DROP TRIGGER reject_exact_duplicate_transition`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	err = service.rejectExactDuplicate(context.Background(), storedJob)
	if !errors.Is(err, ErrExactDuplicate) {
		t.Fatalf("retry exact duplicate error = %v", err)
	}
	if _, err = os.Stat(upload.Path); !os.IsNotExist(err) {
		t.Fatalf("staged duplicate still exists after persisted rejection: %v", err)
	}
	storedJob, err = store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get rejected duplicate job: %v", err)
	}
	if storedJob.Status != STATUS_FAILED || storedJob.ErrorCode != ERROR_CODE_EXACT_DUPLICATE || storedJob.StagedFilePath != "" {
		t.Fatalf("persisted duplicate job = %+v", storedJob)
	}
}
