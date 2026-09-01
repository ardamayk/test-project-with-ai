package managedimport

import (
	"bytes"
	"context"
	"os"
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
