package managedimport

import (
	"context"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestCreateBatchStoresRecordingIdentificationChoice(t *testing.T) {
	store := NewStore(testutil.OpenMigratedDB(t))

	withIdentification, err := store.CreateBatch(context.Background(), BatchOptions{RecordingIdentification: true})
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	without, err := store.CreateBatch(context.Background(), BatchOptions{})
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}

	if !withIdentification.RecordingIdentification || without.RecordingIdentification {
		t.Fatalf("created batches = %+v / %+v", withIdentification, without)
	}
	reloaded, err := store.GetBatch(context.Background(), withIdentification.ID)
	if err != nil {
		t.Fatalf("GetBatch() error = %v", err)
	}
	if !reloaded.RecordingIdentification {
		t.Fatalf("reloaded batch = %+v, want RecordingIdentification true", reloaded)
	}
	reloadedWithout, err := store.GetBatch(context.Background(), without.ID)
	if err != nil || reloadedWithout.RecordingIdentification {
		t.Fatalf("reloaded batch = %+v err = %v", reloadedWithout, err)
	}
}
