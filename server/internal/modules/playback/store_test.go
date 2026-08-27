package playback

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

type fakeTrackAccess struct {
	tracks map[string]library.Track
}

func TestStoreAtomicallyAcceptsOnlyOneConcurrentClientMutation(t *testing.T) {
	ctx := context.Background()
	store := setupPlaybackStore(t, map[string]library.Track{
		"track-1": {ID: "track-1", Title: "First"},
		"track-2": {ID: "track-2", Title: "Second"},
	})
	start := make(chan struct{})
	errorsByClient := make([]error, 2)
	trackIDs := []string{"track-1", "track-2"}
	var waitGroup sync.WaitGroup
	for clientIndex := range trackIDs {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, errorsByClient[clientIndex] = store.AppendItem(ctx, "user-1", trackIDs[clientIndex], "0")
		}()
	}
	close(start)
	waitGroup.Wait()

	successCount := 0
	conflictCount := 0
	for _, err := range errorsByClient {
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflictCount++
		} else {
			t.Fatalf("unexpected mutation error: %v", err)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1 each", successCount, conflictCount)
	}
	queue, err := store.GetQueue(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if queue.Revision != "1" || len(queue.Items) != 1 {
		t.Fatalf("queue = %+v, want one item at revision 1", queue)
	}
}

func (f fakeTrackAccess) GetTrack(_ context.Context, trackID string) (library.Track, error) {
	track, ok := f.tracks[trackID]
	if !ok {
		return library.Track{}, library.ErrNotFound
	}
	return track, nil
}

func (f fakeTrackAccess) GetTrackFilePath(_ context.Context, trackID string) (string, error) {
	track, ok := f.tracks[trackID]
	if !ok {
		return "", library.ErrNotFound
	}
	return track.FilePath, nil
}

func setupPlaybackStore(t *testing.T, tracks map[string]library.Track) *Store {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	for trackID := range tracks {
		testutil.InsertTrack(t, db, trackID)
	}
	return NewStore(db, fakeTrackAccess{tracks: tracks})
}

func TestStoreUsesTrackAccessInterface(t *testing.T) {
	trackID := "track-1"
	store := setupPlaybackStore(t, map[string]library.Track{
		trackID: {ID: trackID, Title: "Song"},
	})

	queue, err := store.AppendItem(context.Background(), "user-1", trackID, "0")
	if err != nil {
		t.Fatal(err)
	}

	if len(queue.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(queue.Items))
	}
	if queue.Items[0].Track.Title != "Song" {
		t.Fatalf("track = %+v", queue.Items[0].Track)
	}
	if queue.Revision != "1" {
		t.Fatalf("revision = %q, want 1", queue.Revision)
	}
}

func TestStoreRejectsStaleAppendWithoutOverwritingConcurrentQueue(t *testing.T) {
	ctx := context.Background()
	store := setupPlaybackStore(t, map[string]library.Track{
		"track-1": {ID: "track-1", Title: "First"},
		"track-2": {ID: "track-2", Title: "Second"},
	})

	first, err := store.AppendItem(ctx, "user-1", "track-1", "0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppendItem(ctx, "user-1", "track-2", "0")
	if err != ErrRevisionConflict {
		t.Fatalf("err = %v, want ErrRevisionConflict", err)
	}
	current, err := store.GetQueue(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	if first.Revision != "1" || current.Revision != "1" {
		t.Fatalf("revisions = %q, %q, want 1", first.Revision, current.Revision)
	}
	if len(current.Items) != 1 || current.Items[0].TrackID != "track-1" {
		t.Fatalf("queue = %+v, want only track-1", current.Items)
	}
}

func TestStoreAppliesAllMutationsAgainstExpectedRevision(t *testing.T) {
	ctx := context.Background()
	store := setupPlaybackStore(t, map[string]library.Track{
		"track-1": {ID: "track-1", Title: "First"},
		"track-2": {ID: "track-2", Title: "Second"},
		"track-3": {ID: "track-3", Title: "Third"},
	})

	queue, err := store.ReplaceQueue(ctx, "user-1", []string{"track-1", "track-2"}, "0")
	if err != nil {
		t.Fatal(err)
	}
	if queue.Revision != "1" {
		t.Fatalf("replace revision = %q, want 1", queue.Revision)
	}

	queue, err = store.ReorderItems(ctx, "user-1", []string{queue.Items[1].ID, queue.Items[0].ID}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if queue.Revision != "2" || queue.Items[0].TrackID != "track-2" {
		t.Fatalf("reordered queue = %+v, revision %q", queue.Items, queue.Revision)
	}

	queue, err = store.RemoveItem(ctx, "user-1", queue.Items[0].ID, "2")
	if err != nil {
		t.Fatal(err)
	}
	if queue.Revision != "3" || len(queue.Items) != 1 || queue.Items[0].Position != 0 {
		t.Fatalf("removed queue = %+v, revision %q", queue.Items, queue.Revision)
	}

	_, err = store.ReplaceQueue(ctx, "user-1", []string{"track-3"}, "2")
	if err != ErrRevisionConflict {
		t.Fatalf("stale replace err = %v, want ErrRevisionConflict", err)
	}
	_, err = store.ReorderItems(ctx, "user-1", []string{queue.Items[0].ID}, "2")
	if err != ErrRevisionConflict {
		t.Fatalf("stale reorder err = %v, want ErrRevisionConflict", err)
	}
}

func TestStoreRejectsReorderThatDoesNotDescribeCurrentQueue(t *testing.T) {
	ctx := context.Background()
	store := setupPlaybackStore(t, map[string]library.Track{
		"track-1": {ID: "track-1", Title: "First"},
		"track-2": {ID: "track-2", Title: "Second"},
	})
	queue, err := store.ReplaceQueue(ctx, "user-1", []string{"track-1", "track-2"}, "0")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ReorderItems(ctx, "user-1", []string{queue.Items[0].ID}, "1")
	if err != ErrInvalidQueueOrder {
		t.Fatalf("err = %v, want ErrInvalidQueueOrder", err)
	}
	current, err := store.GetQueue(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != "1" || current.Items[0].ID != queue.Items[0].ID {
		t.Fatalf("queue changed after invalid reorder: %+v", current)
	}
}
