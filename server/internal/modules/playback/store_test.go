package playback

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	_ "modernc.org/sqlite"
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

func setupPlaybackStore(t *testing.T, tracks map[string]library.Track) (*Store, *sql.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE playback_queue (id TEXT PRIMARY KEY, user_id TEXT, position INTEGER, track_id TEXT, UNIQUE(user_id, position));
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db, fakeTrackAccess{tracks: tracks}), db
}

func TestStoreUsesTrackAccessInterface(t *testing.T) {
	trackID := "track-1"
	store, _ := setupPlaybackStore(t, map[string]library.Track{
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
