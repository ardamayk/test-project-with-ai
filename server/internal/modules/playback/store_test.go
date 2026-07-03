package playback

import (
	"context"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

type fakeTrackAccess struct {
	tracks map[string]library.Track
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

	queue, err := store.AppendItem(context.Background(), "user-1", trackID)
	if err != nil {
		t.Fatal(err)
	}

	if len(queue.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(queue.Items))
	}
	if queue.Items[0].Track.Title != "Song" {
		t.Fatalf("track = %+v", queue.Items[0].Track)
	}
}
